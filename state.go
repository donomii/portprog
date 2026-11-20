package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const installModuleBoundary = "module portprog.local/installed\n"

func ensureStateDirectories(app App) error {
	directories := []string{
		app.Paths.StoreRoot, app.Paths.CacheObjects, app.Paths.ReceiptsRoot, app.Paths.TrashRoot,
	}
	for _, directory := range directories {
		if err := ensureContainedDirectory(app.Paths.BaseDir, directory); err != nil {
			return fmt.Errorf("prepare Portprog directory %q: expected writable directory, got error: %w", directory, err)
		} else {
			continue
		}
	}
	return ensureInstallModuleBoundary(app)
}

func ensureInstallModuleBoundary(app App) error {
	filename := filepath.Join(app.Paths.InstallRoot, "go.mod")
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return writeBytesAtomically(app, filename, []byte(installModuleBoundary), "previous-install-module-boundary")
	} else if err != nil {
		return fmt.Errorf("read installed-tool module boundary %q: expected Portprog marker or no file, got error: %w", filename, err)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("validate installed-tool module boundary %q: expected a regular file without symbolic links, got mode %v", filename, info.Mode())
	} else {
		data, readErr := readRegularFile(filename, "read installed-tool module boundary")
		if readErr != nil {
			return readErr
		} else if !bytes.Equal(data, []byte(installModuleBoundary)) {
			return fmt.Errorf("validate installed-tool module boundary %q: expected %q, got different content; Portprog will not overwrite it", filename, strings.TrimSpace(installModuleBoundary))
		} else {
			return nil
		}
	}
}

func writeLock(app App, lock LockFile) error {
	if err := validateLock(lock); err != nil {
		return fmt.Errorf("write lockfile %q: %w", app.Paths.LockFile, err)
	} else {
		data, marshalErr := json.MarshalIndent(lock, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode lockfile %q: expected serializable lock data, got error: %w", app.Paths.LockFile, marshalErr)
		} else {
			return writeBytesAtomically(app, app.Paths.LockFile, append(data, '\n'), "previous-lockfile")
		}
	}
}

func readLock(filename string) (LockFile, error) {
	data, err := readRegularFile(filename, "read lockfile")
	if err != nil {
		return LockFile{}, err
	} else {
		decoder, decoderErr := newStrictJSONDecoder(data, filename)
		if decoderErr != nil {
			return LockFile{}, decoderErr
		} else {
			var lock LockFile
			if decodeErr := decoder.Decode(&lock); decodeErr != nil {
				return LockFile{}, fmt.Errorf("decode lockfile %q: expected the v1 schema, got error: %w", filename, decodeErr)
			} else if endErr := requireJSONEnd(decoder, filename); endErr != nil {
				return LockFile{}, endErr
			} else if validateErr := validateLock(lock); validateErr != nil {
				return LockFile{}, fmt.Errorf("validate lockfile %q: %w", filename, validateErr)
			} else {
				return lock, nil
			}
		}
	}
}

func validateLock(lock LockFile) error {
	if lock.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("expected schema_version %d, got %d", currentSchemaVersion, lock.SchemaVersion)
	} else if err := validateIdentifier("lock profile", lock.Profile); err != nil {
		return err
	} else if strings.TrimSpace(lock.ProfileDescription) == "" {
		return fmt.Errorf("validate lock profile %q: expected a description", lock.Profile)
	} else if !supportedOS(lock.OS) || !supportedArch(lock.Arch) {
		return fmt.Errorf("validate lock profile %q: expected supported platform, got %s/%s", lock.Profile, lock.OS, lock.Arch)
	} else if err := validateUTCRFC3339("lock generated_at", lock.GeneratedAt); err != nil {
		return err
	} else if len(lock.Packages) == 0 {
		return fmt.Errorf("validate lock profile %q: expected at least one package", lock.Profile)
	} else {
		return validateLockedPackages(lock)
	}
}

func validateLockedPackages(lock LockFile) error {
	seen := make(map[string]bool)
	for _, pkg := range lock.Packages {
		if err := validateIdentifier("locked package name", pkg.Name); err != nil {
			return err
		} else if seen[pkg.Name] {
			return fmt.Errorf("validate lock profile %q: expected package %q once, got duplicate", lock.Profile, pkg.Name)
		} else if versionErr := validateVersion("locked package version", pkg.Version); versionErr != nil {
			return versionErr
		} else if strings.TrimSpace(pkg.Description) == "" || strings.TrimSpace(pkg.License) == "" {
			return fmt.Errorf("validate locked package %q: expected non-empty version, description, and license", pkg.Name)
		} else if pkg.Asset.OS != lock.OS || pkg.Asset.Arch != lock.Arch {
			return fmt.Errorf("validate locked package %q: expected asset %s/%s, got %s/%s", pkg.Name, lock.OS, lock.Arch, pkg.Asset.OS, pkg.Asset.Arch)
		} else if err := validateAsset(pkg.Asset); err != nil {
			return fmt.Errorf("validate locked package %q: %w", pkg.Name, err)
		} else if err := validateLockedDependencies(pkg, seen); err != nil {
			return err
		} else {
			seen[pkg.Name] = true
		}
	}
	return nil
}

