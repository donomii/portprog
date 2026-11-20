package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validPackageJSON = `{
  "schema_version": 1,
  "name": "tiny-tool",
  "version": "1.2.3",
  "description": "A tiny test tool.",
  "homepage": "https://example.com/tiny-tool",
  "license": "MIT",
  "dependencies": [],
  "assets": [{
    "os": "darwin",
    "arch": "arm64",
    "url": "https://example.com/tiny-tool-1.2.3",
    "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "size": 12,
    "format": "raw",
    "strip_components": 0,
    "raw_path": "bin/tiny-tool",
    "bin_dirs": ["bin"],
    "executables": ["bin/tiny-tool"]
  }]
}`

const validProfileJSON = `{
  "schema_version": 1,
  "name": "starter",
  "description": "Small test profile.",
  "packages": ["tiny-tool"]
}`

func TestLoadPackageManifestIsStrict(t *testing.T) {
	tempDir := t.TempDir()
	validPath := filepath.Join(tempDir, "valid.json")
	writeTestFile(t, validPath, validPackageJSON)
	manifest, err := loadPackageManifest(validPath)
	if err != nil {
		t.Fatalf("load valid package manifest: expected success, got %v", err)
	} else {
		if manifest.Name != "tiny-tool" {
			t.Fatalf("load package name: expected tiny-tool, got %q", manifest.Name)
		} else {
			unknownPath := filepath.Join(tempDir, "unknown.json")
			writeTestFile(t, unknownPath, strings.Replace(validPackageJSON, `"license": "MIT",`, `"license": "MIT", "unknown": true,`, 1))
			_, unknownErr := loadPackageManifest(unknownPath)
			if unknownErr == nil {
				t.Fatal("load manifest with unknown field: expected rejection, got success")
			} else {
				duplicatePath := filepath.Join(tempDir, "duplicate.json")
				writeTestFile(t, duplicatePath, strings.Replace(validPackageJSON, `"name": "tiny-tool",`, `"name": "tiny-tool", "name": "other",`, 1))
				_, duplicateErr := loadPackageManifest(duplicatePath)
				if duplicateErr == nil {
					t.Fatal("load manifest with duplicate field: expected rejection, got success")
				} else {
					return
				}
			}
		}
	}
}

func TestResolveLockOrdersDependenciesBeforeDependents(t *testing.T) {
	asset := testAsset("https://example.com/archive", strings.Repeat("a", sha256HexLength))
	catalog := map[string]PackageManifest{
		"runtime":  testManifest("runtime", nil, asset),
		"compiler": testManifest("compiler", []string{"runtime"}, asset),
		"editor":   testManifest("editor", []string{"runtime"}, asset),
	}
	profile := Profile{SchemaVersion: currentSchemaVersion, Name: "starter", Description: "Test", Packages: []string{"compiler", "editor"}}
	generated := time.Date(2026, time.August, 27, 4, 5, 6, 0, time.UTC)
	lock, err := resolveLock(catalog, profile, Platform{OS: "darwin", Arch: "arm64"}, generated)
	if err != nil {
		t.Fatalf("resolve valid dependency graph: expected success, got %v", err)
	} else {
		got := packageNames(lock.Packages)
		want := "runtime,compiler,editor"
		if got != want {
			t.Fatalf("resolve dependency order: expected %s, got %s", want, got)
		} else {
			return
		}
	}
}

func TestResolveLockRejectsCyclesAndMissingAssets(t *testing.T) {
	asset := testAsset("https://example.com/archive", strings.Repeat("b", sha256HexLength))
	cycleCatalog := map[string]PackageManifest{
		"one": testManifest("one", []string{"two"}, asset),
		"two": testManifest("two", []string{"one"}, asset),
	}
	profile := Profile{SchemaVersion: currentSchemaVersion, Name: "starter", Description: "Test", Packages: []string{"one"}}
	_, cycleErr := resolveLock(cycleCatalog, profile, Platform{OS: "darwin", Arch: "arm64"}, time.Now())
	if cycleErr == nil {
		t.Fatal("resolve dependency cycle: expected rejection, got success")
	} else {
		linuxCatalog := map[string]PackageManifest{"one": testManifest("one", nil, asset)}
		_, assetErr := resolveLock(linuxCatalog, profile, Platform{OS: "linux", Arch: "amd64"}, time.Now())
		if assetErr == nil {
			t.Fatal("resolve package without matching asset: expected rejection, got success")
		} else {
			return
		}
	}
}

func TestPortablePathsRejectCrossPlatformHazards(t *testing.T) {
	invalid := []string{"C:/escape", "bin/CON", "bin/tool.", "bin/tool?copy"}
	for _, value := range invalid {
		if err := validatePortableRelativePath("test path", value, false); err == nil {
			t.Fatalf("validate hazardous portable path %q: expected rejection, got success", value)
		} else {
			continue
		}
	}
	if err := validatePortableRelativePath("binary directory", ".", true); err != nil {
		t.Fatalf("validate installation root as binary directory: expected success, got %v", err)
	} else if err := validateIdentifier("package name", "con"); err == nil {
		t.Fatal("validate Windows device package name: expected rejection, got success")
	} else if err := validateVersion("package version", "1.0."); err == nil {
		t.Fatal("validate version ending in dot: expected rejection, got success")
	} else {
		return
	}
}

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	err := os.WriteFile(path, []byte(contents), defaultFileMode)
	if err != nil {
		t.Fatalf("write test file %q: expected success, got %v", path, err)
	} else {
		return
	}
}

func testAsset(url string, digest string) PackageAsset {
	return PackageAsset{
		OS: "darwin", Arch: "arm64", URL: url, SHA256: digest,
		Size: 1, Format: "raw", RawPath: "bin/tool",
		BinDirs: []string{"bin"}, Executables: []string{"bin/tool"},
	}
}

func testManifest(name string, dependencies []string, asset PackageAsset) PackageManifest {
	return PackageManifest{
		SchemaVersion: currentSchemaVersion,
		Name:          name, Version: "1.0.0", Description: "Test package " + name,
		Homepage: "https://example.com/" + name, License: "MIT",
		Dependencies: dependencies, Assets: []PackageAsset{asset},
	}
}

func packageNames(packages []LockedPackage) string {
	names := make([]string, 0, len(packages))
	for _, pkg := range packages {
		names = append(names, pkg.Name)
	}
	return strings.Join(names, ",")
}
