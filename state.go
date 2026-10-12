package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

func defaultSettings() Settings {
	return Settings{SchemaVersion: 1, DefaultProfile: "default", HTTPTimeoutSecs: 120,
		MaxDownloadBytes: 8 << 30, MaxExtractBytes: 16 << 30}
}

func loadSettings(root string) (Settings, error) {
	path := filepath.Join(root, "portprog.json")
	settings, err := readJSONDocument[Settings](path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	if settings.SchemaVersion != 1 || settings.HTTPTimeoutSecs <= 0 || settings.HTTPTimeoutSecs > 86400 ||
		settings.MaxDownloadBytes <= 0 || settings.MaxExtractBytes <= 0 || !safeIdentifier(settings.DefaultProfile) {
		return Settings{}, fmt.Errorf("settings %s require schema_version=1, a simple default_profile, timeout 1..86400 seconds and positive byte limits; received %+v", path, settings)
	}
	return settings, nil
}

func readJSONDocument[T Settings | LockFile | Receipt | Profile | Package](path string) (T, error) {
	var result T
	file, err := os.Open(path)
	if err != nil {
		return result, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return result, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !stat.Mode().IsRegular() || stat.Size() > 32<<20 {
		return result, fmt.Errorf("JSON document %s must be a regular file no larger than 32 MiB; received mode %s and %d bytes", path, stat.Mode(), stat.Size())
	}
	contents, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil {
		return result, fmt.Errorf("read JSON document %s: %w", path, err)
	}
	if len(contents) > 32<<20 {
		return result, fmt.Errorf("JSON document %s grew beyond the 32 MiB limit while reading", path)
	}
	if err := validateJSONKeys(contents); err != nil {
		return result, fmt.Errorf("validate JSON document %s: %w", path, err)
	}
	if err := validateDocumentFields(contents, reflect.TypeOf(result)); err != nil {
		return result, fmt.Errorf("validate JSON fields in %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return result, fmt.Errorf("JSON document %s must contain exactly one value; trailing decode result: %v", path, err)
	}
	return result, nil
}

func writeJSONAtomic[T Settings | LockFile | Receipt | Profile](path string, document T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create parent for %s: %w", path, err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".portprog-json-*")
	if err != nil {
		return fmt.Errorf("stage JSON document %s: %w", path, err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	writeErr := encoder.Encode(document)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write %s via retained staging file %s: %w", path, file.Name(), err)
	}
	return publishFile(file.Name(), path)
}

func publishFile(staged, destination string) error {
	backup := ""
	if _, err := os.Lstat(destination); err == nil {
		backup = destination + ".previous-" + time.Now().UTC().Format("20060102T150405.000000000")
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("preserve previous %s at %s: %w", destination, backup, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect destination %s: %w", destination, err)
	}
	if err := os.Rename(staged, destination); err != nil {
		if backup != "" {
			return errors.Join(fmt.Errorf("publish %s: %w", destination, err), os.Rename(backup, destination))
		}
		return fmt.Errorf("publish %s: %w", destination, err)
	}
	return nil
}

func safeIdentifier(value string) bool {
	return packageComponent(value)
}

func validateLockedPackage(p LockedPackage, b Config) error {
	if err := validatePackage(p.Package); err != nil {
		return fmt.Errorf("locked package %s: %w", p.Package.ID, err)
	}
	if !validDigest(p.SHA256) || p.Size < 0 || p.Size > b.MaxDownloadBytes {
		return fmt.Errorf("locked package %s needs lowercase SHA-256 and size between 0 and %d bytes; received digest %q and size %d", p.Package.ID, b.MaxDownloadBytes, p.SHA256, p.Size)
	}
	if p.Package.SHA256 != "" && p.Package.SHA256 != p.SHA256 {
		return fmt.Errorf("locked package %s digest %s differs from recipe digest %s", p.Package.ID, p.SHA256, p.Package.SHA256)
	}
	if p.Package.Fetch == "git" {
		commit, err := hex.DecodeString(p.Commit)
		if err != nil || len(commit) != 20 && len(commit) != 32 {
			return fmt.Errorf("locked Git package %s requires an exact commit, received %q", p.Package.ID, p.Commit)
		}
	}
	if p.Package.OS != "all" && p.Package.OS != b.OS || p.Package.Arch != "all" && p.Package.Arch != b.Arch {
		return fmt.Errorf("package %s targets %s/%s, installation targets %s/%s", p.Package.ID, p.Package.OS, p.Package.Arch, b.OS, b.Arch)
	}
	return nil
}

func readLock(path string, b Config) (LockFile, error) {
	lock, err := readJSONDocument[LockFile](path)
	if err != nil {
		return LockFile{}, err
	}
	if lock.SchemaVersion != 1 || lock.OS != b.OS || lock.Arch != b.Arch {
		return LockFile{}, fmt.Errorf("lockfile %s requires schema 1 and target %s/%s; received schema %d and %s/%s", path, b.OS, b.Arch, lock.SchemaVersion, lock.OS, lock.Arch)
	}
	seen := make(map[string]bool)
	targets := make(map[string]string)
	for _, p := range lock.Packages {
		if err := validateLockedPackage(p, b); err != nil {
			return LockFile{}, err
		}
		if seen[p.Package.ID] {
			return LockFile{}, fmt.Errorf("lockfile %s repeats package ID %s", path, p.Package.ID)
		}
		name := strings.ToLower(p.Package.Name)
		if owner, exists := targets[name]; exists {
			return LockFile{}, fmt.Errorf("packages %s and %s both own install directory %s", owner, p.Package.ID, p.Package.Name)
		}
		for _, dependency := range p.Package.Dependencies {
			if !seen[dependency] {
				return LockFile{}, fmt.Errorf("locked package %s requires preceding dependency %s", p.Package.ID, dependency)
			}
		}
		seen[p.Package.ID] = true
		targets[name] = p.Package.ID
	}
	return lock, nil
}

func packagesEqual(left, right Package) bool {
	return reflect.DeepEqual(left, right)
}

func withInstallationLock(b Config, action func() error) error {
	if err := os.MkdirAll(b.RootDir, 0755); err != nil {
		return fmt.Errorf("create installation root %s: %w", b.RootDir, err)
	}
	path := filepath.Join(b.RootDir, ".portprog-operation.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("reserve installation %s using %s; another operation or an interrupted operation may own this file: %w", b.RootDir, path, err)
	}
	_, writeErr := fmt.Fprintf(file, "Portprog operation started %s\n", time.Now().UTC().Format(time.RFC3339))
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("record installation operation in %s: %w", path, err)
	}
	actionErr := action()
	releaseErr := os.Remove(path)
	if releaseErr != nil {
		releaseErr = fmt.Errorf("release installation reservation %s: %w", path, releaseErr)
	}
	return errors.Join(actionErr, releaseErr)
}