func validateLockedDependencies(pkg LockedPackage, earlier map[string]bool) error {
	seen := make(map[string]bool)
	for _, dependency := range pkg.Dependencies {
		if err := validateIdentifier("locked dependency", dependency); err != nil {
			return err
		} else if dependency == pkg.Name {
			return fmt.Errorf("validate locked package %q: expected another package, got self-dependency", pkg.Name)
		} else if seen[dependency] {
			return fmt.Errorf("validate locked package %q: expected dependency %q once, got duplicate", pkg.Name, dependency)
		} else if !earlier[dependency] {
			return fmt.Errorf("validate locked package %q: expected dependency %q earlier in lock order, got none", pkg.Name, dependency)
		} else {
			seen[dependency] = true
		}
	}
	return nil
}

func writeReceipt(app App, receipt Receipt) error {
	if err := validateReceipt(receipt); err != nil {
		return err
	} else {
		filename := receiptFilename(app, receipt.Name)
		data, marshalErr := json.MarshalIndent(receipt, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode receipt for package %q: expected serializable data, got error: %w", receipt.Name, marshalErr)
		} else {
			return writeBytesAtomically(app, filename, append(data, '\n'), "previous-receipt-"+receipt.Name)
		}
	}
}

func readReceipt(app App, packageName string) (Receipt, error) {
	filename := receiptFilename(app, packageName)
	data, err := readRegularFile(filename, "read receipt for package "+packageName)
	if err != nil {
		return Receipt{}, err
	} else {
		decoder, decoderErr := newStrictJSONDecoder(data, filename)
		if decoderErr != nil {
			return Receipt{}, decoderErr
		} else {
			var receipt Receipt
			if decodeErr := decoder.Decode(&receipt); decodeErr != nil {
				return Receipt{}, fmt.Errorf("decode receipt %q: expected the v1 schema, got error: %w", filename, decodeErr)
			} else if endErr := requireJSONEnd(decoder, filename); endErr != nil {
				return Receipt{}, endErr
			} else if validateErr := validateReceipt(receipt); validateErr != nil {
				return Receipt{}, validateErr
			} else {
				return receipt, nil
			}
		}
	}
}

func validateReceipt(receipt Receipt) error {
	if receipt.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("validate receipt for package %q: expected schema_version %d, got %d", receipt.Name, currentSchemaVersion, receipt.SchemaVersion)
	} else if err := validateIdentifier("receipt package name", receipt.Name); err != nil {
		return err
	} else if err := validateVersion("receipt version", receipt.Version); err != nil {
		return err
	} else if !supportedOS(receipt.OS) || !supportedArch(receipt.Arch) {
		return fmt.Errorf("validate receipt for package %q: expected supported platform, got %s/%s", receipt.Name, receipt.OS, receipt.Arch)
	} else if err := validateDownloadURL(receipt.URL, false); err != nil {
		return err
	} else if err := validateDigest(receipt.SHA256); err != nil {
		return err
	} else if err := validateRelativePath("receipt store_path", receipt.StorePath); err != nil {
		return err
	} else if err := validateUTCRFC3339("receipt installed_at for package "+receipt.Name, receipt.InstalledAt); err != nil {
		return err
	} else if len(receipt.BinDirs) == 0 || len(receipt.Executables) == 0 {
		return fmt.Errorf("validate receipt for package %q: expected binary directories and executables", receipt.Name)
	} else {
		return validateReceiptPaths(receipt)
	}
}

func validateReceiptPaths(receipt Receipt) error {
	seenBinDirs := make(map[string]bool)
	for _, binDir := range receipt.BinDirs {
		if err := validatePortableRelativePath("receipt bin_dirs", binDir, true); err != nil {
			return err
		} else if seenBinDirs[binDir] {
			return fmt.Errorf("validate receipt for package %q: expected binary directory %q once, got duplicate", receipt.Name, binDir)
		} else {
			seenBinDirs[binDir] = true
		}
	}
	seenExecutables := make(map[string]bool)
	for _, executable := range receipt.Executables {
		if err := validatePortableRelativePath("receipt executables", executable, false); err != nil {
			return err
		} else if seenExecutables[executable] {
			return fmt.Errorf("validate receipt for package %q: expected executable %q once, got duplicate", receipt.Name, executable)
		} else {
			seenExecutables[executable] = true
		}
	}
	return nil
}

func validateUTCRFC3339(field string, value string) error {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("validate %s: expected RFC 3339 UTC time, got %q: %w", field, value, err)
	} else {
		_, offset := parsed.Zone()
		if offset != 0 {
			return fmt.Errorf("validate %s: expected UTC offset zero, got %q", field, value)
		} else {
			return nil
		}
	}
}

func receiptFilename(app App, packageName string) string {
	return filepath.Join(app.Paths.ReceiptsRoot, packageName+".json")
}

