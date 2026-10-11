package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func exportBundle(b Config, source LockFile, destination string) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve offline bundle destination: %w", err)
	}
	if err := validateRoot(destination); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("offline bundle destination %s must not exist; inspection result: %v", destination, err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return fmt.Errorf("create parent of bundle %s: %w", destination, err)
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".portprog-bundle-*")
	if err != nil {
		return fmt.Errorf("stage offline bundle %s: %w", destination, err)
	}
	if err := populateBundle(b, source, stage); err != nil {
		return fmt.Errorf("export offline bundle %s; incomplete files retained at %s: %w", destination, stage, err)
	}
	if err := os.Rename(stage, destination); err != nil {
		return fmt.Errorf("publish offline bundle from %s to %s: %w", stage, destination, err)
	}
	logBundle(b.Output, destination, len(source.Packages))
	return nil
}

func populateBundle(b Config, source LockFile, stage string) error {
	cache := filepath.Join(stage, "zips")
	if err := os.Mkdir(cache, 0755); err != nil {
		return fmt.Errorf("create bundle cache %s: %w", cache, err)
	}
	lock := LockFile{SchemaVersion: 1, OS: b.OS, Arch: b.Arch, Profile: source.Profile}
	for _, p := range source.Packages {
		image, err := bundleInstalledPackage(b, p, cache)
		if err != nil {
			return err
		}
		lock.Packages = append(lock.Packages, image)
	}
	if err := writeJSONAtomic(filepath.Join(stage, "source.lock.json"), source); err != nil {
		return err
	}
	if err := writeJSONAtomic(filepath.Join(stage, "portprog.lock.json"), lock); err != nil {
		return err
	}
	settings := defaultSettings()
	settings.Offline = true
	settings.MaxDownloadBytes = b.MaxDownloadBytes
	settings.MaxExtractBytes = b.MaxExtractBytes
	if err := writeJSONAtomic(filepath.Join(stage, "portprog.json"), settings); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable for offline bundle: %w", err)
	}
	name := "portprog"
	if b.OS == "windows" {
		name += ".exe"
	}
	if err := copyBundleFile(executable, filepath.Join(stage, name), 0755); err != nil {
		return err
	}
	launcher := "#!/bin/sh\nset -eu\nroot=$(CDPATH= cd -- \"$(dirname -- \"$0\")\" && pwd -L)\nexec \"$root/portprog\"\n"
	batch := "@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"%~dp0portprog.exe\"\r\nexit /b %errorlevel%\r\n"
	if err := os.WriteFile(filepath.Join(stage, "run.sh"), []byte(launcher), 0755); err != nil {
		return fmt.Errorf("write offline Unix launcher: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "run.bat"), []byte(batch), 0644); err != nil {
		return fmt.Errorf("write offline Windows launcher: %w", err)
	}
	return nil
}

func bundleInstalledPackage(b Config, locked LockedPackage, cache string) (LockedPackage, error) {
	installed := filepath.Join(b.InstallDir, locked.Package.Name)
	receipt, err := readReceipt(installed, b)
	if err != nil {
		return LockedPackage{}, err
	}
	if receipt.Package.SHA256 != locked.SHA256 || !packagesEqual(receipt.Package.Package, locked.Package) {
		return LockedPackage{}, fmt.Errorf("package %s installation does not match the selected lockfile", locked.Package.ID)
	}
	if err := verifyReceipt(installed, receipt); err != nil {
		return LockedPackage{}, err
	}
	archive, err := os.CreateTemp(cache, ".image-*")
	if err != nil {
		return LockedPackage{}, fmt.Errorf("create installation image for %s: %w", locked.Package.ID, err)
	}
	hash := sha256.New()
	writer := zip.NewWriter(io.MultiWriter(archive, hash))
	writeErr := writeInstalledImage(writer, installed, receipt)
	zipErr := writer.Close()
	syncErr := archive.Sync()
	stat, statErr := archive.Stat()
	closeErr := archive.Close()
	if err := errors.Join(writeErr, zipErr, syncErr, statErr, closeErr); err != nil {
		return LockedPackage{}, fmt.Errorf("archive installed package %s: %w", locked.Package.ID, err)
	}
	if stat.Size() > b.MaxDownloadBytes {
		return LockedPackage{}, fmt.Errorf("installation image for %s is %d bytes, exceeding bundle artifact limit %d", locked.Package.ID, stat.Size(), b.MaxDownloadBytes)
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	if err := os.Rename(archive.Name(), filepath.Join(cache, digest)); err != nil {
		return LockedPackage{}, fmt.Errorf("publish installation image for %s: %w", locked.Package.ID, err)
	}
	p := locked.Package
	p.Fetch, p.Plan = "web", "zipWithNoDirectory"
	p.Zip = p.ID + ".zip"
	p.Url = (&url.URL{Scheme: "file", Path: "/portprog-bundle/" + p.Zip}).String()
	p.OS, p.Arch, p.SHA256 = b.OS, b.Arch, digest
	p.ArchiveRoot, p.Branch, p.Command = "", "", ""
	p.Deletes = nil
	return LockedPackage{Package: p, SHA256: digest, Size: stat.Size()}, nil
}

func writeInstalledImage(writer *zip.Writer, root string, receipt Receipt) error {
	for _, entry := range receipt.Files {
		path := filepath.Join(root, filepath.FromSlash(entry.Path))
		stat, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect package %s image file %s: %w", receipt.Package.Package.ID, path, err)
		}
		header, err := zip.FileInfoHeader(stat)
		if err != nil {
			return fmt.Errorf("encode archive header for %s: %w", path, err)
		}
		header.Name = entry.Path
		header.Modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
		header.Method = zip.Deflate
		if stat.IsDir() {
			header.Name += "/"
		}
		output, err := writer.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create image entry %s: %w", path, err)
		}
		if entry.LinkTarget != "" {
			if _, err := io.WriteString(output, entry.LinkTarget); err != nil {
				return fmt.Errorf("write image symlink %s: %w", path, err)
			}
		} else if !stat.IsDir() {
			if err := copyVerifiedImageFile(path, entry.SHA256, output); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyVerifiedImageFile(path, expected string, output io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read image source %s: %w", path, err)
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(output, hash), file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("copy image source %s: %w", path, err)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != expected {
		return fmt.Errorf("image source %s changed while exporting: expected SHA-256 %s, received %s", path, expected, actual)
	}
	return nil
}

func copyBundleFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("read bundle input %s: %w", source, err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return errors.Join(fmt.Errorf("create bundle file %s: %w", destination, err), input.Close())
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	return errors.Join(copyErr, syncErr, input.Close(), output.Close())
}
