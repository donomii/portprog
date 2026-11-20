package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const packMetadataMaximum = int64(4 * bytesPerMebibyte)

func createOfflinePack(app App, requestedPath string, lock LockFile) (string, error) {
	if err := validateCompatibleLock(app, lock); err != nil {
		return "", err
	} else {
		packPath, pathErr := containedPath(app.Paths.BaseDir, requestedPath)
		if pathErr != nil {
			return "", fmt.Errorf("create offline pack %q: %w", requestedPath, pathErr)
		} else if filepath.Ext(packPath) != ".zip" {
			return "", fmt.Errorf("create offline pack %q: expected a .zip filename, got %q", requestedPath, filepath.Ext(packPath))
		} else if err := validatePackDestination(packPath); err != nil {
			return "", err
		} else if err := verifyLockCache(app, lock); err != nil {
			return "", err
		} else {
			return writeOfflinePack(app, packPath, lock)
		}
	}
}

func validatePackDestination(packPath string) error {
	info, err := os.Lstat(packPath)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect offline pack destination %q: expected a regular file or no path, got error: %w", packPath, err)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("inspect offline pack destination %q: expected a regular file or no path, got mode %v", packPath, info.Mode())
	} else {
		return nil
	}
}

func verifyLockCache(app App, lock LockFile) error {
	for _, pkg := range lock.Packages {
		cachePath := filepath.Join(app.Paths.CacheObjects, pkg.Asset.SHA256)
		valid, err := verifyCachedFile(cachePath, pkg.Asset)
		if err != nil {
			return fmt.Errorf("create offline pack for profile %q: %w", lock.Profile, err)
		} else if !valid {
			return fmt.Errorf("create offline pack for profile %q: expected verified cache object for package %q at %q, got none", lock.Profile, pkg.Name, cachePath)
		} else {
			continue
		}
	}
	return nil
}

func writeOfflinePack(app App, packPath string, lock LockFile) (string, error) {
	if err := ensureContainedDirectory(app.Paths.BaseDir, filepath.Dir(packPath)); err != nil {
		return "", fmt.Errorf("prepare offline pack directory %q: expected writable directory, got error: %w", filepath.Dir(packPath), err)
	} else {
		temporary, createErr := os.CreateTemp(filepath.Dir(packPath), ".pack-*.zip")
		if createErr != nil {
			return "", fmt.Errorf("create temporary offline pack near %q: expected writable directory, got error: %w", packPath, createErr)
		} else {
			return populateOfflinePack(app, temporary, packPath, lock)
		}
	}
}

func populateOfflinePack(app App, temporary *os.File, packPath string, lock LockFile) (string, error) {
	writer := zip.NewWriter(temporary)
	manifest := PackManifest{
		SchemaVersion: currentSchemaVersion, Profile: lock.Profile,
		OS: lock.OS, Arch: lock.Arch, CreatedAt: app.Deps.Now().UTC().Format(time.RFC3339),
	}
	manifestData, manifestErr := json.MarshalIndent(manifest, "", "  ")
	lockData, lockErr := json.MarshalIndent(lock, "", "  ")
	if manifestErr != nil || lockErr != nil {
		return "", abortPack(temporary, writer, fmt.Errorf("encode offline pack metadata: expected serializable data, got manifest=%v lock=%v", manifestErr, lockErr))
	} else if err := writePackBytes(writer, "pack.json", append(manifestData, '\n'), zip.Deflate); err != nil {
		return "", abortPack(temporary, writer, err)
	} else if err := writePackBytes(writer, "lock.json", append(lockData, '\n'), zip.Deflate); err != nil {
		return "", abortPack(temporary, writer, err)
	} else if err := writePackArchives(app, writer, lock); err != nil {
		return "", abortPack(temporary, writer, err)
	} else {
		return finishPackFile(app, temporary, writer, packPath)
	}
}

func writePackArchives(app App, writer *zip.Writer, lock LockFile) error {
	seen := make(map[string]bool)
	for _, pkg := range lock.Packages {
		if seen[pkg.Asset.SHA256] {
			continue
		} else {
			seen[pkg.Asset.SHA256] = true
			cachePath := filepath.Join(app.Paths.CacheObjects, pkg.Asset.SHA256)
			if err := writePackFile(writer, "archives/"+pkg.Asset.SHA256, cachePath); err != nil {
				return fmt.Errorf("add package %q cache object %q to offline pack: %w", pkg.Name, cachePath, err)
			} else {
				continue
			}
		}
	}
	return nil
}

