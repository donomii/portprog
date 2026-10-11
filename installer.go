package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func installPackage(b Config, p LockedPackage) error {
	name := p.Package.Name
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") {
		return fmt.Errorf("invalid installation directory name %q", name)
	}
	if _, err := archivePath(b.InstallDir, name); err != nil {
		return err
	}
	artifact := filepath.Join(b.ZipDir, p.SHA256)
	if err := verifyArtifact(artifact, p.SHA256, p.Size, downloadLimit(b)); err != nil {
		if b.Offline || !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("package %q: %w", name, err)
		}
		request := p.Package
		request.SHA256 = p.SHA256
		if request.Fetch == "git" {
			if p.Commit == "" {
				return fmt.Errorf("Git package %q lock has no exact commit", name)
			}
			request.Branch = p.Commit
		}
		resolved, err := resolvePackage(b, request)
		if err != nil {
			return fmt.Errorf("restore locked artifact for package %q: %w", name, err)
		}
		if resolved.SHA256 != p.SHA256 || resolved.Size != p.Size {
			return fmt.Errorf("restored artifact for %q differs from lock: SHA256 %s size %d; expected %s size %d", name, resolved.SHA256, resolved.Size, p.SHA256, p.Size)
		}
	}
	if err := os.MkdirAll(b.TempDir, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(b.TempDir, name+"-")
	if err != nil {
		return err
	}
	prepared, err := preparePackage(b, p, artifact, stage)
	if err != nil {
		return fmt.Errorf("package %q installation failed; working files retained at %q: %w", name, stage, err)
	}
	payload := filepath.Join(stage, "install")
	if err := copyInstallTree(prepared, payload, p.Package.Deletes, extractionLimit(b)); err != nil {
		return fmt.Errorf("prepare installation of %q in %q: %w", name, stage, err)
	}
	files, err := collectInstalledFiles(payload)
	if err != nil {
		return err
	}
	outputs := 0
	for _, file := range files {
		if file.SHA256 != "" {
			outputs++
		}
	}
	if outputs == 0 {
		return fmt.Errorf("package %q produced no installed files; working files retained at %q", name, stage)
	}
	if err := checkInstalledPaths(payload, p.Package); err != nil {
		return err
	}
	receipt := Receipt{SchemaVersion: 1, Package: p, Files: files}
	encoded, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(payload, ".portprog-receipt.json")
	file, err := os.OpenFile(receiptPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("create receipt for package %q: %w", name, err)
	}
	_, writeErr := file.Write(append(encoded, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return publishInstallation(b, name, payload)
}

func checkInstalledPaths(root string, p Package) error {
	directories := append([]string{}, p.BinDirs...)
	if p.BinDir != "" {
		directories = append(directories, p.BinDir)
	}
	if p.LibDir != "" {
		directories = append(directories, p.LibDir)
	}
	for _, directory := range directories {
		filename, err := archivePath(root, directory)
		if err != nil {
			return err
		}
		info, err := os.Stat(filename)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("package %q declares executable/library directory %q but it is missing or not a directory: %v", p.Name, directory, err)
		}
	}
	return nil
}

func publishInstallation(b Config, name, payload string) error {
	if err := os.MkdirAll(b.InstallDir, 0755); err != nil {
		return err
	}
	destination := filepath.Join(b.InstallDir, name)
	backup := ""
	if info, err := os.Lstat(destination); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("existing installation %q is not a real directory", destination)
		}
		root := b.RootDir
		if root == "" {
			root = filepath.Dir(b.InstallDir)
		}
		backups := filepath.Join(root, "backups")
		if err := os.MkdirAll(backups, 0755); err != nil {
			return err
		}
		backupDirectory, err := os.MkdirTemp(backups, name+"-")
		if err != nil {
			return err
		}
		backup = filepath.Join(backupDirectory, name)
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("preserve previous installation %q at %q: %w", destination, backup, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(payload, destination); err != nil {
		if backup != "" {
			if restoreErr := os.Rename(backup, destination); restoreErr != nil {
				return fmt.Errorf("publish %q: %v; restore previous installation from %q: %w", destination, err, backup, restoreErr)
			}
		}
		return fmt.Errorf("publish installation %q from %q: %w", destination, payload, err)
	}
	return nil
}

func sourceRoot(extracted string, p Package, requireSingle bool) (string, error) {
	if p.ArchiveRoot != "" {
		selected, err := archivePath(extracted, p.ArchiveRoot)
		if err != nil {
			return "", err
		}
		info, err := os.Lstat(selected)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("package %q archive root %q must be an existing real directory: %v", p.Name, p.ArchiveRoot, err)
		}
		return selected, nil
	}
	if p.Fetch == "git" {
		return extracted, nil
	}
	entries, err := os.ReadDir(extracted)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(extracted, entries[0].Name()), nil
	}
	if requireSingle {
		return "", fmt.Errorf("package %q expected one archive root directory, received %d entries; set ArchiveRoot explicitly", p.Name, len(entries))
	}
	return extracted, nil
}

