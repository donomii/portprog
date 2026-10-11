package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func catalogFixture(id string) Package {
	return Package{
		ID: id, Name: id, Zip: id + ".zip", Url: "file:///tmp/" + id + ".zip",
		Fetch: "web", Plan: "zipWithNoDirectory", OS: "linux", Arch: "amd64",
	}
}

func writeCatalogFixture(t *testing.T, root, name string, recipe Package) string {
	t.Helper()
	data, err := json.Marshal(recipe)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(root, name)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestLoadPackageRejectsInvalidJSON(t *testing.T) {
	valid, err := json.Marshal(catalogFixture("tool"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown field":   strings.TrimSuffix(string(valid), "}") + `,"Typo":true}`,
		"trailing object": string(valid) + `{}`,
		"trailing text":   string(valid) + `oops`,
		"trailing comma":  strings.TrimSuffix(string(valid), "}") + `,}`,
		"wrong type":      `{"Name":42}`,
		"empty object":    `{}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "invalid.json")
			if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadPackage(filename); err == nil || !strings.Contains(err.Error(), filename) {
				t.Fatalf("expected an error identifying %q, received %v", filename, err)
			}
		})
	}
}

func TestValidatePackage(t *testing.T) {
	cases := map[string]func(*Package){
		"missing ID":                 func(p *Package) { p.ID = "" },
		"unsafe Name":                func(p *Package) { p.Name = "../tool" },
		"reserved Name":              func(p *Package) { p.Name = "NUL" },
		"unsafe Zip":                 func(p *Package) { p.Zip = "https://example.org/tool.zip" },
		"unknown fetch":              func(p *Package) { p.Fetch = "curl" },
		"unknown plan":               func(p *Package) { p.Plan = "ZipWithDirectory" },
		"HTTP":                       func(p *Package) { p.Url = "http://example.org/tool.zip" },
		"remote file":                func(p *Package) { p.Url = "file://example.org/tool.zip" },
		"relative file":              func(p *Package) { p.Url = "file:tool.zip" },
		"credentials":                func(p *Package) { p.Url = "https://user:password@example.org/tool.zip" },
		"invalid digest":             func(p *Package) { p.SHA256 = "abc" },
		"absolute binary directory":  func(p *Package) { p.BinDir = "/bin" },
		"escaping binary directory":  func(p *Package) { p.BinDirs = []string{"../bin"} },
		"escaping library directory": func(p *Package) { p.LibDir = "lib/../../other" },
		"escaping archive root":      func(p *Package) { p.ArchiveRoot = "../source" },
		"escaping removal":           func(p *Package) { p.Deletes = []string{"../file"} },
		"remove installation":        func(p *Package) { p.Deletes = []string{"."} },
		"command string":             func(p *Package) { p.Command = "echo unexpected" },
		"missing OS":                 func(p *Package) { p.OS = "" },
		"missing Arch":               func(p *Package) { p.Arch = "" },
		"unknown Type":               func(p *Package) { p.Type = "Application" },
		"wrong OS for MSI":           func(p *Package) { p.Plan = "msi" },
		"wrong OS for PKG":           func(p *Package) { p.Plan = "pkg" },
		"wrong OS for DMG":           func(p *Package) { p.Plan = "dmg" },
		"data needs raw plan":        func(p *Package) { p.Type = "data" },
		"duplicate dependency":       func(p *Package) { p.Dependencies = []string{"dependency", "dependency"} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			recipe := catalogFixture("tool")
			change(&recipe)
			if err := validatePackage(recipe); err == nil {
				t.Fatalf("expected invalid recipe to be rejected: %+v", recipe)
			}
		})
	}
	recipe := catalogFixture("tool")
	recipe.BinDir = "."
	recipe.BinDirs = []string{"bin", "tools/bin"}
	if err := validatePackage(recipe); err != nil {
		t.Fatal(err)
	}
}

func TestSelectPackagesDependencyOrder(t *testing.T) {
	app := catalogFixture("app")
	app.Dependencies = []string{"middle", "base"}
	middle := catalogFixture("middle")
	middle.Dependencies = []string{"base"}
	base := catalogFixture("base")
	base.OS, base.Arch = "all", "all"
	selected, err := selectPackages(map[string]Package{"app": app, "middle": middle, "base": base}, []string{"app", "base"}, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, recipe := range selected {
		ids = append(ids, recipe.ID)
	}
	if !reflect.DeepEqual(ids, []string{"base", "middle", "app"}) {
		t.Fatalf("expected dependency-first order without duplicates, received %v", ids)
	}
}

func TestSelectPackagesRejectsBrokenGraph(t *testing.T) {
	cases := map[string]func(map[string]Package){
		"missing dependency": func(c map[string]Package) { p := c["app"]; p.Dependencies = []string{"absent"}; c["app"] = p },
		"cycle":              func(c map[string]Package) { p := c["base"]; p.Dependencies = []string{"app"}; c["base"] = p },
		"directory conflict": func(c map[string]Package) { p := c["base"]; p.Name = "APP"; c["base"] = p },
		"wrong OS":           func(c map[string]Package) { p := c["base"]; p.OS = "windows"; c["base"] = p },
		"wrong architecture": func(c map[string]Package) { p := c["base"]; p.Arch = "arm64"; c["base"] = p },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			app := catalogFixture("app")
			app.Dependencies = []string{"base"}
			catalog := map[string]Package{"app": app, "base": catalogFixture("base")}
			change(catalog)
			if _, err := selectPackages(catalog, []string{"app"}, "linux", "amd64"); err == nil {
				t.Fatal("expected invalid dependency graph to be rejected")
			}
		})
	}
}

func TestLoadCatalogRejectsDuplicateIDs(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "packages")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	writeCatalogFixture(t, directory, "one.json", catalogFixture("tool"))
	writeCatalogFixture(t, directory, "two.json", catalogFixture("tool"))
	if _, err := loadCatalog(root); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected duplicate ID error, received %v", err)
	}
}

func TestShippedCatalogAndProfiles(t *testing.T) {
	catalog, err := loadCatalog(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) < 84 {
		t.Fatalf("expected all 73 legacy recipes, eight Go recipes, and three Python recipes, received %d", len(catalog))
	}
	filenames, err := filepath.Glob("profiles/*.json")
	if err != nil || len(filenames) == 0 {
		t.Fatalf("expected selectable profiles, received %v: %v", filenames, err)
	}
	for _, filename := range filenames {
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		var profile Profile
		if err := json.Unmarshal(data, &profile); err != nil {
			t.Fatalf("%s: %v", filename, err)
		}
		if profile.Description == "" {
			t.Fatalf("%s: profile needs an explanation", filename)
		}
		for platform, ids := range profile.Packages {
			parts := strings.Split(platform, "/")
			if len(parts) != 2 {
				t.Fatalf("%s: invalid platform %q", filename, platform)
			}
			if _, err := selectPackages(catalog, ids, parts[0], parts[1]); err != nil {
				t.Fatalf("%s platform %s: %v", filename, platform, err)
			}
		}
	}
}