func writePackBytes(writer *zip.Writer, name string, data []byte, method uint16) error {
	header := &zip.FileHeader{Name: name, Method: method}
	header.SetMode(defaultFileMode)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create offline pack entry %q: expected success, got error: %w", name, err)
	} else if written, err := entry.Write(data); err != nil {
		return fmt.Errorf("write offline pack entry %q: expected complete data, got error: %w", name, err)
	} else if written != len(data) {
		return fmt.Errorf("write offline pack entry %q: expected %d bytes, wrote %d", name, len(data), written)
	} else {
		return nil
	}
}

func writePackFile(writer *zip.Writer, name string, filename string) error {
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(defaultFileMode)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create offline pack entry %q: expected success, got error: %w", name, err)
	} else {
		file, openErr := os.Open(filename)
		if openErr != nil {
			return fmt.Errorf("open cached file %q: expected readable file, got error: %w", filename, openErr)
		} else {
			_, copyErr := io.Copy(entry, file)
			closeErr := file.Close()
			if copyErr != nil || closeErr != nil {
				return fmt.Errorf("copy cached file %q to offline pack: expected complete read, got copy=%v close=%v", filename, copyErr, closeErr)
			} else {
				return nil
			}
		}
	}
}

func finishPackFile(app App, temporary *os.File, writer *zip.Writer, packPath string) (string, error) {
	zipCloseErr := writer.Close()
	syncErr := temporary.Sync()
	fileCloseErr := temporary.Close()
	if zipCloseErr != nil || syncErr != nil || fileCloseErr != nil {
		removeErr := os.Remove(temporary.Name())
		return "", fmt.Errorf("finish offline pack %q: expected synchronized ZIP, got zip-close=%v sync=%v file-close=%v cleanup=%v", packPath, zipCloseErr, syncErr, fileCloseErr, removeErr)
	} else if err := os.Chmod(temporary.Name(), defaultFileMode); err != nil {
		removeErr := os.Remove(temporary.Name())
		return "", fmt.Errorf("set offline pack %q permissions: expected mode %o, got error %v and cleanup result %v", packPath, defaultFileMode, err, removeErr)
	} else if err := publishStateFile(app, temporary.Name(), packPath, "previous-offline-pack"); err != nil {
		return "", err
	} else {
		return packPath, nil
	}
}

func abortPack(temporary *os.File, writer *zip.Writer, cause error) error {
	zipCloseErr := writer.Close()
	fileCloseErr := temporary.Close()
	removeErr := os.Remove(temporary.Name())
	return fmt.Errorf("create offline pack: %w; cleanup zip-close=%v file-close=%v remove=%v", cause, zipCloseErr, fileCloseErr, removeErr)
}

func restoreOfflinePack(app App, filename string) error {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return fmt.Errorf("open offline pack %q: expected readable ZIP, got error: %w", filename, err)
	} else {
		entries, indexErr := indexPackEntries(reader.File)
		if indexErr != nil {
			return closeOfflinePackOnError(reader, filename, indexErr)
		} else {
			lock, validateErr := validateOfflinePack(app, entries)
			if validateErr != nil {
				return closeOfflinePackOnError(reader, filename, validateErr)
			} else if importErr := importPackArchives(app, entries, lock); importErr != nil {
				return closeOfflinePackOnError(reader, filename, importErr)
			} else if closeErr := reader.Close(); closeErr != nil {
				return fmt.Errorf("close offline pack %q: expected success, got error: %w", filename, closeErr)
			} else {
				offlineApp := app
				offlineApp.Settings.Offline = true
				return installLockedProfile(offlineApp, lock)
			}
		}
	}
}

func closeOfflinePackOnError(reader *zip.ReadCloser, filename string, cause error) error {
	if closeErr := reader.Close(); closeErr != nil {
		return fmt.Errorf("use offline pack %q: %w; close also failed: %v", filename, cause, closeErr)
	} else {
		return cause
	}
}

