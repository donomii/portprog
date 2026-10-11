package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

func readReceipt(directory string, b Config) (Receipt, error) {
	path := filepath.Join(directory, ".portprog-receipt.json")
	receipt, err := readJSONDocument[Receipt](path)
	if err != nil {
		return Receipt{}, err
	}
	if receipt.SchemaVersion != 1 || receipt.Package.Package.Name != filepath.Base(directory) {
		return Receipt{}, fmt.Errorf("receipt %s must use schema 1 and own directory %s; received schema %d and directory %s", path, filepath.Base(directory), receipt.SchemaVersion, receipt.Package.Package.Name)
	}
	if err := validateLockedPackage(receipt.Package, b); err != nil {
		return Receipt{}, fmt.Errorf("receipt %s: %w", path, err)
	}
	seen := make(map[string]bool)
	for _, file := range receipt.Files {
		if !safeReceiptPath(file.Path) || seen[file.Path] || file.SHA256 != "" && !validDigest(file.SHA256) {
			return Receipt{}, fmt.Errorf("receipt %s has invalid or repeated file record %+v", path, file)
		}
		seen[file.Path] = true
	}
	return receipt, nil
}

func safeReceiptPath(path string) bool {
	return path != "" && path != "." && !filepath.IsAbs(path) && !strings.ContainsAny(path, "\\\r\n\x00:") &&
		path != ".." && !strings.HasPrefix(path, "../") && filepath.ToSlash(filepath.Clean(path)) == path
}

func installedReceipts(b Config) ([]Receipt, error) {
	entries, err := os.ReadDir(b.InstallDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read installed packages in %s: %w", b.InstallDir, err)
	}
	var receipts []Receipt
	seen := make(map[string]string)
	for _, entry := range entries {
		path := filepath.Join(b.InstallDir, entry.Name())
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected installed entry %s; expected a package directory with an installation receipt", path)
		}
		receipt, err := readReceipt(path, b)
		if errors.Is(err, os.ErrNotExist) {
			logUnmanaged(b.Output, path)
			continue
		}
		if err != nil {
			return nil, err
		}
		id := receipt.Package.Package.ID
		if previous, exists := seen[id]; exists {
			return nil, fmt.Errorf("package ID %s has receipts in both %s and %s", id, previous, path)
		}
		seen[id] = path
		receipts = append(receipts, receipt)
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Package.Package.ID < receipts[j].Package.Package.ID })
	return receipts, nil
}

func verifyReceipt(directory string, receipt Receipt) error {
	actual, err := collectInstalledFiles(directory)
	if err != nil {
		return fmt.Errorf("inspect installed package %s at %s: %w", receipt.Package.Package.ID, directory, err)
	}
	byPath := make(map[string]InstalledFile, len(actual))
	for _, file := range actual {
		byPath[file.Path] = file
	}
	for _, expected := range receipt.Files {
		received, exists := byPath[expected.Path]
		if !exists {
			return fmt.Errorf("package %s is missing %s", receipt.Package.Package.ID, filepath.Join(directory, expected.Path))
		}
		if !reflect.DeepEqual(expected, received) {
			return fmt.Errorf("package %s file %s differs: expected %+v, received %+v", receipt.Package.Package.ID, expected.Path, expected, received)
		}
		delete(byPath, expected.Path)
	}
	if len(byPath) != 0 {
		extra := make([]string, 0, len(byPath))
		for path := range byPath {
			extra = append(extra, path)
		}
		sort.Strings(extra)
		return fmt.Errorf("package %s contains unrecorded files: %s", receipt.Package.Package.ID, strings.Join(extra, ", "))
	}
	return nil
}

func activateInstalled(b Config) error {
	receipts, err := installedReceipts(b)
	if err != nil {
		return err
	}
	packages := make([]LockedPackage, 0, len(receipts))
	for _, receipt := range receipts {
		if err := verifyReceipt(filepath.Join(b.InstallDir, receipt.Package.Package.Name), receipt); err != nil {
			return err
		}
		packages = append(packages, receipt.Package)
	}
	if err := writeEnvironment(b, packages); err != nil {
		return err
	}
	logActivation(b.Output, b.RootDir, len(packages))
	return nil
}

func listInstalled(b Config) error {
	receipts, err := installedReceipts(b)
	if err != nil {
		return err
	}
	var failures []error
	for _, receipt := range receipts {
		err := verifyReceipt(filepath.Join(b.InstallDir, receipt.Package.Package.Name), receipt)
		logReceipt(b.Output, receipt, err)
		if err != nil {
			failures = append(failures, err)
		}
	}
	if len(receipts) == 0 {
		logNoInstalled(b.Output, b.InstallDir)
	}
	return errors.Join(failures...)
}

func repairInstalled(b Config) error {
	receipts, err := installedReceipts(b)
	if err != nil {
		return err
	}
	for _, receipt := range receipts {
		err := verifyReceipt(filepath.Join(b.InstallDir, receipt.Package.Package.Name), receipt)
		logReceipt(b.Output, receipt, err)
		if err != nil {
			if err := installPackage(b, receipt.Package); err != nil {
				return fmt.Errorf("repair %s from recorded artifact %s: %w", receipt.Package.Package.ID, receipt.Package.SHA256, err)
			}
			logInstalled(b.Output, receipt.Package.Package)
		}
	}
	return activateInstalled(b)
}

func removeInstalled(b Config, id string) error {
	if !safeIdentifier(id) {
		return fmt.Errorf("invalid package ID %q", id)
	}
	receipts, err := installedReceipts(b)
	if err != nil {
		return err
	}
	var target *Receipt
	for index := range receipts {
		receipt := &receipts[index]
		if receipt.Package.Package.ID == id {
			target = receipt
		}
		for _, dependency := range receipt.Package.Package.Dependencies {
			if dependency == id {
				return fmt.Errorf("cannot remove %s while installed package %s depends on it", id, receipt.Package.Package.ID)
			}
		}
	}
	if target == nil {
		return fmt.Errorf("package %s has no installation receipt under %s", id, b.InstallDir)
	}
	retired := filepath.Join(b.RootDir, "retired")
	if err := os.MkdirAll(retired, 0755); err != nil {
		return fmt.Errorf("create retired-package directory %s: %w", retired, err)
	}
	destination := filepath.Join(retired, id+"-"+time.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.Rename(filepath.Join(b.InstallDir, target.Package.Package.Name), destination); err != nil {
		return fmt.Errorf("retire installed package %s to %s: %w", id, destination, err)
	}
	logRetired(b.Output, id, destination)
	return activateInstalled(b)
}
