package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

const metadataFileMaximum = int64(4 * 1024 * 1024)

func derivePaths(settings Settings) (AppPaths, error) {
	baseDir, err := filepath.Abs(settings.BaseDir)
	if err != nil {
		return AppPaths{}, fmt.Errorf("derive Portprog paths: expected an absolute base directory for %q, got error: %w", settings.BaseDir, err)
	} else {
		canonicalBase, canonicalErr := filepath.EvalSymlinks(baseDir)
		if canonicalErr != nil {
			return AppPaths{}, fmt.Errorf("derive Portprog paths: expected existing base directory %q, got error resolving symbolic links: %w", baseDir, canonicalErr)
		} else {
			baseDir = canonicalBase
		}
		paths := AppPaths{
			BaseDir:     baseDir,
			InstallRoot: filepath.Join(baseDir, settings.InstallRoot),
			CacheRoot:   filepath.Join(baseDir, settings.CacheRoot),
			StateRoot:   filepath.Join(baseDir, settings.StateRoot),
			TrashRoot:   filepath.Join(baseDir, settings.TrashRoot),
			CatalogDir:  filepath.Join(baseDir, settings.CatalogDir),
			ProfilesDir: filepath.Join(baseDir, settings.ProfilesDir),
			LockFile:    filepath.Join(baseDir, settings.LockFile),
		}
		paths.StoreRoot = filepath.Join(paths.InstallRoot, "store")
		paths.CacheObjects = filepath.Join(paths.CacheRoot, "sha256")
		paths.ReceiptsRoot = filepath.Join(paths.StateRoot, "receipts")
		return validateDerivedPaths(paths)
	}
}

func validateDerivedPaths(paths AppPaths) (AppPaths, error) {
	candidates := []string{
		paths.InstallRoot, paths.StoreRoot, paths.CacheRoot, paths.CacheObjects,
		paths.StateRoot, paths.ReceiptsRoot, paths.TrashRoot, paths.CatalogDir,
		paths.ProfilesDir, paths.LockFile,
	}
	for _, candidate := range candidates {
		inside, err := pathIsInside(paths.BaseDir, candidate)
		if err != nil {
			return AppPaths{}, err
		} else if !inside {
			return AppPaths{}, fmt.Errorf("derive Portprog path %q: expected it inside base directory %q", candidate, paths.BaseDir)
		} else if filepath.Clean(candidate) == filepath.Clean(paths.BaseDir) {
			return AppPaths{}, fmt.Errorf("derive Portprog path %q: expected a child of base directory %q, got the base itself", candidate, paths.BaseDir)
		} else if err := rejectExistingSymlinkComponents(paths.BaseDir, candidate); err != nil {
			return AppPaths{}, err
		} else {
			continue
		}
	}
	return validateDistinctConfiguredPaths(paths)
}

func validateDistinctConfiguredPaths(paths AppPaths) (AppPaths, error) {
	roots := []struct {
		name string
		path string
	}{
		{name: "install_root", path: paths.InstallRoot},
		{name: "cache_root", path: paths.CacheRoot},
		{name: "state_root", path: paths.StateRoot},
		{name: "trash_root", path: paths.TrashRoot},
		{name: "catalog_dir", path: paths.CatalogDir},
		{name: "profiles_dir", path: paths.ProfilesDir},
	}
	for left := 0; left < len(roots); left++ {
		for right := left + 1; right < len(roots); right++ {
			overlap, err := configuredPathsOverlap(roots[left].path, roots[right].path)
			if err != nil {
				return AppPaths{}, err
			} else if overlap {
				return AppPaths{}, fmt.Errorf("validate configured paths: expected %s %q and %s %q to be separate, got overlapping paths", roots[left].name, roots[left].path, roots[right].name, roots[right].path)
			} else {
				continue
			}
		}
	}
	for _, root := range roots {
		overlap, err := configuredPathsOverlap(root.path, paths.LockFile)
		if err != nil {
			return AppPaths{}, err
		} else if overlap {
			return AppPaths{}, fmt.Errorf("validate configured paths: expected lock_file %q outside %s %q, got overlapping paths", paths.LockFile, root.name, root.path)
		} else {
			continue
		}
	}
	return paths, nil
}

func configuredPathsOverlap(left string, right string) (bool, error) {
	rightInsideLeft, leftErr := pathIsInside(left, right)
	leftInsideRight, rightErr := pathIsInside(right, left)
	if leftErr != nil {
		return false, leftErr
	} else if rightErr != nil {
		return false, rightErr
	} else {
		return rightInsideLeft || leftInsideRight, nil
	}
}

