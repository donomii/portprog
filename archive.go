package main

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

type extractionBudget struct {
	maximum int64
	written int64
}

const maximumArchiveEntries = 100000

func extractAsset(source string, target string, asset PackageAsset, maximumBytes int64) error {
	if maximumBytes <= 0 {
		return fmt.Errorf("extract asset %q: expected positive extraction limit, got %d", source, maximumBytes)
	} else {
		if err := prepareExtractionTarget(target); err != nil {
			return fmt.Errorf("prepare extraction target for asset %q: %w", source, err)
		} else {
			budget := &extractionBudget{maximum: maximumBytes}
			var extractErr error
			switch asset.Format {
			case "raw":
				extractErr = extractRaw(source, target, asset, budget)
			case "zip":
				extractErr = extractZip(source, target, asset, budget)
			case "tar.gz", "tar.bz2", "tar.xz":
				extractErr = extractTar(source, target, asset, budget)
			default:
				extractErr = fmt.Errorf("expected supported asset format, got %q", asset.Format)
			}
			if extractErr != nil {
				return fmt.Errorf("extract %s asset %q into %q: %w", asset.Format, source, target, extractErr)
			} else {
				return verifyExtractedLayout(target, asset)
			}
		}
	}
}

func prepareExtractionTarget(target string) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		if mkdirErr := os.MkdirAll(target, defaultDirMode); mkdirErr != nil {
			return fmt.Errorf("create extraction target %q: expected writable directory, got error: %w", target, mkdirErr)
		} else {
			return nil
		}
	} else if err != nil {
		return fmt.Errorf("inspect extraction target %q: expected directory or no path, got error: %w", target, err)
	} else if !info.IsDir() {
		return fmt.Errorf("inspect extraction target %q: expected directory, got mode %v", target, info.Mode())
	} else {
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return fmt.Errorf("inspect extraction target %q: expected readable empty directory, got error: %w", target, readErr)
		} else if len(entries) != 0 {
			return fmt.Errorf("inspect extraction target %q: expected empty directory, got %d entries", target, len(entries))
		} else {
			return nil
		}
	}
}

func extractRaw(source string, target string, asset PackageAsset, budget *extractionBudget) error {
	destination, err := containedPortablePath(target, asset.RawPath)
	if err != nil {
		return err
	} else if err := os.MkdirAll(filepath.Dir(destination), defaultDirMode); err != nil {
		return fmt.Errorf("create raw asset parent %q: expected writable directory, got error: %w", filepath.Dir(destination), err)
	} else {
		return copyRegularFile(source, destination, executableFileMode, budget)
	}
}

func extractZip(source string, target string, asset PackageAsset, budget *extractionBudget) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return fmt.Errorf("open ZIP archive: expected readable ZIP, got error: %w", err)
	} else {
		seen := make(map[string]bool)
		for index, entry := range reader.File {
			if index >= maximumArchiveEntries {
				closeErr := reader.Close()
				return fmt.Errorf("extract ZIP archive: expected at most %d entries, got more; close result: %v", maximumArchiveEntries, closeErr)
			} else if entryErr := extractZipEntry(entry, target, asset.StripComponents, budget, seen); entryErr != nil {
				closeErr := reader.Close()
				return fmt.Errorf("extract ZIP entry %q: %v; close result: %v", entry.Name, entryErr, closeErr)
			} else {
				continue
			}
		}
		if closeErr := reader.Close(); closeErr != nil {
			return fmt.Errorf("close ZIP archive %q: expected success, got error: %w", source, closeErr)
		} else {
			return nil
		}
	}
}

func extractZipEntry(entry *zip.File, target string, stripComponents int, budget *extractionBudget, seen map[string]bool) error {
	relative, skipped, err := strippedArchivePath(entry.Name, stripComponents)
	if err != nil {
		return err
	} else if skipped {
		return nil
	} else if seen[relative] {
		return fmt.Errorf("expected unique extracted path, got duplicate %q", relative)
	} else {
		seen[relative] = true
		destination, destinationErr := containedPortablePath(target, relative)
		mode := entry.Mode()
		if destinationErr != nil {
			return destinationErr
		} else if mode&os.ModeSymlink != 0 || mode&os.ModeType != 0 && !mode.IsDir() {
			return fmt.Errorf("expected regular file or directory, got mode %v", mode)
		} else if mode.IsDir() {
			return os.MkdirAll(destination, defaultDirMode)
		} else {
			return copyZipFile(entry, destination, mode, budget)
		}
	}
}

