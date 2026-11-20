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
	"time"
	"unicode/utf8"
)

var supportedAssetFormats = map[string]bool{
	"raw": true, "zip": true, "tar.gz": true, "tar.bz2": true, "tar.xz": true,
}

func loadPackageManifest(filename string) (PackageManifest, error) {
	data, err := readRegularFile(filename, "read package manifest")
	if err != nil {
		return PackageManifest{}, err
	} else {
		decoder, decoderErr := newStrictJSONDecoder(data, filename)
		if decoderErr != nil {
			return PackageManifest{}, decoderErr
		} else {
			var manifest PackageManifest
			if decodeErr := decoder.Decode(&manifest); decodeErr != nil {
				return PackageManifest{}, fmt.Errorf("decode package manifest %q: expected the v1 schema, got error: %w", filename, decodeErr)
			} else if endErr := requireJSONEnd(decoder, filename); endErr != nil {
				return PackageManifest{}, endErr
			} else if validateErr := validatePackageManifest(manifest); validateErr != nil {
				return PackageManifest{}, fmt.Errorf("validate package manifest %q: %w", filename, validateErr)
			} else {
				return manifest, nil
			}
		}
	}
}

func loadProfile(filename string) (Profile, error) {
	data, err := readRegularFile(filename, "read profile")
	if err != nil {
		return Profile{}, err
	} else {
		decoder, decoderErr := newStrictJSONDecoder(data, filename)
		if decoderErr != nil {
			return Profile{}, decoderErr
		} else {
			var profile Profile
			if decodeErr := decoder.Decode(&profile); decodeErr != nil {
				return Profile{}, fmt.Errorf("decode profile %q: expected the v1 schema, got error: %w", filename, decodeErr)
			} else if endErr := requireJSONEnd(decoder, filename); endErr != nil {
				return Profile{}, endErr
			} else if validateErr := validateProfile(profile); validateErr != nil {
				return Profile{}, fmt.Errorf("validate profile %q: %w", filename, validateErr)
			} else {
				return profile, nil
			}
		}
	}
}

func loadCatalog(directory string) (map[string]PackageManifest, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read package catalog %q: expected a readable directory, got error: %w", directory, err)
	} else {
		sort.Slice(entries, func(left int, right int) bool { return entries[left].Name() < entries[right].Name() })
		catalog := make(map[string]PackageManifest)
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			} else {
				filename := filepath.Join(directory, entry.Name())
				manifest, loadErr := loadPackageManifest(filename)
				if loadErr != nil {
					return nil, loadErr
				} else if strings.TrimSuffix(entry.Name(), ".json") != manifest.Name {
					return nil, fmt.Errorf("load package catalog %q: expected file %q to define package %q, got %q", directory, entry.Name(), strings.TrimSuffix(entry.Name(), ".json"), manifest.Name)
				} else if _, duplicate := catalog[manifest.Name]; duplicate {
					return nil, fmt.Errorf("load package catalog %q: expected package %q once, got a duplicate", directory, manifest.Name)
				} else {
					catalog[manifest.Name] = manifest
				}
			}
		}
		if len(catalog) == 0 {
			return nil, fmt.Errorf("load package catalog %q: expected at least one JSON package manifest, got none", directory)
		} else {
			return catalog, nil
		}
	}
}

func loadNamedProfile(directory string, name string) (Profile, error) {
	if err := validateIdentifier("profile name", name); err != nil {
		return Profile{}, err
	} else {
		filename := filepath.Join(directory, name+".json")
		profile, loadErr := loadProfile(filename)
		if loadErr != nil {
			return Profile{}, loadErr
		} else if profile.Name != name {
			return Profile{}, fmt.Errorf("load profile %q: expected profile name %q, got %q", filename, name, profile.Name)
		} else {
			return profile, nil
		}
	}
}