func preparePackage(b Config, locked LockedPackage, artifact, stage string) (string, error) {
	p := locked.Package
	prepared := filepath.Join(stage, "prepared")
	if p.Plan == "rawData" {
		name := filepath.Base(p.Zip)
		if name == "." || name == "" {
			return "", fmt.Errorf("raw-data package %q needs an artifact filename", p.Name)
		}
		if _, err := archivePath(prepared, name); err != nil {
			return "", err
		}
		if err := os.Mkdir(prepared, 0755); err != nil {
			return "", err
		}
		return prepared, copyArtifactFile(artifact, filepath.Join(prepared, name))
	}
	if p.Plan == "msi" || p.Plan == "pkg" || p.Plan == "dmg" {
		return prepareNativePackage(b, p, artifact, stage)
	}
	extracted := filepath.Join(stage, "source")
	archiveName := p.Zip
	if p.Fetch == "git" {
		archiveName = "source.zip"
	}
	if err := extractArchive(b, artifact, archiveName, extracted); err != nil {
		return "", err
	}
	switch p.Plan {
	case "zipWithDirectory":
		return sourceRoot(extracted, p, true)
	case "zipWithNoDirectory":
		if p.ArchiveRoot != "" {
			return sourceRoot(extracted, p, false)
		}
		return extracted, nil
	case "standardConfigure", "gitAndMake", "goGetAndMake", "cmake":
		source, err := sourceRoot(extracted, p, false)
		if err != nil {
			return "", err
		}
		return buildSource(b, p, source, stage)
	default:
		return "", fmt.Errorf("package %q has unsupported installation plan %q", p.Name, p.Plan)
	}
}

func prepareNativePackage(b Config, p Package, artifact, stage string) (prepared string, err error) {
	prepared = filepath.Join(stage, "prepared")
	switch p.Plan {
	case "msi":
		if runtime.GOOS != "windows" {
			return "", fmt.Errorf("package %q uses MSI administrative extraction and requires Windows; running on %s", p.Name, runtime.GOOS)
		}
		msi := filepath.Join(stage, "artifact.msi")
		if err := copyArtifactFile(artifact, msi); err != nil {
			return "", err
		}
		if err := os.Mkdir(prepared, 0755); err != nil {
			return "", err
		}
		err = runInstallerCommand(b, stage, "msiexec.exe", "/a", msi, "/qn", "TARGETDIR="+prepared, "INSTALLDIR="+prepared, "INSTALLLOCATION="+prepared, "/L*v", filepath.Join(stage, "msi.log"))
		return prepared, err
	case "pkg":
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("package %q uses pkg payload extraction and requires macOS; running on %s", p.Name, runtime.GOOS)
		}
		expanded := filepath.Join(stage, "expanded")
		if err := runInstallerCommand(b, stage, "pkgutil", "--expand-full", artifact, expanded); err != nil {
			return "", err
		}
		payloads := make([]string, 0)
		if err := filepath.WalkDir(expanded, func(filename string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() && entry.Name() == "Payload" {
				payloads = append(payloads, filename)
				return filepath.SkipDir
			}
			return nil
		}); err != nil {
			return "", err
		}
		if len(payloads) != 1 {
			return "", fmt.Errorf("package %q contains %d pkg payloads; exactly one is supported without executing install scripts", p.Name, len(payloads))
		}
		if p.ArchiveRoot != "" {
			return sourceRoot(payloads[0], p, false)
		}
		return payloads[0], nil
	case "dmg":
		if runtime.GOOS != "darwin" {
			return "", fmt.Errorf("package %q uses a disk image and requires macOS; running on %s", p.Name, runtime.GOOS)
		}
		mountpoint := filepath.Join(stage, "mounted")
		if err := os.Mkdir(mountpoint, 0755); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, stage, "hdiutil", "attach", "-readonly", "-nobrowse", "-noautoopen", "-mountpoint", mountpoint, artifact); err != nil {
			return "", err
		}
		defer func() {
			if detachErr := runInstallerCommand(b, stage, "hdiutil", "detach", mountpoint); detachErr != nil {
				err = errors.Join(err, detachErr)
			}
		}()
		selected := mountpoint
		if p.ArchiveRoot != "" {
			selected, err = sourceRoot(mountpoint, p, false)
			if err != nil {
				return "", err
			}
		}
		err = copyInstallTree(selected, prepared, []string{"Applications", ".DS_Store", ".background", ".fseventsd"}, extractionLimit(b))
		return prepared, err
	}
	return "", fmt.Errorf("unknown native package plan %q", p.Plan)
}