func copyZipFile(entry *zip.File, destination string, mode os.FileMode, budget *extractionBudget) error {
	reader, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open compressed file: expected readable entry, got error: %w", err)
	} else {
		copyErr := writeExtractedFile(reader, destination, sanitizedFileMode(mode), budget)
		closeErr := reader.Close()
		if copyErr != nil {
			return copyErr
		} else if closeErr != nil {
			return fmt.Errorf("close compressed file %q: expected success, got error: %w", entry.Name, closeErr)
		} else {
			return nil
		}
	}
}

func extractTar(source string, target string, asset PackageAsset, budget *extractionBudget) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open tar archive: expected readable file, got error: %w", err)
	} else {
		reader, closeCompression, readerErr := tarStream(file, asset.Format)
		if readerErr != nil {
			closeErr := file.Close()
			return fmt.Errorf("prepare tar archive %q: %w; file close result: %v", source, readerErr, closeErr)
		} else {
			extractErr := extractTarEntries(tar.NewReader(reader), target, asset.StripComponents, budget)
			compressionErr := closeCompression()
			fileErr := file.Close()
			if extractErr != nil {
				return extractErr
			} else if compressionErr != nil {
				return compressionErr
			} else if fileErr != nil {
				return fmt.Errorf("close tar archive %q: expected success, got error: %w", source, fileErr)
			} else {
				return nil
			}
		}
	}
}

func tarStream(file *os.File, format string) (io.Reader, func() error, error) {
	switch format {
	case "tar.gz":
		reader, err := gzip.NewReader(file)
		if err != nil {
			return nil, nil, fmt.Errorf("open gzip stream: expected valid gzip data, got error: %w", err)
		} else {
			return reader, reader.Close, nil
		}
	case "tar.bz2":
		return bzip2.NewReader(file), func() error { return nil }, nil
	case "tar.xz":
		reader, err := xz.NewReader(file)
		if err != nil {
			return nil, nil, fmt.Errorf("open xz stream: expected valid xz data, got error: %w", err)
		} else {
			return reader, func() error { return nil }, nil
		}
	default:
		return nil, nil, fmt.Errorf("open tar stream: expected tar.gz, tar.bz2, or tar.xz, got %q", format)
	}
}

func extractTarEntries(reader *tar.Reader, target string, stripComponents int, budget *extractionBudget) error {
	seen := make(map[string]bool)
	entryCount := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("read tar entry: expected complete archive, got error: %w", err)
		} else if entryCount >= maximumArchiveEntries {
			return fmt.Errorf("extract tar archive: expected at most %d entries, got more", maximumArchiveEntries)
		} else if entryErr := extractTarEntry(reader, header, target, stripComponents, budget, seen); entryErr != nil {
			return fmt.Errorf("extract tar entry %q: %w", header.Name, entryErr)
		} else {
			entryCount++
			continue
		}
	}
}

func extractTarEntry(reader *tar.Reader, header *tar.Header, target string, stripComponents int, budget *extractionBudget, seen map[string]bool) error {
	relative, skipped, err := strippedArchivePath(header.Name, stripComponents)
	if err != nil {
		return err
	} else if skipped {
		return nil
	} else if seen[relative] {
		return fmt.Errorf("expected unique extracted path, got duplicate %q", relative)
	} else {
		seen[relative] = true
		destination, destinationErr := containedPortablePath(target, relative)
		if destinationErr != nil {
			return destinationErr
		} else {
			switch header.Typeflag {
			case tar.TypeDir:
				return os.MkdirAll(destination, defaultDirMode)
			case tar.TypeReg, tar.TypeRegA:
				return writeExtractedFile(reader, destination, sanitizedFileMode(os.FileMode(header.Mode)), budget)
			default:
				return fmt.Errorf("expected regular file or directory, got tar type %d", header.Typeflag)
			}
		}
	}
}

func strippedArchivePath(name string, stripComponents int) (string, bool, error) {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", false, fmt.Errorf("expected portable relative archive path, got %q", name)
	} else {
		cleaned := path.Clean(name)
		if cleaned == "." {
			return "", true, nil
		} else if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return "", false, fmt.Errorf("expected archive path without parent traversal, got %q", name)
		} else {
			components := strings.Split(cleaned, "/")
			if len(components) <= stripComponents {
				return "", true, nil
			} else {
				return strings.Join(components[stripComponents:], "/"), false, nil
			}
		}
	}
}