func validatePackageManifest(manifest PackageManifest) error {
	if manifest.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("expected schema_version %d, got %d", currentSchemaVersion, manifest.SchemaVersion)
	} else if err := validateIdentifier("package name", manifest.Name); err != nil {
		return err
	} else if err := validateVersion("package version", manifest.Version); err != nil {
		return err
	} else if strings.TrimSpace(manifest.Description) == "" {
		return fmt.Errorf("validate package %q: expected a user-facing description", manifest.Name)
	} else if strings.TrimSpace(manifest.License) == "" {
		return fmt.Errorf("validate package %q: expected a license identifier", manifest.Name)
	} else if err := validateHTTPSURL("homepage", manifest.Homepage); err != nil {
		return fmt.Errorf("validate package %q: %w", manifest.Name, err)
	} else if len(manifest.Assets) == 0 {
		return fmt.Errorf("validate package %q: expected at least one platform asset", manifest.Name)
	} else if err := validateDependencies(manifest); err != nil {
		return err
	} else {
		return validateAssets(manifest)
	}
}

func validateDependencies(manifest PackageManifest) error {
	seen := make(map[string]bool)
	for _, dependency := range manifest.Dependencies {
		if err := validateIdentifier("dependency", dependency); err != nil {
			return fmt.Errorf("validate dependencies for package %q: %w", manifest.Name, err)
		} else if dependency == manifest.Name {
			return fmt.Errorf("validate dependencies for package %q: expected another package, got a self-dependency", manifest.Name)
		} else if seen[dependency] {
			return fmt.Errorf("validate dependencies for package %q: expected dependency %q once, got a duplicate", manifest.Name, dependency)
		} else {
			seen[dependency] = true
		}
	}
	return nil
}

func validateAssets(manifest PackageManifest) error {
	seen := make(map[string]bool)
	for index, asset := range manifest.Assets {
		if err := validateAsset(asset); err != nil {
			return fmt.Errorf("validate asset %d for package %q: %w", index+1, manifest.Name, err)
		} else {
			key := asset.OS + "/" + asset.Arch
			if seen[key] {
				return fmt.Errorf("validate assets for package %q: expected one asset for %s, got a duplicate", manifest.Name, key)
			} else {
				seen[key] = true
			}
		}
	}
	return nil
}

func validateAsset(asset PackageAsset) error {
	if !supportedOS(asset.OS) {
		return fmt.Errorf("expected os darwin, linux, or windows, got %q", asset.OS)
	} else if !supportedArch(asset.Arch) {
		return fmt.Errorf("expected arch amd64 or arm64, got %q", asset.Arch)
	} else if err := validateHTTPSURL("asset URL", asset.URL); err != nil {
		return err
	} else if err := validateDigest(asset.SHA256); err != nil {
		return err
	} else if asset.Size < 0 {
		return fmt.Errorf("expected size zero or greater, got %d", asset.Size)
	} else if !supportedAssetFormats[asset.Format] {
		return fmt.Errorf("expected format raw, zip, tar.gz, tar.bz2, or tar.xz, got %q", asset.Format)
	} else if asset.StripComponents < 0 {
		return fmt.Errorf("expected strip_components zero or greater, got %d", asset.StripComponents)
	} else if len(asset.BinDirs) == 0 {
		return fmt.Errorf("expected at least one bin_dirs entry, got none")
	} else if len(asset.Executables) == 0 {
		return fmt.Errorf("expected at least one executables entry, got none")
	} else if err := validateAssetPaths(asset); err != nil {
		return err
	} else if asset.Format == "raw" && asset.StripComponents != 0 {
		return fmt.Errorf("expected strip_components 0 for raw asset, got %d", asset.StripComponents)
	} else if asset.Format == "raw" && asset.RawPath == "" {
		return fmt.Errorf("expected raw_path for raw asset, got empty text")
	} else if asset.Format != "raw" && asset.RawPath != "" {
		return fmt.Errorf("expected empty raw_path for %s asset, got %q", asset.Format, asset.RawPath)
	} else {
		return nil
	}
}

func validateAssetPaths(asset PackageAsset) error {
	if err := validateOptionalRawPath(asset.RawPath); err != nil {
		return err
	} else {
		return validateAssetPathLists(asset)
	}
}