func indexPackEntries(files []*zip.File) (map[string]*zip.File, error) {
	entries := make(map[string]*zip.File)
	for _, file := range files {
		if file.Mode()&os.ModeSymlink != 0 || !file.Mode().IsRegular() {
			return nil, fmt.Errorf("validate offline pack entry %q: expected regular file, got mode %v", file.Name, file.Mode())
		} else if _, duplicate := entries[file.Name]; duplicate {
			return nil, fmt.Errorf("validate offline pack: expected unique entry names, got duplicate %q", file.Name)
		} else if file.Name != "pack.json" && file.Name != "lock.json" && !validArchiveEntryName(file.Name) {
			return nil, fmt.Errorf("validate offline pack: expected pack.json, lock.json, or archives/SHA256, got %q", file.Name)
		} else {
			entries[file.Name] = file
		}
	}
	return entries, nil
}

func validArchiveEntryName(name string) bool {
	if !strings.HasPrefix(name, "archives/") {
		return false
	} else {
		digest := strings.TrimPrefix(name, "archives/")
		return validateDigest(digest) == nil && !strings.Contains(digest, "/")
	}
}

func validateOfflinePack(app App, entries map[string]*zip.File) (LockFile, error) {
	manifestEntry, hasManifest := entries["pack.json"]
	lockEntry, hasLock := entries["lock.json"]
	if !hasManifest || !hasLock {
		return LockFile{}, fmt.Errorf("validate offline pack: expected pack.json and lock.json, got pack.json=%t lock.json=%t", hasManifest, hasLock)
	} else {
		manifest, manifestErr := readPackManifest(manifestEntry)
		lock, lockErr := readPackedLock(lockEntry)
		if manifestErr != nil {
			return LockFile{}, manifestErr
		} else if lockErr != nil {
			return LockFile{}, lockErr
		} else if manifest.Profile != lock.Profile || manifest.OS != lock.OS || manifest.Arch != lock.Arch {
			return LockFile{}, fmt.Errorf("validate offline pack metadata: expected matching profile and platform, got manifest=%s %s/%s lock=%s %s/%s", manifest.Profile, manifest.OS, manifest.Arch, lock.Profile, lock.OS, lock.Arch)
		} else if err := validateCompatibleLock(app, lock); err != nil {
			return LockFile{}, err
		} else if err := verifyPackArchives(entries, lock); err != nil {
			return LockFile{}, err
		} else {
			return lock, nil
		}
	}
}

func readPackManifest(entry *zip.File) (PackManifest, error) {
	data, err := readPackEntry(entry, packMetadataMaximum)
	if err != nil {
		return PackManifest{}, err
	} else {
		decoder, decoderErr := newStrictJSONDecoder(data, entry.Name)
		if decoderErr != nil {
			return PackManifest{}, decoderErr
		} else {
			var manifest PackManifest
			if decodeErr := decoder.Decode(&manifest); decodeErr != nil {
				return PackManifest{}, fmt.Errorf("decode pack manifest: expected v1 metadata, got error: %w", decodeErr)
			} else if endErr := requireJSONEnd(decoder, entry.Name); endErr != nil {
				return PackManifest{}, endErr
			} else if validateErr := validatePackManifest(manifest); validateErr != nil {
				return PackManifest{}, validateErr
			} else {
				return manifest, nil
			}
		}
	}
}

func validatePackManifest(manifest PackManifest) error {
	if manifest.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("validate pack manifest: expected schema_version %d, got %d", currentSchemaVersion, manifest.SchemaVersion)
	} else if err := validateIdentifier("pack profile", manifest.Profile); err != nil {
		return err
	} else if !supportedOS(manifest.OS) || !supportedArch(manifest.Arch) {
		return fmt.Errorf("validate pack manifest: expected supported platform, got %s/%s", manifest.OS, manifest.Arch)
	} else if err := validateUTCRFC3339("pack created_at", manifest.CreatedAt); err != nil {
		return err
	} else {
		return nil
	}
}

