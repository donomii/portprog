package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runSelfTest(output io.Writer) error {
	tempDir, err := os.MkdirTemp("", "portprog-selftest-*")
	if err != nil {
		return fmt.Errorf("create built-in test directory: expected writable temporary storage, got error: %w", err)
	} else {
		defer cleanupSelfTest(tempDir, output)
		if _, err := parseSettings(strings.NewReader(validSelfTestSettings()), tempDir); err != nil {
			return fmt.Errorf("built-in configuration test: expected valid documented settings, got %w", err)
		} else {
			fmt.Fprintln(output, "PASS configuration: documented settings parse strictly.")
		}
		if err := validatePackageManifest(selfTestManifest()); err != nil {
			return fmt.Errorf("built-in package test: expected valid manifest, got %w", err)
		} else {
			fmt.Fprintln(output, "PASS package schema: platform asset and checksum rules hold.")
		}
		if _, err := containedPortablePath(tempDir, "../escape"); err == nil {
			return fmt.Errorf("built-in path test: expected parent traversal rejection, got success")
		} else {
			fmt.Fprintln(output, "PASS path safety: parent traversal is rejected.")
		}
		if err := runSelfTestArchive(tempDir); err != nil {
			return err
		} else {
			fmt.Fprintln(output, "PASS archive safety: contained ZIP extraction succeeds and verifies its executable.")
			fmt.Fprintln(output, "Built-in tests passed without network access or project-state changes.")
			return nil
		}
	}
}

func validSelfTestSettings() string {
	return `schema_version = 1
default_profile = starter
install_root = portable
cache_root = cache
state_root = state
trash_root = trash
catalog_dir = catalog
profiles_dir = profiles
lock_file = portprog.lock.json
offline = false
max_download_mb = 64
max_extract_mb = 256
download_timeout_seconds = 30
`
}

func selfTestManifest() PackageManifest {
	return PackageManifest{
		SchemaVersion: currentSchemaVersion,
		Name:          "self-test", Version: "1.0.0", Description: "Offline built-in test package.",
		Homepage: "https://example.com/self-test", License: "MIT", Dependencies: []string{},
		Assets: []PackageAsset{{
			OS: "darwin", Arch: "arm64", URL: "https://example.com/self-test.zip",
			SHA256: strings.Repeat("0", sha256HexLength), Size: 1, Format: "zip",
			StripComponents: 1, RawPath: "", BinDirs: []string{"bin"},
			Executables: []string{"bin/self-test"},
		}},
	}
}

func runSelfTestArchive(tempDir string) error {
	archivePath := filepath.Join(tempDir, "fixture.zip")
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	header := &zip.FileHeader{Name: "fixture/bin/self-test", Method: zip.Deflate}
	header.SetMode(executableFileMode)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create built-in ZIP entry: expected success, got error: %w", err)
	} else if _, err := entry.Write([]byte("self-test")); err != nil {
		return fmt.Errorf("write built-in ZIP entry: expected success, got error: %w", err)
	} else if err := writer.Close(); err != nil {
		return fmt.Errorf("close built-in ZIP: expected success, got error: %w", err)
	} else if err := os.WriteFile(archivePath, data.Bytes(), defaultFileMode); err != nil {
		return fmt.Errorf("save built-in ZIP %q: expected success, got error: %w", archivePath, err)
	} else {
		asset := PackageAsset{Format: "zip", StripComponents: 1, BinDirs: []string{"bin"}, Executables: []string{"bin/self-test"}}
		return extractAsset(archivePath, filepath.Join(tempDir, "extracted"), asset, bytesPerMebibyte)
	}
}

func cleanupSelfTest(tempDir string, output io.Writer) {
	if !strings.HasPrefix(filepath.Base(tempDir), "portprog-selftest-") {
		fmt.Fprintf(output, "Self-test cleanup skipped: expected a Portprog temporary directory, got %q.\n", tempDir)
	} else {
		paths := []string{
			filepath.Join(tempDir, "extracted", "bin", "self-test"),
			filepath.Join(tempDir, "extracted", "bin"),
			filepath.Join(tempDir, "extracted"),
			filepath.Join(tempDir, "fixture.zip"),
			tempDir,
		}
		failures := make([]string, 0)
		for _, filename := range paths {
			if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
				failures = append(failures, fmt.Sprintf("%s: %v", filename, err))
			} else {
				continue
			}
		}
		if len(failures) != 0 {
			fmt.Fprintf(output, "Self-test cleanup warning: expected to remove known temporary files, got %s.\n", strings.Join(failures, "; "))
		} else {
			return
		}
	}
}