func containedPortablePath(root string, relative string) (string, error) {
	if err := validatePortableRelativePath("archive path", relative, false); err != nil {
		return "", err
	} else {
		candidate := filepath.Join(root, filepath.FromSlash(relative))
		inside, containErr := pathIsInside(root, candidate)
		if containErr != nil {
			return "", containErr
		} else if !inside {
			return "", fmt.Errorf("resolve archive path %q: expected destination inside %q, got %q", relative, root, candidate)
		} else if err := rejectExistingSymlinkComponents(root, candidate); err != nil {
			return "", err
		} else {
			return candidate, nil
		}
	}
}

func writeExtractedFile(reader io.Reader, destination string, mode os.FileMode, budget *extractionBudget) error {
	if err := os.MkdirAll(filepath.Dir(destination), defaultDirMode); err != nil {
		return fmt.Errorf("create extracted file parent %q: expected writable directory, got error: %w", filepath.Dir(destination), err)
	} else {
		file, createErr := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if createErr != nil {
			return fmt.Errorf("create extracted file %q: expected new regular file, got error: %w", destination, createErr)
		} else {
			return copyWithBudget(reader, file, destination, budget)
		}
	}
}

func copyRegularFile(source string, destination string, mode os.FileMode, budget *extractionBudget) error {
	reader, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open raw asset %q: expected readable file, got error: %w", source, err)
	} else {
		file, createErr := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if createErr != nil {
			closeErr := reader.Close()
			return fmt.Errorf("create raw asset %q: expected new regular file, got error %v and source close result %v", destination, createErr, closeErr)
		} else {
			copyErr := copyWithBudget(reader, file, destination, budget)
			readerCloseErr := reader.Close()
			if copyErr != nil {
				return copyErr
			} else if readerCloseErr != nil {
				return fmt.Errorf("close raw asset %q: expected success, got error: %w", source, readerCloseErr)
			} else {
				return nil
			}
		}
	}
}

func copyWithBudget(reader io.Reader, file *os.File, destination string, budget *extractionBudget) error {
	remaining := budget.maximum - budget.written
	written, copyErr := io.Copy(file, io.LimitReader(reader, remaining+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	budget.written += written
	if copyErr != nil {
		return fmt.Errorf("write extracted file %q: expected complete data, got copy error %v, sync result %v, close result %v", destination, copyErr, syncErr, closeErr)
	} else if syncErr != nil || closeErr != nil {
		return fmt.Errorf("finish extracted file %q: expected synchronized close, got sync=%v close=%v", destination, syncErr, closeErr)
	} else if budget.written > budget.maximum {
		return fmt.Errorf("extract archive: expected at most configured maximum of %d bytes, got at least %d", budget.maximum, budget.written)
	} else {
		return nil
	}
}

func sanitizedFileMode(mode os.FileMode) os.FileMode {
	if mode.Perm()&0o111 != 0 {
		return executableFileMode
	} else {
		return defaultFileMode
	}
}

func verifyExtractedLayout(target string, asset PackageAsset) error {
	for _, relative := range asset.BinDirs {
		var directory string
		var err error
		if relative == "." {
			directory = target
		} else {
			directory, err = containedPortablePath(target, relative)
		}
		if err != nil {
			return err
		} else {
			info, statErr := os.Lstat(directory)
			if statErr != nil {
				return fmt.Errorf("verify extracted binary directory %q: expected directory, got error: %w", directory, statErr)
			} else if !info.IsDir() {
				return fmt.Errorf("verify extracted binary directory %q: expected directory, got mode %v", directory, info.Mode())
			} else {
				continue
			}
		}
	}
	for _, relative := range asset.Executables {
		filename, err := containedPortablePath(target, relative)
		if err != nil {
			return err
		} else {
			info, statErr := os.Lstat(filename)
			if statErr != nil {
				return fmt.Errorf("verify extracted executable %q: expected regular file, got error: %w", filename, statErr)
			} else if !info.Mode().IsRegular() {
				return fmt.Errorf("verify extracted executable %q: expected regular file, got mode %v", filename, info.Mode())
			} else if err := os.Chmod(filename, executableFileMode); err != nil {
				return fmt.Errorf("make extracted executable %q runnable: expected mode %o, got error: %w", filename, executableFileMode, err)
			} else {
				continue
			}
		}
	}
	return nil
}
