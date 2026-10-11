package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func selfTest(parent Config) error {
	workspace, err := os.MkdirTemp("", "portprog-selftest-")
	if err != nil {
		return fmt.Errorf("create self-test workspace: %w", err)
	}
	if err := exerciseLocalInstallation(parent, workspace); err != nil {
		return fmt.Errorf("self-test failed; diagnostic files retained at %s: %w", workspace, err)
	}
	logSelfTest(parent.Output, workspace)
	return nil
}

func exerciseLocalInstallation(parent Config, workspace string) error {
	root := filepath.Join(workspace, "portable environment")
	b := configuration(root, defaultSettings(), parent.Output)
	if err := ensureInstallationDirectories(b); err != nil {
		return err
	}
	archive := filepath.Join(workspace, "fixture.zip")
	if err := createSelfTestArchive(archive); err != nil {
		return err
	}
	contents, err := os.ReadFile(archive)
	if err != nil {
		return fmt.Errorf("read self-test archive: %w", err)
	}
	hash := sha256.Sum256(contents)
	p := Package{ID: "selftest", Version: "1", Name: "selftest", Zip: "fixture.zip", Url: localFileURL(archive),
		Fetch: "web", Plan: "zipWithNoDirectory", Type: "application", OS: b.OS, Arch: b.Arch,
		BinDir: "bin", SHA256: hex.EncodeToString(hash[:])}
	locked, err := resolvePackage(b, p)
	if err != nil {
		return err
	}
	if err := installPackage(b, locked); err != nil {
		return err
	}
	lock := LockFile{SchemaVersion: 1, OS: b.OS, Arch: b.Arch, Profile: "selftest", Packages: []LockedPackage{locked}}
	if err := writeJSONAtomic(filepath.Join(root, "portprog.lock.json"), lock); err != nil {
		return err
	}
	if err := activateInstalled(b); err != nil {
		return err
	}
	bundle := filepath.Join(workspace, "offline bundle")
	if err := exportBundle(b, lock, bundle); err != nil {
		return err
	}
	offline := configuration(bundle, defaultSettings(), parent.Output)
	offline.Offline = true
	if err := installSelected(offline, commandOptions{}, false); err != nil {
		return fmt.Errorf("replay offline installation image: %w", err)
	}
	relocated := filepath.Join(workspace, "relocated environment")
	if err := os.Rename(root, relocated); err != nil {
		return fmt.Errorf("relocate self-test installation: %w", err)
	}
	b = configuration(relocated, defaultSettings(), parent.Output)
	b.Offline = true
	installed := filepath.Join(b.InstallDir, "selftest", "bin", "message.txt")
	if err := os.WriteFile(installed, []byte("damaged\n"), 0644); err != nil {
		return fmt.Errorf("create self-test damaged payload: %w", err)
	}
	if err := repairInstalled(b); err != nil {
		return err
	}
	if contents, err := os.ReadFile(installed); err != nil || string(contents) != "portable installation works\n" {
		return fmt.Errorf("self-test repair expected original payload at %s; received %q, error %v", installed, contents, err)
	}
	return activateInstalled(b)
}

func createSelfTestArchive(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("create self-test archive %s: %w", path, err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "bin/message.txt", Method: zip.Deflate}
	header.SetMode(0644)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return errors.Join(err, writer.Close(), file.Close())
	}
	_, writeErr := entry.Write([]byte("portable installation works\n"))
	return errors.Join(writeErr, writer.Close(), file.Close())
}

func localFileURL(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}