func readPackedLock(entry *zip.File) (LockFile, error) {
	data, err := readPackEntry(entry, packMetadataMaximum)
	if err != nil {
		return LockFile{}, err
	} else {
		decoder, decoderErr := newStrictJSONDecoder(data, entry.Name)
		if decoderErr != nil {
			return LockFile{}, decoderErr
		} else {
			var lock LockFile
			if decodeErr := decoder.Decode(&lock); decodeErr != nil {
				return LockFile{}, fmt.Errorf("decode packed lock: expected v1 lock, got error: %w", decodeErr)
			} else if endErr := requireJSONEnd(decoder, entry.Name); endErr != nil {
				return LockFile{}, endErr
			} else if validateErr := validateLock(lock); validateErr != nil {
				return LockFile{}, validateErr
			} else {
				return lock, nil
			}
		}
	}
}

func readPackEntry(entry *zip.File, maximum int64) ([]byte, error) {
	if entry.UncompressedSize64 > uint64(maximum) {
		return nil, fmt.Errorf("read offline pack entry %q: expected at most %d bytes, declared %d", entry.Name, maximum, entry.UncompressedSize64)
	} else {
		reader, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open offline pack entry %q: expected readable file, got error: %w", entry.Name, err)
		} else {
			data, readErr := io.ReadAll(io.LimitReader(reader, maximum+1))
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil {
				return nil, fmt.Errorf("read offline pack entry %q: expected complete file, got read=%v close=%v", entry.Name, readErr, closeErr)
			} else if int64(len(data)) > maximum {
				return nil, fmt.Errorf("read offline pack entry %q: expected at most %d bytes, got at least %d", entry.Name, maximum, len(data))
			} else {
				return data, nil
			}
		}
	}
}

func verifyPackArchives(entries map[string]*zip.File, lock LockFile) error {
	expected := lockAssetsByDigest(lock)
	for digest, asset := range expected {
		entry, exists := entries["archives/"+digest]
		if !exists {
			return fmt.Errorf("validate offline pack: expected archive %s, got none", digest)
		} else if err := verifyPackArchive(entry, asset); err != nil {
			return err
		} else {
			continue
		}
	}
	for name := range entries {
		if strings.HasPrefix(name, "archives/") {
			digest := strings.TrimPrefix(name, "archives/")
			if _, expectedDigest := expected[digest]; !expectedDigest {
				return fmt.Errorf("validate offline pack: expected only locked archives, got unexpected digest %s", digest)
			} else {
				continue
			}
		} else {
			continue
		}
	}
	return nil
}

func verifyPackArchive(entry *zip.File, asset PackageAsset) error {
	maximum, maximumErr := packArchiveMaximum(entry, asset)
	if maximumErr != nil {
		return maximumErr
	} else if asset.Size != 0 && int64(entry.UncompressedSize64) != asset.Size {
		return fmt.Errorf("verify offline archive %s size: expected %d bytes, got %d", asset.SHA256, asset.Size, entry.UncompressedSize64)
	} else {
		reader, err := entry.Open()
		if err != nil {
			return fmt.Errorf("open offline archive %s: expected readable file, got error: %w", asset.SHA256, err)
		} else {
			hasher := sha256.New()
			written, copyErr := io.Copy(hasher, io.LimitReader(reader, maximum+1))
			closeErr := reader.Close()
			actual := fmt.Sprintf("%x", hasher.Sum(nil))
			if copyErr != nil || closeErr != nil {
				return fmt.Errorf("verify offline archive %s: expected complete read, got copy=%v close=%v", asset.SHA256, copyErr, closeErr)
			} else if written > maximum {
				return fmt.Errorf("verify offline archive %s: expected bounded size, got at least %d bytes", asset.SHA256, written)
			} else if actual != asset.SHA256 {
				return fmt.Errorf("verify offline archive SHA-256: expected %s, got %s", asset.SHA256, actual)
			} else {
				return nil
			}
		}
	}
}

func packArchiveMaximum(entry *zip.File, asset PackageAsset) (int64, error) {
	if entry.UncompressedSize64 > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("verify offline archive %s: expected size representable as a signed integer, got %d", asset.SHA256, entry.UncompressedSize64)
	} else if entry.UncompressedSize64 == uint64(math.MaxInt64) || asset.Size == math.MaxInt64 {
		return 0, fmt.Errorf("verify offline archive %s: expected size below %d so the over-limit sentinel is representable", asset.SHA256, int64(math.MaxInt64))
	} else if asset.Size == 0 {
		return int64(entry.UncompressedSize64), nil
	} else {
		return asset.Size, nil
	}
}

