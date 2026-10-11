package main

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

func packageComponent(value string) bool {
	if value == "" || value == "." || value == ".." || strings.TrimSpace(value) != value {
		return false
	}
	if strings.ContainsAny(value, "/\\:;<>\"|?*") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, letter := range value {
		if unicode.IsControl(letter) {
			return false
		}
	}
	stem := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

func packageRelativePath(value string) bool {
	if value == "." {
		return true
	}
	if value == "" || path.Clean(value) != value || strings.HasPrefix(value, "/") {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if !packageComponent(component) {
			return false
		}
	}
	return true
}

func validatePackage(recipe Package) error {
	for field, value := range map[string]string{"ID": recipe.ID, "Name": recipe.Name} {
		if !packageComponent(value) {
			return fmt.Errorf("package %q field %s: expected a nonempty safe filename, received %q", recipe.ID, field, value)
		}
	}
	if recipe.OS != "all" && recipe.OS != "windows" && recipe.OS != "darwin" && recipe.OS != "linux" {
		return fmt.Errorf("package %q OS %q: expected windows, darwin, linux, or all", recipe.ID, recipe.OS)
	}
	if !supportedArch(recipe.Arch) {
		return fmt.Errorf("package %q Arch %q: expected 386, amd64, arm64, or all", recipe.ID, recipe.Arch)
	}
	if recipe.Command != "" {
		return fmt.Errorf("package %q Command: arbitrary command strings are unsupported", recipe.ID)
	}
	if recipe.Type != "" && recipe.Type != "application" && recipe.Type != "data" && recipe.Type != "source" {
		return fmt.Errorf("package %q Type %q: expected application, data, or source", recipe.ID, recipe.Type)
	}
	if err := validatePackageSource(recipe); err != nil {
		return err
	}
	if err := validatePackagePlan(recipe); err != nil {
		return err
	}
	paths := append([]string{recipe.BinDir, recipe.LibDir, recipe.ArchiveRoot}, recipe.BinDirs...)
	paths = append(paths, recipe.Deletes...)
	for _, relative := range paths {
		if relative != "" && !packageRelativePath(relative) {
			return fmt.Errorf("package %q path %q: expected a canonical relative path inside its installation", recipe.ID, relative)
		}
	}
	for _, relative := range recipe.Deletes {
		if relative == "" || relative == "." {
			return fmt.Errorf("package %q Deletes path %q: expected a file inside its installation", recipe.ID, relative)
		}
	}
	seen := make(map[string]bool)
	for _, dependency := range recipe.Dependencies {
		if !packageComponent(dependency) || dependency == recipe.ID || seen[dependency] {
			return fmt.Errorf("package %q dependency %q: expected a distinct valid package ID", recipe.ID, dependency)
		}
		seen[dependency] = true
	}
	return nil
}

func validatePackageSource(recipe Package) error {
	if recipe.Fetch != "web" && recipe.Fetch != "git" {
		return fmt.Errorf("package %q Fetch %q: expected web or git", recipe.ID, recipe.Fetch)
	}
	location, err := url.Parse(recipe.Url)
	if err != nil {
		return fmt.Errorf("package %q URL %q: %w", recipe.ID, recipe.Url, err)
	}
	validHTTPS := location.Scheme == "https" && location.Hostname() != "" && location.User == nil
	localHost := location.Host == "" || location.Host == "localhost"
	validFile := location.Scheme == "file" && localHost && strings.HasPrefix(location.Path, "/") &&
		location.RawQuery == "" && location.User == nil
	if (!validHTTPS && !validFile) || location.Fragment != "" {
		return fmt.Errorf("package %q URL %q: expected HTTPS or an absolute local file URL without credentials or fragment",
			recipe.ID, recipe.Url)
	}
	if recipe.Fetch == "web" && !packageComponent(recipe.Zip) {
		return fmt.Errorf("package %q Zip %q: expected a nonempty archive filename", recipe.ID, recipe.Zip)
	}
	if recipe.Fetch == "git" && recipe.Zip != "" {
		return fmt.Errorf("package %q Zip %q: Git recipes do not use an archive filename", recipe.ID, recipe.Zip)
	}
	if recipe.Fetch == "git" && recipe.Branch == "" {
		return fmt.Errorf("package %q requires an explicit Git Branch, tag or commit", recipe.ID)
	}
	if strings.HasPrefix(recipe.Branch, "-") || strings.ContainsAny(recipe.Branch, "\r\n\x00") {
		return fmt.Errorf("package %q Branch %q: expected a Git branch, tag, or commit without command options or control characters",
			recipe.ID, recipe.Branch)
	}
	if recipe.SHA256 != "" {
		digest, err := hex.DecodeString(recipe.SHA256)
		if err != nil || len(digest) != 32 || recipe.SHA256 != strings.ToLower(recipe.SHA256) {
			return fmt.Errorf("package %q SHA256 %q: expected 64 lowercase hexadecimal digits", recipe.ID, recipe.SHA256)
		}
	}
	return nil
}

func validatePackagePlan(recipe Package) error {
	switch recipe.Plan {
	case "zipWithDirectory", "zipWithNoDirectory", "rawData", "msi", "dmg", "pkg":
		if recipe.Fetch != "web" {
			return fmt.Errorf("package %q plan %q requires Fetch web, received %q", recipe.ID, recipe.Plan, recipe.Fetch)
		}
	case "standardConfigure", "gitAndMake", "goGetAndMake", "cmake":
	default:
		return fmt.Errorf("package %q unknown Plan %q", recipe.ID, recipe.Plan)
	}
	if recipe.Plan == "msi" && recipe.OS != "windows" {
		return fmt.Errorf("package %q plan msi requires OS windows, received %q", recipe.ID, recipe.OS)
	}
	if (recipe.Plan == "dmg" || recipe.Plan == "pkg") && recipe.OS != "darwin" {
		return fmt.Errorf("package %q plan %s requires OS darwin, received %q", recipe.ID, recipe.Plan, recipe.OS)
	}
	if recipe.Type == "data" && recipe.Plan != "rawData" {
		return fmt.Errorf("package %q Type data requires plan rawData, received %q", recipe.ID, recipe.Plan)
	}
	return nil
}

func loadCatalog(root string) (map[string]Package, error) {
	catalog := make(map[string]Package)
	directories := []string{"packages", "packages-windows", "packages-osx", "packages-data", "packages-develop"}
	for _, directory := range directories {
		entries, err := os.ReadDir(filepath.Join(root, directory))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read catalog directory %q: %w", filepath.Join(root, directory), err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			filename := filepath.Join(root, directory, entry.Name())
			recipe, err := loadPackage(filename)
			if err != nil {
				return nil, err
			}
			if previous, found := catalog[recipe.ID]; found {
				return nil, fmt.Errorf("catalog package %q in %q duplicates package %q", recipe.ID, filename, previous.Name)
			}
			catalog[recipe.ID] = recipe
		}
	}
	if err := validateCatalogDependencies(catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

func validateCatalogDependencies(catalog map[string]Package) error {
	states := make(map[string]int)
	var visit func(string, []string) error
	visit = func(id string, ancestry []string) error {
		if states[id] == 1 {
			return fmt.Errorf("package dependency cycle: %s", strings.Join(append(ancestry, id), " -> "))
		}
		if states[id] == 2 {
			return nil
		}
		recipe, exists := catalog[id]
		if !exists {
			return fmt.Errorf("package %q required by %q is missing from the catalog", id, strings.Join(ancestry, " -> "))
		}
		states[id] = 1
		for _, dependency := range recipe.Dependencies {
			if err := visit(dependency, append(ancestry, id)); err != nil {
				return err
			}
		}
		states[id] = 2
		return nil
	}
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id, nil); err != nil {
			return err
		}
	}
	return nil
}

