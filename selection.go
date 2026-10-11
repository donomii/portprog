package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func readProfile(root, name string) (Profile, error) {
	if !safeIdentifier(name) {
		return Profile{}, fmt.Errorf("profile name %q must be a single filename without a path", name)
	}
	path := filepath.Join(root, "profiles", name+".json")
	profile, err := readJSONDocument[Profile](path)
	if err != nil {
		return Profile{}, err
	}
	if strings.TrimSpace(profile.Description) == "" || len(profile.Packages) == 0 {
		return Profile{}, fmt.Errorf("profile %s needs a description and at least one OS/architecture package selection", path)
	}
	for target, ids := range profile.Packages {
		parts := strings.Split(target, "/")
		if len(parts) != 2 || !supportedOS(parts[0]) || !supportedArch(parts[1]) || len(ids) == 0 {
			return Profile{}, fmt.Errorf("profile %s has invalid or empty platform selection %q", path, target)
		}
	}
	return profile, nil
}

func supportedOS(name string) bool {
	return name == "windows" || name == "darwin" || name == "linux" || name == "all"
}

func supportedArch(name string) bool {
	return name == "amd64" || name == "arm64" || name == "386" || name == "all"
}

func profileIDs(profile Profile, osName, arch string) ([]string, error) {
	var result []string
	seen := make(map[string]bool)
	for _, key := range []string{"all/all", "all/" + arch, osName + "/all", osName + "/" + arch} {
		for _, id := range profile.Packages[key] {
			if !seen[id] {
				result = append(result, id)
				seen[id] = true
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("profile %q has no packages for %s/%s", profile.Description, osName, arch)
	}
	return result, nil
}

func selectedPackages(b Config, options commandOptions) ([]Package, error) {
	catalog, err := loadCatalog(b.RootDir)
	if err != nil {
		return nil, err
	}
	var ids []string
	if options.file != "" {
		absolute, err := filepath.Abs(options.file)
		if err != nil {
			return nil, fmt.Errorf("resolve recipe path %s from caller directory: %w", options.file, err)
		}
		p, err := loadPackage(absolute)
		if err != nil {
			return nil, err
		}
		if existing, found := catalog[p.ID]; found && !packagesEqual(existing, p) {
			return nil, fmt.Errorf("recipe %s conflicts with catalog package ID %s", absolute, p.ID)
		}
		catalog[p.ID] = p
		ids = []string{p.ID}
	} else if options.devel {
		paths, err := os.ReadDir(filepath.Join(b.RootDir, "packages-develop"))
		if err != nil {
			return nil, fmt.Errorf("read development recipes in %s: %w", filepath.Join(b.RootDir, "packages-develop"), err)
		}
		for _, entry := range paths {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				return nil, fmt.Errorf("unexpected development catalog entry %s; expected a JSON recipe file", entry.Name())
			}
			p, err := loadPackage(filepath.Join(b.RootDir, "packages-develop", entry.Name()))
			if err != nil {
				return nil, err
			}
			if existing, found := catalog[p.ID]; found && !packagesEqual(existing, p) {
				return nil, fmt.Errorf("development recipe %s conflicts with catalog package ID %s", entry.Name(), p.ID)
			}
			catalog[p.ID] = p
			ids = append(ids, p.ID)
		}
	} else {
		profile, err := readProfile(b.RootDir, options.profile)
		if err != nil {
			return nil, err
		}
		ids, err = profileIDs(profile, b.OS, b.Arch)
		if err != nil {
			return nil, err
		}
	}
	packages, err := selectPackages(catalog, ids, b.OS, b.Arch)
	if err != nil {
		return nil, err
	}
	var locked []LockedPackage
	for _, p := range packages {
		locked = append(locked, LockedPackage{Package: p})
	}
	filtered, err := filterLocked(locked, options)
	if err != nil {
		return nil, err
	}
	result := make([]Package, 0, len(filtered))
	for _, p := range filtered {
		result = append(result, p.Package)
	}
	return result, nil
}

func excludedPackage(p Package, options commandOptions) bool {
	name := strings.ToLower(p.ID + " " + p.Name)
	goCompiler := strings.HasPrefix(strings.ToLower(p.Name), "go-") || p.Name == "go"
	gccCompiler := strings.Contains(name, "gcc") || strings.Contains(name, "tdm64fortran")
	return options.noGo && goCompiler || options.noGcc && gccCompiler || options.noGit && p.Fetch == "git"
}

func filterLocked(packages []LockedPackage, options commandOptions) ([]LockedPackage, error) {
	result := make([]LockedPackage, 0, len(packages))
	selected := make(map[string]bool)
	for _, p := range packages {
		if !excludedPackage(p.Package, options) {
			result = append(result, p)
			selected[p.Package.ID] = true
		}
	}
	for _, p := range result {
		for _, dependency := range p.Package.Dependencies {
			if !selected[dependency] {
				return nil, fmt.Errorf("selected package %s requires %s, which is missing or excluded by a compiler/Git option", p.Package.ID, dependency)
			}
		}
	}
	return result, nil
}

func resolveLock(b Config, profile string, packages []Package) (LockFile, error) {
	if err := checkInstalledOwnership(b, packages); err != nil {
		return LockFile{}, err
	}
	if err := ensureInstallationDirectories(b); err != nil {
		return LockFile{}, err
	}
	lock := LockFile{SchemaVersion: 1, OS: b.OS, Arch: b.Arch, Profile: profile}
	for _, p := range packages {
		logResolving(b.Output, p)
		locked, err := resolvePackage(b, p)
		if err != nil {
			return LockFile{}, fmt.Errorf("resolve %s (%s): %w", p.ID, p.Version, err)
		}
		if err := validateLockedPackage(locked, b); err != nil {
			return LockFile{}, err
		}
		lock.Packages = append(lock.Packages, locked)
	}
	return lock, nil
}

func validateCatalogAndProfiles(b Config) error {
	catalog, err := loadCatalog(b.RootDir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(b.RootDir, "profiles"))
	if err != nil {
		return fmt.Errorf("read profile catalog: %w", err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			return fmt.Errorf("unexpected profile entry %s; expected a JSON profile", entry.Name())
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		profile, err := readProfile(b.RootDir, name)
		if err != nil {
			return err
		}
		for _, osName := range []string{"windows", "darwin", "linux"} {
			for _, arch := range []string{"386", "amd64", "arm64"} {
				ids, selectionErr := profileIDs(profile, osName, arch)
				if selectionErr == nil {
					if _, err := selectPackages(catalog, ids, osName, arch); err != nil {
						return fmt.Errorf("profile %s for %s/%s: %w", name, osName, arch, err)
					}
				}
			}
		}
		count++
	}
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := catalog[id]
		osName, arch := p.OS, p.Arch
		if osName == "all" {
			osName = "linux"
		}
		if arch == "all" {
			arch = "amd64"
		}
		if _, err := selectPackages(catalog, []string{id}, osName, arch); err != nil {
			return fmt.Errorf("catalog package %s: %w", id, err)
		}
	}
	if count == 0 || len(catalog) == 0 {
		return errors.New("catalog validation requires at least one package and one profile")
	}
	logValidated(b.Output, len(catalog), count)
	return nil
}