func validateOptionalRawPath(rawPath string) error {
	if rawPath != "" {
		return validatePortableRelativePath("raw_path", rawPath, false)
	} else {
		return nil
	}
}

func validateAssetPathLists(asset PackageAsset) error {
	seenBinDirs := make(map[string]bool)
	for _, binDir := range asset.BinDirs {
		if err := validatePortableRelativePath("bin_dirs", binDir, true); err != nil {
			return err
		} else if seenBinDirs[binDir] {
			return fmt.Errorf("validate bin_dirs: expected path %q once, got duplicate", binDir)
		} else {
			seenBinDirs[binDir] = true
		}
	}
	seenExecutables := make(map[string]bool)
	for _, executable := range asset.Executables {
		if err := validatePortableRelativePath("executables", executable, false); err != nil {
			return err
		} else if seenExecutables[executable] {
			return fmt.Errorf("validate executables: expected path %q once, got duplicate", executable)
		} else {
			seenExecutables[executable] = true
		}
	}
	return nil
}

func validatePortableRelativePath(field string, value string, allowDot bool) error {
	cleaned := path.Clean(value)
	if !utf8.ValidString(value) {
		return fmt.Errorf("validate %s: expected valid UTF-8 path text", field)
	} else if strings.TrimSpace(value) == "" {
		return fmt.Errorf("validate %s: expected a non-empty relative path", field)
	} else if strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return fmt.Errorf("validate %s: expected portable forward-slash relative path, got %q", field, value)
	} else if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("validate %s: expected no parent traversal, got %q", field, value)
	} else if cleaned == "." && !allowDot {
		return fmt.Errorf("validate %s: expected a file or child path, got %q", field, value)
	} else if cleaned == "." {
		return nil
	} else if cleaned != value {
		return fmt.Errorf("validate %s: expected a clean path, got %q which cleans to %q", field, value, cleaned)
	} else if err := validatePortablePathComponents(field, value); err != nil {
		return err
	} else {
		return nil
	}
}

func validatePortablePathComponents(field string, value string) error {
	for _, component := range strings.Split(value, "/") {
		if strings.ContainsAny(component, "<>:\"|?*") || strings.IndexFunc(component, func(character rune) bool { return character < 0x20 || character == 0x7f }) >= 0 {
			return fmt.Errorf("validate %s: expected components portable to Windows, macOS, and Linux, got %q", field, component)
		} else if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return fmt.Errorf("validate %s: expected components without a trailing dot or space, got %q", field, component)
		} else if windowsReservedPathComponent(component) {
			return fmt.Errorf("validate %s: expected no Windows reserved device name, got %q", field, component)
		} else {
			continue
		}
	}
	return nil
}

func windowsReservedPathComponent(component string) bool {
	base := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return true
	} else if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	} else {
		return false
	}
}

func validateHTTPSURL(field string, value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("validate %s: expected an HTTPS URL, got %q: %w", field, value, err)
	} else if parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("validate %s: expected an HTTPS URL with a host, got %q", field, value)
	} else if parsed.User != nil {
		return fmt.Errorf("validate %s: expected no embedded credentials, got %q", field, value)
	} else {
		return nil
	}
}

func validateDigest(digest string) error {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(digest) != sha256HexLength || strings.ToLower(digest) != digest || len(decoded) != sha256HexLength/2 {
		return fmt.Errorf("validate sha256: expected %d lowercase hexadecimal characters, got %q", sha256HexLength, digest)
	} else {
		return nil
	}
}

