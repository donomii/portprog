package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

func resolveRequestedLock(app App, profileName string, refresh bool) (LockFile, error) {
	if !refresh && regularFileExists(app.Paths.LockFile) {
		lock, err := readLock(app.Paths.LockFile)
		if err != nil {
			return LockFile{}, err
		} else if lock.Profile == profileName && lock.OS == app.Platform.OS && lock.Arch == app.Platform.Arch {
			return lock, nil
		} else {
			return resolveCatalogLock(app, profileName)
		}
	} else {
		return resolveCatalogLock(app, profileName)
	}
}

func resolveCatalogLock(app App, profileName string) (LockFile, error) {
	catalog, catalogErr := loadCatalog(app.Paths.CatalogDir)
	if catalogErr != nil {
		return LockFile{}, catalogErr
	} else {
		profile, profileErr := loadNamedProfile(app.Paths.ProfilesDir, profileName)
		if profileErr != nil {
			return LockFile{}, profileErr
		} else {
			return resolveLock(catalog, profile, app.Platform, app.Deps.Now())
		}
	}
}

func installLockedProfile(app App, lock LockFile) error {
	if err := validateCompatibleLock(app, lock); err != nil {
		return err
	} else if err := ensureStateDirectories(app); err != nil {
		return err
	} else {
		for _, pkg := range lock.Packages {
			health := packageHealth(app, pkg)
			if health.State == "installed" {
				fmt.Fprintf(app.Deps.Output, "Use %s %s: already installed and verified.\n", pkg.Name, pkg.Version)
				continue
			} else if err := installLockedPackage(app, pkg); err != nil {
				return err
			} else {
				fmt.Fprintf(app.Deps.Output, "Installed %s %s for %s/%s.\n", pkg.Name, pkg.Version, app.Platform.OS, app.Platform.Arch)
			}
		}
		if err := writeLock(app, lock); err != nil {
			return err
		} else {
			fmt.Fprintf(app.Deps.Output, "Profile %s is ready. The lockfile records %d exact package assets.\n", lock.Profile, len(lock.Packages))
			return nil
		}
	}
}

func installLockedPackage(app App, pkg LockedPackage) error {
	cachePath, downloadErr := downloadAsset(app, pkg.Name, pkg.Asset)
	if downloadErr != nil {
		return downloadErr
	} else {
		finalPath := packageStorePath(app, pkg)
		parent := filepath.Dir(finalPath)
		if err := ensureContainedDirectory(app.Paths.BaseDir, parent); err != nil {
			return fmt.Errorf("prepare store for package %q at %q: expected writable directory, got error: %w", pkg.Name, parent, err)
		} else {
			staging, stagingErr := os.MkdirTemp(parent, "."+pkg.Name+"-staging-*")
			if stagingErr != nil {
				return fmt.Errorf("create staging directory for package %q under %q: expected writable directory, got error: %w", pkg.Name, parent, stagingErr)
			} else {
				return stageAndPublishPackage(app, pkg, cachePath, staging, finalPath)
			}
		}
	}
}

func stageAndPublishPackage(app App, pkg LockedPackage, cachePath string, staging string, finalPath string) error {
	maximum := app.Settings.MaxExtractMB * bytesPerMebibyte
	if err := extractAsset(cachePath, staging, pkg.Asset, maximum); err != nil {
		trashStaging(app, staging, pkg.Name)
		return fmt.Errorf("stage package %q: %w", pkg.Name, err)
	} else {
		priorReceipt, hadReceipt := optionalReceipt(app, pkg.Name)
		priorFinal, preserveErr := preserveSameTarget(app, finalPath, pkg.Name)
		if preserveErr != nil {
			trashStaging(app, staging, pkg.Name)
			return preserveErr
		} else if err := os.Rename(staging, finalPath); err != nil {
			restoreErr := restorePriorState(priorFinal, finalPath)
			trashStaging(app, staging, pkg.Name)
			return fmt.Errorf("publish package %q from %q to %q: expected atomic rename, got error %v and prior restore result %v", pkg.Name, staging, finalPath, err, restoreErr)
		} else {
			return finishPackagePublish(app, pkg, finalPath, priorFinal, priorReceipt, hadReceipt)
		}
	}
}

func preserveSameTarget(app App, finalPath string, packageName string) (string, error) {
	if _, err := os.Lstat(finalPath); err == nil {
		return movePathToTrash(app, finalPath, "replaced-store-"+packageName)
	} else if os.IsNotExist(err) {
		return "", nil
	} else {
		return "", fmt.Errorf("inspect prior package target %q: expected path or no path, got error: %w", finalPath, err)
	}
}

