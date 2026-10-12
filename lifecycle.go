package main

import (
	"encoding/json"
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
	if receipt.Root != "" && !validRelocationRoot(receipt.Root) {
		return Receipt{}, fmt.Errorf("receipt %s has invalid installation root %q", path, receipt.Root)
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

func checkInstalledOwnership(b Config, packages []Package) error {
	receipts, err := installedReceipts(b)
	if err != nil {
		return err
	}
	for _, selected := range packages {
		for _, receipt := range receipts {
			installed := receipt.Package.Package
			if selected.ID == installed.ID && selected.Name != installed.Name {
				return fmt.Errorf("package ID %s already owns %s and cannot also own %s", selected.ID,
					filepath.Join(b.InstallDir, installed.Name), filepath.Join(b.InstallDir, selected.Name))
			}
			if strings.EqualFold(selected.Name, installed.Name) && selected.ID != installed.ID {
				return fmt.Errorf("package %s cannot own directory %s already owned by %s", selected.ID,
					filepath.Join(b.InstallDir, installed.Name), installed.ID)
			}
		}
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
		if err := checkRelocation(b, receipt); err != nil {
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
		if err == nil {
			err = checkRelocation(b, receipt)
		}
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
	lock, err := readLock(filepath.Join(b.RootDir, "portprog.lock.json"), b)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	byID := make(map[string]LockedPackage)
	for _, locked := range lock.Packages {
		byID[locked.Package.ID] = locked
	}
	for _, receipt := range receipts {
		byID[receipt.Package.Package.ID] = receipt.Package
	}
	packages := make([]LockedPackage, 0, len(byID))
	for _, locked := range byID {
		packages = append(packages, locked)
	}
	packages, err = orderLockedPackages(b, packages)
	if err != nil {
		return err
	}
	for _, locked := range packages {
		directory := filepath.Join(b.InstallDir, locked.Package.Name)
		receipt, receiptErr := readReceipt(directory, b)
		if receiptErr != nil && !errors.Is(receiptErr, os.ErrNotExist) {
			return receiptErr
		}
		if receiptErr == nil {
			receiptErr = verifyReceipt(directory, receipt)
			logReceipt(b.Output, receipt, receiptErr)
		}
		if receiptErr != nil {
			logInstalling(b.Output, locked.Package, directory)
			if err := installPackage(b, locked); err != nil {
				return fmt.Errorf("repair %s from recorded artifact %s: %w", locked.Package.ID, locked.SHA256, err)
			}
			logInstalled(b.Output, locked.Package)
		} else if err := relocatePackage(b, receipt); err != nil {
			return err
		}
	}
	if err := relocateInstalled(b); err != nil {
		return err
	}
	return activateInstalled(b)
}

func orderLockedPackages(b Config, packages []LockedPackage) ([]LockedPackage, error) {
	catalog := make(map[string]Package, len(packages))
	lockedByID := make(map[string]LockedPackage, len(packages))
	ids := make([]string, 0, len(packages))
	for _, locked := range packages {
		catalog[locked.Package.ID] = locked.Package
		lockedByID[locked.Package.ID] = locked
		ids = append(ids, locked.Package.ID)
	}
	sort.Strings(ids)
	ordered, err := selectPackages(catalog, ids, b.OS, b.Arch)
	if err != nil {
		return nil, err
	}
	if err := checkInstalledOwnership(b, ordered); err != nil {
		return nil, err
	}
	result := make([]LockedPackage, 0, len(ordered))
	for _, p := range ordered {
		result = append(result, lockedByID[p.ID])
	}
	return result, nil
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
	remaining := make([]LockedPackage, 0, len(receipts)-1)
	for _, receipt := range receipts {
		if receipt.Package.Package.ID != id {
			if err := verifyReceipt(filepath.Join(b.InstallDir, receipt.Package.Package.Name), receipt); err != nil {
				return fmt.Errorf("removal of %s cannot regenerate activation: %w", id, err)
			}
			remaining = append(remaining, receipt.Package)
		}
	}
	documents, err := environmentDocuments(remaining, b.OS == "windows")
	if err != nil {
		return err
	}
	lockPath := filepath.Join(b.RootDir, "portprog.lock.json")
	lock, err := readLock(lockPath, b)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		remaining, err = orderLockedPackages(b, remaining)
		if err != nil {
			return err
		}
		lock = LockFile{SchemaVersion: 1, OS: b.OS, Arch: b.Arch, Profile: "installed", Packages: remaining}
	} else {
		retained := make([]LockedPackage, 0, len(lock.Packages))
		for _, p := range lock.Packages {
			for _, dependency := range p.Package.Dependencies {
				if dependency == id {
					return fmt.Errorf("cannot remove %s while locked package %s depends on it", id, p.Package.ID)
				}
			}
			if p.Package.ID != id {
				retained = append(retained, p)
			}
		}
		lock.Packages = retained
	}
	lockBytes, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("encode updated lock after removing %s: %w", id, err)
	}
	documents["portprog.lock.json"] = append(lockBytes, '\n')
	retired := filepath.Join(b.RootDir, "retired")
	if err := os.MkdirAll(retired, 0755); err != nil {
		return fmt.Errorf("create retired-package directory %s: %w", retired, err)
	}
	destination := filepath.Join(retired, id+"-"+time.Now().UTC().Format("20060102T150405.000000000"))
	original := filepath.Join(b.InstallDir, target.Package.Package.Name)
	if err := os.Rename(original, destination); err != nil {
		return fmt.Errorf("retire installed package %s to %s: %w", id, destination, err)
	}
	if err := publishDocuments(b.RootDir, documents); err != nil {
		rollbackErr := os.Rename(destination, original)
		if rollbackErr != nil {
			rollbackErr = fmt.Errorf("restore package %s from %s to %s: %w", id, destination, original, rollbackErr)
		}
		return errors.Join(err, rollbackErr)
	}
	logRetired(b.Output, id, destination)
	logActivation(b.Output, b.RootDir, len(remaining))
	return nil
}
