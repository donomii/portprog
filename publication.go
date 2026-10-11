package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func publishDocuments(root string, documents map[string][]byte) error {
	names := make([]string, 0, len(documents))
	for name := range documents {
		if !safeIdentifier(name) {
			return fmt.Errorf("invalid document filename %q", name)
		}
		destination := filepath.Join(root, name)
		if stat, err := os.Lstat(destination); err == nil && !stat.Mode().IsRegular() {
			return fmt.Errorf("document destination %s must be a regular file; received %s", destination, stat.Mode())
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect document destination %s: %w", destination, err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	staged := make(map[string]string)
	for _, name := range names {
		file, err := os.CreateTemp(root, ".portprog-document-*")
		if err != nil {
			return fmt.Errorf("stage document %s: %w", name, err)
		}
		_, writeErr := file.Write(documents[name])
		modeErr := file.Chmod(0644)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, modeErr, syncErr, closeErr); err != nil {
			return fmt.Errorf("stage document %s in retained file %s: %w", name, file.Name(), err)
		}
		staged[name] = file.Name()
	}
	backups := make(map[string]string)
	published := make(map[string]bool)
	for _, name := range names {
		destination := filepath.Join(root, name)
		if _, err := os.Lstat(destination); err == nil {
			backup := destination + ".previous-" + time.Now().UTC().Format("20060102T150405.000000000")
			if err := os.Rename(destination, backup); err != nil {
				return errors.Join(fmt.Errorf("preserve document %s: %w", destination, err), rollbackDocuments(root, names, staged, backups, published))
			}
			backups[name] = backup
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.Join(err, rollbackDocuments(root, names, staged, backups, published))
		}
		if err := os.Rename(staged[name], destination); err != nil {
			return errors.Join(fmt.Errorf("publish document %s: %w", destination, err), rollbackDocuments(root, names, staged, backups, published))
		}
		published[name] = true
	}
	return nil
}

func rollbackDocuments(root string, names []string, staged, backups map[string]string, published map[string]bool) error {
	var failures []error
	for index := len(names) - 1; index >= 0; index-- {
		name := names[index]
		destination := filepath.Join(root, name)
		if published[name] {
			if err := os.Rename(destination, staged[name]); err != nil {
				failures = append(failures, fmt.Errorf("retain failed document %s at %s: %w", destination, staged[name], err))
				continue
			}
		}
		if backup := backups[name]; backup != "" {
			if err := os.Rename(backup, destination); err != nil {
				failures = append(failures, fmt.Errorf("restore document %s from %s: %w", destination, backup, err))
			}
		}
	}
	return errors.Join(failures...)
}