func finishPackagePublish(app App, pkg LockedPackage, finalPath string, priorFinal string, priorReceipt Receipt, hadReceipt bool) error {
	receipt, err := makeReceipt(app, pkg, finalPath)
	if err != nil {
		rollbackPublishedPackage(app, finalPath, priorFinal, pkg.Name)
		return err
	} else if err := writeReceipt(app, receipt); err != nil {
		rollbackPublishedPackage(app, finalPath, priorFinal, pkg.Name)
		return err
	} else {
		preserveOldVersion(app, priorReceipt, hadReceipt, finalPath, pkg.Name)
		return nil
	}
}

func makeReceipt(app App, pkg LockedPackage, finalPath string) (Receipt, error) {
	relativeStore, err := filepath.Rel(app.Paths.BaseDir, finalPath)
	if err != nil {
		return Receipt{}, fmt.Errorf("record package %q store path %q: expected path relative to %q, got error: %w", pkg.Name, finalPath, app.Paths.BaseDir, err)
	} else {
		return Receipt{
			SchemaVersion: currentSchemaVersion, Name: pkg.Name, Version: pkg.Version,
			OS: app.Platform.OS, Arch: app.Platform.Arch,
			URL: pkg.Asset.URL, SHA256: pkg.Asset.SHA256,
			StorePath:   filepath.ToSlash(relativeStore),
			BinDirs:     append([]string(nil), pkg.Asset.BinDirs...),
			Executables: append([]string(nil), pkg.Asset.Executables...),
			InstalledAt: app.Deps.Now().UTC().Format(time.RFC3339),
		}, nil
	}
}

func rollbackPublishedPackage(app App, finalPath string, priorFinal string, packageName string) {
	_, trashErr := movePathToTrash(app, finalPath, "failed-publish-"+packageName)
	restoreErr := restorePriorState(priorFinal, finalPath)
	if trashErr != nil || restoreErr != nil {
		fmt.Fprintf(app.Deps.Error, "Rollback package %s: expected new store in trash and prior store restored, got trash=%v restore=%v\n", packageName, trashErr, restoreErr)
	} else {
		return
	}
}

func optionalReceipt(app App, packageName string) (Receipt, bool) {
	if !regularFileExists(receiptFilename(app, packageName)) {
		return Receipt{}, false
	} else {
		receipt, err := readReceipt(app, packageName)
		if err != nil {
			fmt.Fprintf(app.Deps.Error, "Read prior receipt for %s: expected valid receipt, got %v; damaged receipt will be preserved during replacement.\n", packageName, err)
			return Receipt{}, false
		} else {
			return receipt, true
		}
	}
}

func preserveOldVersion(app App, prior Receipt, hadReceipt bool, newFinalPath string, packageName string) {
	if !hadReceipt {
		return
	} else {
		oldPath, err := containedPath(app.Paths.BaseDir, prior.StorePath)
		if err != nil {
			fmt.Fprintf(app.Deps.Error, "Preserve old version of %s: expected a contained prior store path, got %v\n", packageName, err)
		} else if filepath.Clean(oldPath) == filepath.Clean(newFinalPath) {
			return
		} else if _, statErr := os.Lstat(oldPath); os.IsNotExist(statErr) {
			return
		} else if statErr != nil {
			fmt.Fprintf(app.Deps.Error, "Preserve old version of %s at %q: expected path or no path, got %v\n", packageName, oldPath, statErr)
		} else {
			_, moveErr := movePathToTrash(app, oldPath, "replaced-version-"+packageName)
			if moveErr != nil {
				fmt.Fprintf(app.Deps.Error, "Preserve old version of %s at %q: expected recoverable trash move, got %v\n", packageName, oldPath, moveErr)
			} else {
				return
			}
		}
	}
}

func trashStaging(app App, staging string, packageName string) {
	_, err := movePathToTrash(app, staging, "failed-staging-"+packageName)
	if err != nil {
		fmt.Fprintf(app.Deps.Error, "Preserve failed staging directory %q: expected trash move, got %v\n", staging, err)
	} else {
		return
	}
}

func packageStorePath(app App, pkg LockedPackage) string {
	return filepath.Join(app.Paths.StoreRoot, pkg.Name, pkg.Version, app.Platform.OS+"-"+app.Platform.Arch)
}

func packageHealth(app App, pkg LockedPackage) PackageHealth {
	filename := receiptFilename(app, pkg.Name)
	if !regularFileExists(filename) {
		return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "missing", Detail: "No active receipt records this package."}
	} else {
		receipt, err := readReceipt(app, pkg.Name)
		if err != nil {
			return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "damaged", Detail: err.Error()}
		} else if detail := receiptMismatch(app, receipt, pkg); detail != "" {
			return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "outdated", Detail: detail}
		} else {
			return executableHealth(app, receipt, pkg)
		}
	}
}