func lockAssetsByDigest(lock LockFile) map[string]PackageAsset {
	assets := make(map[string]PackageAsset)
	for _, pkg := range lock.Packages {
		assets[pkg.Asset.SHA256] = pkg.Asset
	}
	return assets
}

func importPackArchives(app App, entries map[string]*zip.File, lock LockFile) error {
	if err := ensureStateDirectories(app); err != nil {
		return err
	} else {
		assets := lockAssetsByDigest(lock)
		digests := make([]string, 0, len(assets))
		for digest := range assets {
			digests = append(digests, digest)
		}
		sort.Strings(digests)
		for _, digest := range digests {
			if err := importPackArchive(app, entries["archives/"+digest], assets[digest]); err != nil {
				return err
			} else {
				continue
			}
		}
		return nil
	}
}

func importPackArchive(app App, entry *zip.File, asset PackageAsset) error {
	cachePath := filepath.Join(app.Paths.CacheObjects, asset.SHA256)
	valid, verifyErr := verifyCachedFile(cachePath, asset)
	if verifyErr != nil {
		return verifyErr
	} else if valid {
		return nil
	} else {
		if regularFileExists(cachePath) {
			if _, err := movePathToTrash(app, cachePath, "invalid-cache-restore"); err != nil {
				return err
			} else {
				return copyPackArchiveToCache(app, entry, asset, cachePath)
			}
		} else {
			return copyPackArchiveToCache(app, entry, asset, cachePath)
		}
	}
}

func copyPackArchiveToCache(app App, entry *zip.File, asset PackageAsset, cachePath string) error {
	reader, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open offline archive %s for import: expected readable file, got error: %w", asset.SHA256, err)
	} else {
		temporary, createErr := os.CreateTemp(app.Paths.CacheObjects, ".restore-*")
		if createErr != nil {
			closeErr := reader.Close()
			return fmt.Errorf("create temporary restored cache object: expected writable cache, got error %v and archive close result %v", createErr, closeErr)
		} else {
			maximum, maximumErr := packArchiveMaximum(entry, asset)
			if maximumErr != nil {
				readerCloseErr := reader.Close()
				fileCloseErr := temporary.Close()
				removeErr := os.Remove(temporary.Name())
				return fmt.Errorf("import offline archive %s: %w; cleanup reader-close=%v file-close=%v remove=%v", asset.SHA256, maximumErr, readerCloseErr, fileCloseErr, removeErr)
			} else {
				return finishPackArchiveImport(app, reader, temporary, asset, cachePath, maximum)
			}
		}
	}
}

func finishPackArchiveImport(app App, reader io.ReadCloser, temporary *os.File, asset PackageAsset, cachePath string, maximum int64) error {
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hasher), io.LimitReader(reader, maximum+1))
	syncErr := temporary.Sync()
	fileCloseErr := temporary.Close()
	readerCloseErr := reader.Close()
	actual := fmt.Sprintf("%x", hasher.Sum(nil))
	if copyErr != nil || syncErr != nil || fileCloseErr != nil || readerCloseErr != nil {
		preserveTemporaryDownload(app, temporary.Name(), "offline-restore")
		return fmt.Errorf("import offline archive %s: expected synchronized file, got copy=%v sync=%v file-close=%v reader-close=%v", asset.SHA256, copyErr, syncErr, fileCloseErr, readerCloseErr)
	} else if written > maximum || actual != asset.SHA256 {
		preserveTemporaryDownload(app, temporary.Name(), "offline-restore")
		return fmt.Errorf("import offline archive: expected size at most %d and SHA-256 %s, got size %d and SHA-256 %s", maximum, asset.SHA256, written, actual)
	} else if err := os.Chmod(temporary.Name(), defaultFileMode); err != nil {
		preserveTemporaryDownload(app, temporary.Name(), "offline-restore")
		return fmt.Errorf("set imported cache object permissions: expected mode %o, got error: %w", defaultFileMode, err)
	} else if err := os.Rename(temporary.Name(), cachePath); err != nil {
		preserveTemporaryDownload(app, temporary.Name(), "offline-restore")
		return fmt.Errorf("publish imported cache object %q: expected atomic rename, got error: %w", cachePath, err)
	} else {
		return nil
	}
}