func validateProfile(profile Profile) error {
	if profile.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("expected schema_version %d, got %d", currentSchemaVersion, profile.SchemaVersion)
	} else if err := validateIdentifier("profile name", profile.Name); err != nil {
		return err
	} else if strings.TrimSpace(profile.Description) == "" {
		return fmt.Errorf("validate profile %q: expected a user-facing description", profile.Name)
	} else if len(profile.Packages) == 0 {
		return fmt.Errorf("validate profile %q: expected at least one package", profile.Name)
	} else {
		seen := make(map[string]bool)
		for _, packageName := range profile.Packages {
			if err := validateIdentifier("profile package", packageName); err != nil {
				return err
			} else if seen[packageName] {
				return fmt.Errorf("validate profile %q: expected package %q once, got a duplicate", profile.Name, packageName)
			} else {
				seen[packageName] = true
			}
		}
		return nil
	}
}

func resolveLock(catalog map[string]PackageManifest, profile Profile, platform Platform, generated time.Time) (LockFile, error) {
	if err := validateProfile(profile); err != nil {
		return LockFile{}, err
	} else if !supportedOS(platform.OS) || !supportedArch(platform.Arch) {
		return LockFile{}, fmt.Errorf("resolve profile %q: expected supported platform, got %s/%s", profile.Name, platform.OS, platform.Arch)
	} else {
		order, orderErr := dependencyOrder(catalog, profile)
		if orderErr != nil {
			return LockFile{}, orderErr
		} else {
			return makeLock(catalog, profile, platform, generated, order)
		}
	}
}

func dependencyOrder(catalog map[string]PackageManifest, profile Profile) ([]string, error) {
	states := make(map[string]int)
	order := make([]string, 0)
	var visit func(string, []string) error
	visit = func(name string, stack []string) error {
		manifest, exists := catalog[name]
		if !exists {
			return fmt.Errorf("resolve profile %q: expected package %q in catalog, got none", profile.Name, name)
		} else if states[name] == 2 {
			return nil
		} else if states[name] == 1 {
			return fmt.Errorf("resolve profile %q: expected an acyclic dependency graph, got cycle %s -> %s", profile.Name, strings.Join(stack, " -> "), name)
		} else if err := validatePackageManifest(manifest); err != nil {
			return err
		} else {
			states[name] = 1
			for _, dependency := range manifest.Dependencies {
				if err := visit(dependency, append(stack, name)); err != nil {
					return err
				} else {
					continue
				}
			}
			states[name] = 2
			order = append(order, name)
			return nil
		}
	}
	for _, name := range profile.Packages {
		if err := visit(name, nil); err != nil {
			return nil, err
		} else {
			continue
		}
	}
	return order, nil
}

func makeLock(catalog map[string]PackageManifest, profile Profile, platform Platform, generated time.Time, order []string) (LockFile, error) {
	lock := LockFile{
		SchemaVersion: currentSchemaVersion, Profile: profile.Name,
		ProfileDescription: profile.Description, OS: platform.OS, Arch: platform.Arch,
		GeneratedAt: generated.UTC().Format(time.RFC3339),
		Packages:    make([]LockedPackage, 0, len(order)),
	}
	for _, name := range order {
		manifest := catalog[name]
		asset, err := selectAsset(manifest, platform)
		if err != nil {
			return LockFile{}, err
		} else {
			lock.Packages = append(lock.Packages, LockedPackage{
				Name: manifest.Name, Version: manifest.Version,
				Description: manifest.Description, License: manifest.License,
				Dependencies: append([]string(nil), manifest.Dependencies...), Asset: asset,
			})
		}
	}
	return lock, nil
}

func selectAsset(manifest PackageManifest, platform Platform) (PackageAsset, error) {
	matches := make([]PackageAsset, 0, 1)
	for _, asset := range manifest.Assets {
		if asset.OS == platform.OS && asset.Arch == platform.Arch {
			matches = append(matches, asset)
		} else {
			continue
		}
	}
	if len(matches) != 1 {
		return PackageAsset{}, fmt.Errorf("select asset for package %q: expected exactly one %s/%s asset, got %d", manifest.Name, platform.OS, platform.Arch, len(matches))
	} else {
		return matches[0], nil
	}
}

func supportedOS(value string) bool {
	return value == "darwin" || value == "linux" || value == "windows"
}

func supportedArch(value string) bool {
	return value == "amd64" || value == "arm64"
}