func validateRelativePath(name string, value string) error {
	cleaned := filepath.Clean(value)
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("validate setting %s: expected a non-empty relative path, got empty text", name)
	} else if filepath.IsAbs(value) {
		return fmt.Errorf("validate setting %s: expected a base-directory-relative path, got absolute path %q", name, value)
	} else if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("validate setting %s: expected a child path without parent traversal, got %q", name, value)
	} else {
		return nil
	}
}

func validateIdentifier(field string, value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("validate %s: expected lowercase letters, digits, dots, or hyphens beginning with a letter or digit, got %q", field, value)
	} else {
		return validatePortablePathComponents(field, value)
	}
}

func validateVersion(field string, value string) error {
	if !versionPattern.MatchString(value) {
		return fmt.Errorf("validate %s: expected a path-safe immutable version using letters, digits, dots, underscores, plus, or hyphens, got %q", field, value)
	} else {
		return validatePortablePathComponents(field, value)
	}
}

func pathIsInside(root string, candidate string) (bool, error) {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false, fmt.Errorf("compare path %q with root %q: expected comparable paths, got error: %w", candidate, root, err)
	} else if relative == "." {
		return true, nil
	} else if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false, nil
	} else {
		return true, nil
	}
}

func containedPath(root string, relative string) (string, error) {
	if err := validateRelativePath("contained path", relative); err != nil {
		return "", err
	} else {
		candidate := filepath.Join(root, filepath.FromSlash(relative))
		inside, containErr := pathIsInside(root, candidate)
		if containErr != nil {
			return "", containErr
		} else if !inside {
			return "", fmt.Errorf("resolve contained path %q: expected it inside %q, got %q", relative, root, candidate)
		} else if err := rejectExistingSymlinkComponents(root, candidate); err != nil {
			return "", err
		} else {
			return candidate, nil
		}
	}
}

func regularFileExists(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	} else {
		return info.Mode().IsRegular()
	}
}

func readRegularFile(path string, task string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("%s %q: expected a readable regular file, got error: %w", task, path, err)
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %q: expected a regular file without symbolic links, got mode %v", task, path, info.Mode())
	} else if info.Size() > metadataFileMaximum {
		return nil, fmt.Errorf("%s %q: expected at most %d bytes, got %d", task, path, metadataFileMaximum, info.Size())
	} else {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("%s %q: expected complete readable data, got error: %w", task, path, readErr)
		} else if int64(len(data)) > metadataFileMaximum {
			return nil, fmt.Errorf("%s %q: expected at most %d bytes, read %d", task, path, metadataFileMaximum, len(data))
		} else {
			return data, nil
		}
	}
}

func rejectExistingSymlinkComponents(root string, candidate string) error {
	inside, err := pathIsInside(root, candidate)
	if err != nil {
		return err
	} else if !inside {
		return fmt.Errorf("inspect path %q: expected it inside %q", candidate, root)
	} else {
		relative, relativeErr := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
		if relativeErr != nil {
			return fmt.Errorf("inspect path %q beneath %q: expected comparable paths, got error: %w", candidate, root, relativeErr)
		} else {
			current := filepath.Clean(root)
			components := []string{}
			if relative != "." {
				components = strings.Split(relative, string(filepath.Separator))
			} else {
				components = []string{}
			}
			for index := -1; index < len(components); index++ {
				if index >= 0 {
					current = filepath.Join(current, components[index])
				} else {
					current = filepath.Clean(root)
				}
				info, statErr := os.Lstat(current)
				if os.IsNotExist(statErr) {
					return nil
				} else if statErr != nil {
					return fmt.Errorf("inspect path component %q: expected path or no path, got error: %w", current, statErr)
				} else if info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("inspect path component %q: expected no symbolic links beneath %q, got mode %v", current, root, info.Mode())
				} else {
					continue
				}
			}
			return nil
		}
	}
}

func ensureContainedDirectory(root string, directory string) error {
	if err := rejectExistingSymlinkComponents(root, directory); err != nil {
		return err
	} else if err := os.MkdirAll(directory, defaultDirMode); err != nil {
		return fmt.Errorf("create contained directory %q: expected writable directory, got error: %w", directory, err)
	} else if err := rejectExistingSymlinkComponents(root, directory); err != nil {
		return err
	} else {
		info, statErr := os.Lstat(directory)
		if statErr != nil {
			return fmt.Errorf("inspect contained directory %q: expected directory, got error: %w", directory, statErr)
		} else if !info.IsDir() {
			return fmt.Errorf("inspect contained directory %q: expected directory, got mode %v", directory, info.Mode())
		} else {
			return nil
		}
	}
}