func selectPackages(catalog map[string]Package, ids []string, osName, arch string) ([]Package, error) {
	states := make(map[string]int)
	targets := make(map[string]string)
	var selected []Package
	var visit func(string, []string) error
	visit = func(id string, ancestry []string) error {
		if states[id] == 1 {
			return fmt.Errorf("package dependency cycle: %s", strings.Join(append(ancestry, id), " -> "))
		}
		if states[id] == 2 {
			return nil
		}
		recipe, exists := catalog[id]
		if !exists {
			return fmt.Errorf("package %q required by %q is missing from the catalog", id, strings.Join(ancestry, " -> "))
		}
		if err := validatePackage(recipe); err != nil {
			return err
		}
		if (recipe.OS != "all" && recipe.OS != osName) || (recipe.Arch != "all" && recipe.Arch != arch) {
			return fmt.Errorf("package %q supports %s/%s, requested platform is %s/%s", id, recipe.OS, recipe.Arch, osName, arch)
		}
		target := strings.ToLower(recipe.Name)
		if owner, exists := targets[target]; exists && owner != id {
			return fmt.Errorf("packages %q and %q both own installation directory %q", owner, id, recipe.Name)
		}
		targets[target] = id
		states[id] = 1
		dependencies := append([]string(nil), recipe.Dependencies...)
		sort.Strings(dependencies)
		for _, dependency := range dependencies {
			if err := visit(dependency, append(ancestry, id)); err != nil {
				return err
			}
		}
		states[id] = 2
		selected = append(selected, recipe)
		return nil
	}
	for _, id := range ids {
		if err := visit(id, nil); err != nil {
			return nil, err
		}
	}
	return selected, nil
}