func receiptMismatch(app App, receipt Receipt, pkg LockedPackage) string {
	expectedStore, storeErr := filepath.Rel(app.Paths.BaseDir, packageStorePath(app, pkg))
	if storeErr != nil {
		return fmt.Sprintf("Expected store path relative to %q, got error %v.", app.Paths.BaseDir, storeErr)
	} else if receipt.Name != pkg.Name || receipt.Version != pkg.Version {
		return fmt.Sprintf("Receipt records %s %s instead of %s %s.", receipt.Name, receipt.Version, pkg.Name, pkg.Version)
	} else if receipt.OS != app.Platform.OS || receipt.Arch != app.Platform.Arch {
		return fmt.Sprintf("Receipt records %s/%s instead of %s/%s.", receipt.OS, receipt.Arch, app.Platform.OS, app.Platform.Arch)
	} else if receipt.SHA256 != pkg.Asset.SHA256 || receipt.URL != pkg.Asset.URL {
		return "Receipt source URL or SHA-256 differs from the lockfile."
	} else if !slices.Equal(receipt.BinDirs, pkg.Asset.BinDirs) || !slices.Equal(receipt.Executables, pkg.Asset.Executables) {
		return "Receipt binary paths differ from the lockfile."
	} else if filepath.ToSlash(receipt.StorePath) != filepath.ToSlash(expectedStore) {
		return fmt.Sprintf("Receipt store path %q differs from expected path %q.", receipt.StorePath, filepath.ToSlash(expectedStore))
	} else {
		return ""
	}
}

func executableHealth(app App, receipt Receipt, pkg LockedPackage) PackageHealth {
	storePath, err := containedPath(app.Paths.BaseDir, receipt.StorePath)
	if err != nil {
		return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "damaged", Detail: err.Error()}
	} else {
		for _, executable := range receipt.Executables {
			filename, pathErr := containedPortablePath(storePath, executable)
			if pathErr != nil {
				return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "damaged", Detail: pathErr.Error()}
			} else {
				info, statErr := os.Lstat(filename)
				if statErr != nil || !info.Mode().IsRegular() {
					return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "damaged", Detail: fmt.Sprintf("Expected regular executable %q, got error %v.", filename, statErr)}
				} else if app.Platform.OS != "windows" && info.Mode().Perm()&0o111 == 0 {
					return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "damaged", Detail: fmt.Sprintf("Expected runnable executable %q, got mode %v.", filename, info.Mode())}
				} else {
					continue
				}
			}
		}
		return PackageHealth{Name: pkg.Name, Version: pkg.Version, State: "installed", Detail: "Receipt and expected executables match the lockfile."}
	}
}

func removePackage(app App, packageName string) (string, string, error) {
	if err := validateIdentifier("package name", packageName); err != nil {
		return "", "", err
	} else {
		receipt, receiptErr := readReceipt(app, packageName)
		if receiptErr != nil {
			return "", "", receiptErr
		} else {
			storePath, pathErr := containedPath(app.Paths.BaseDir, receipt.StorePath)
			if pathErr != nil {
				return "", "", pathErr
			} else {
				return removePackagePaths(app, packageName, storePath, receiptFilename(app, packageName))
			}
		}
	}
}

func removePackagePaths(app App, packageName string, storePath string, receiptPath string) (string, string, error) {
	storeTrash, storeErr := movePathToTrash(app, storePath, "removed-store-"+packageName)
	if storeErr != nil {
		return "", "", storeErr
	} else {
		receiptTrash, receiptErr := movePathToTrash(app, receiptPath, "removed-receipt-"+packageName)
		if receiptErr != nil {
			restoreErr := os.Rename(storeTrash, storePath)
			return "", "", fmt.Errorf("remove receipt for package %q: expected recoverable trash move, got error %v and store restore result %v", packageName, receiptErr, restoreErr)
		} else {
			return storeTrash, receiptTrash, nil
		}
	}
}

func validateCompatibleLock(app App, lock LockFile) error {
	if err := validateLock(lock); err != nil {
		return err
	} else if lock.OS != app.Platform.OS || lock.Arch != app.Platform.Arch {
		return fmt.Errorf("install lock profile %q: expected platform %s/%s, got %s/%s", lock.Profile, app.Platform.OS, app.Platform.Arch, lock.OS, lock.Arch)
	} else {
		return nil
	}
}