func writeBytesAtomically(app App, filename string, data []byte, trashLabel string) error {
	if err := ensureContainedDirectory(app.Paths.BaseDir, filepath.Dir(filename)); err != nil {
		return fmt.Errorf("prepare directory for %q: expected writable parent, got error: %w", filename, err)
	} else {
		temporary, createErr := os.CreateTemp(filepath.Dir(filename), ".state-*")
		if createErr != nil {
			return fmt.Errorf("create temporary state for %q: expected writable parent, got error: %w", filename, createErr)
		} else {
			return finishAtomicWrite(app, temporary, filename, data, trashLabel)
		}
	}
}

func finishAtomicWrite(app App, temporary *os.File, filename string, data []byte, trashLabel string) error {
	temporaryName := temporary.Name()
	written, writeErr := temporary.Write(data)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		removeErr := os.Remove(temporaryName)
		return fmt.Errorf("write state file %q: expected complete synchronized data, got write=%v sync=%v close=%v cleanup=%v", filename, writeErr, syncErr, closeErr, removeErr)
	} else if written != len(data) {
		removeErr := os.Remove(temporaryName)
		return fmt.Errorf("write state file %q: expected %d bytes, wrote %d and cleanup result was %v", filename, len(data), written, removeErr)
	} else if err := os.Chmod(temporaryName, defaultFileMode); err != nil {
		removeErr := os.Remove(temporaryName)
		return fmt.Errorf("set state file %q permissions: expected mode %o, got error %v and cleanup result %v", filename, defaultFileMode, err, removeErr)
	} else {
		return publishStateFile(app, temporaryName, filename, trashLabel)
	}
}

func publishStateFile(app App, temporaryName string, filename string, trashLabel string) error {
	priorTrash := ""
	if _, err := os.Lstat(filename); err == nil {
		moved, moveErr := movePathToTrash(app, filename, trashLabel)
		if moveErr != nil {
			removeErr := os.Remove(temporaryName)
			return fmt.Errorf("preserve prior state file %q: expected trash move, got error %v and temporary cleanup result %v", filename, moveErr, removeErr)
		} else {
			priorTrash = moved
		}
	} else if !os.IsNotExist(err) {
		removeErr := os.Remove(temporaryName)
		return fmt.Errorf("inspect prior state file %q: expected file or no file, got error %v and temporary cleanup result %v", filename, err, removeErr)
	} else {
		priorTrash = ""
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		restoreErr := restorePriorState(priorTrash, filename)
		removeErr := os.Remove(temporaryName)
		return fmt.Errorf("publish state file %q: expected atomic rename, got error %v, restore result %v, cleanup result %v", filename, err, restoreErr, removeErr)
	} else {
		return nil
	}
}

func restorePriorState(priorTrash string, filename string) error {
	if priorTrash == "" {
		return nil
	} else {
		return os.Rename(priorTrash, filename)
	}
}

func movePathToTrash(app App, source string, label string) (string, error) {
	inside, containErr := pathIsInside(app.Paths.BaseDir, source)
	if containErr != nil {
		return "", containErr
	} else if !inside || filepath.Clean(source) == filepath.Clean(app.Paths.BaseDir) {
		return "", fmt.Errorf("move path %q to trash: expected a child of Portprog base %q", source, app.Paths.BaseDir)
	} else if err := ensureContainedDirectory(app.Paths.BaseDir, app.Paths.TrashRoot); err != nil {
		return "", fmt.Errorf("prepare trash directory %q: expected writable directory, got error: %w", app.Paths.TrashRoot, err)
	} else {
		destination, destinationErr := availableTrashPath(app, label)
		if destinationErr != nil {
			return "", destinationErr
		} else if err := os.Rename(source, destination); err != nil {
			return "", fmt.Errorf("move %q to recoverable trash %q: expected rename, got error: %w", source, destination, err)
		} else {
			return destination, nil
		}
	}
}

func availableTrashPath(app App, label string) (string, error) {
	safeLabel := sanitizeTrashLabel(label)
	prefix := app.Deps.Now().UTC().Format("20060102T150405.000000000Z") + "-" + safeLabel
	for index := 0; index < 1000; index++ {
		name := prefix
		if index == 0 {
			name = prefix
		} else {
			name = fmt.Sprintf("%s-%d", prefix, index)
		}
		candidate := filepath.Join(app.Paths.TrashRoot, name)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("inspect trash destination %q: expected path or no path, got error: %w", candidate, err)
		} else {
			continue
		}
	}
	return "", fmt.Errorf("choose trash destination for %q: expected a free name after 1000 attempts, got none", label)
}

func sanitizeTrashLabel(label string) string {
	var builder strings.Builder
	for _, character := range strings.ToLower(label) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '.' || character == '-' {
			builder.WriteRune(character)
		} else {
			builder.WriteRune('-')
		}
	}
	result := strings.Trim(builder.String(), "-.")
	if result == "" {
		return "item"
	} else {
		return result
	}
}
