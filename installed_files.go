package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func collectInstalledFiles(root string) ([]InstalledFile, error) {
	files := make([]InstalledFile, 0)
	err := filepath.WalkDir(root, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("installation root %q must be a real directory", root)
			}
			return nil
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		if relative == ".portprog-receipt.json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		record := InstalledFile{Path: filepath.ToSlash(relative), Mode: uint32(info.Mode())}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(filename)
			if err != nil {
				return err
			}
			if err := validateLink(root, relative, target); err != nil {
				return err
			}
			if err := resolveContainedPath(root, relative); err != nil {
				return err
			}
			hash := sha256.Sum256([]byte(target))
			record.LinkTarget, record.SHA256 = target, hex.EncodeToString(hash[:])
		case info.Mode().IsRegular():
			file, err := os.Open(filename)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			record.SHA256 = hex.EncodeToString(hash.Sum(nil))
		case info.IsDir():
		default:
			return fmt.Errorf("installed file %q has unsupported mode %s", filename, info.Mode())
		}
		files = append(files, record)
		return nil
	})
	return files, err
}

func copyInstallTree(source, destination string, exclusions []string, limit int64) error {
	excluded := make(map[string]bool)
	for _, name := range exclusions {
		if _, err := archivePath(source, name); err != nil {
			return fmt.Errorf("invalid package exclusion: %w", err)
		}
		if name == "." || name == "" {
			return fmt.Errorf("package exclusion %q would remove the entire installation", name)
		}
		excluded[filepath.Clean(filepath.FromSlash(name))] = true
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	sink := &archiveSink{root: destination, limit: limit, entries: make(map[string]bool)}
	err := filepath.WalkDir(source, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == source {
			return nil
		}
		relative, err := filepath.Rel(source, filename)
		if err != nil {
			return err
		}
		if excluded[relative] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filename)
			if err != nil {
				return err
			}
			return sink.add(name, info.Mode(), int64(len(target)), strings.NewReader(""), target, false)
		}
		if info.IsDir() {
			return sink.add(name, info.Mode(), 0, strings.NewReader(""), "", false)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source %q has unsupported mode %s", filename, info.Mode())
		}
		file, err := os.Open(filename)
		if err != nil {
			return err
		}
		err = sink.add(name, info.Mode(), info.Size(), file, "", false)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return err
	}
	return sink.finish()
}

func copyArtifactFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
