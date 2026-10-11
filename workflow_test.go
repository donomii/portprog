package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestWorkflowLockReplayAndNoInstall(t *testing.T) {
	root, recipe, archive := workflowFixture(t)
	workflowRun(t, root, "--lock", "--file", recipe)
	lockBefore := workflowRead(t, filepath.Join(root, "portprog.lock.json"))
	if entries, err := os.ReadDir(filepath.Join(root, "installs")); err != nil || len(entries) != 0 {
		t.Fatalf("lock-only operation installed entries %v, error %v; expected empty installs", entries, err)
	}
	if err := os.Rename(archive, archive+".retained"); err != nil {
		t.Fatal(err)
	}
	workflowRun(t, root, "--offline")
	workflowCheckContent(t, filepath.Join(root, "installs", "fixture", "bin", "hello.txt"), "portable fixture\n")
	installsBefore := workflowSnapshot(t, filepath.Join(root, "installs"))
	cacheBefore := workflowSnapshot(t, filepath.Join(root, "zips"))
	workflowRun(t, root, "--no-install")
	workflowRun(t, root, "--offline")
	if after := workflowSnapshot(t, filepath.Join(root, "installs")); !reflect.DeepEqual(after, installsBefore) {
		t.Fatalf("activation or lock replay modified installation: before %v; after %v", installsBefore, after)
	}
	if after := workflowSnapshot(t, filepath.Join(root, "zips")); !reflect.DeepEqual(after, cacheBefore) {
		t.Fatalf("activation or lock replay modified verified cache: before %v; after %v", cacheBefore, after)
	}
	if after := workflowRead(t, filepath.Join(root, "portprog.lock.json")); !bytes.Equal(after, lockBefore) {
		t.Fatal("offline replay rewrote the recorded lockfile")
	}
	workflowReservationReleased(t, root)
}

func TestWorkflowOfflineRepairAndDurableRemoval(t *testing.T) {
	root, recipe, archive := workflowFixture(t)
	workflowRun(t, root, "--file", recipe)
	installed := filepath.Join(root, "installs", "fixture", "bin", "hello.txt")
	if err := os.WriteFile(installed, []byte("damaged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := workflowCommand(root, "--list"); err == nil || !strings.Contains(output+err.Error(), "fixture") {
		t.Fatalf("list returned %v and %q for damaged fixture; expected an identified verification error", err, output)
	}
	if err := os.Rename(archive, archive+".retained"); err != nil {
		t.Fatal(err)
	}
	workflowRun(t, root, "--offline", "--repair")
	workflowCheckContent(t, installed, "portable fixture\n")
	workflowRun(t, root, "--remove", "fixture")
	workflowCheckRemoved(t, root)
	workflowRun(t, root, "--offline")
	if _, err := os.Stat(filepath.Join(root, "installs", "fixture")); !os.IsNotExist(err) {
		t.Fatalf("offline launch reinstated removed fixture: %v", err)
	}
	workflowReservationReleased(t, root)
}

func workflowCheckRemoved(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "installs", "fixture")); !os.IsNotExist(err) {
		t.Fatalf("removed fixture still occupies its installation directory: %v", err)
	}
	preserved, err := filepath.Glob(filepath.Join(root, "retired", "fixture-*", "bin", "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, filename := range preserved {
		if bytes.Equal(workflowRead(t, filename), []byte("portable fixture\n")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("removal did not preserve fixture data under retired: %q", preserved)
	}
	lock, err := readLock(filepath.Join(root, "portprog.lock.json"), configuration(root, defaultSettings(), &bytes.Buffer{}))
	if err != nil || len(lock.Packages) != 0 {
		t.Fatalf("lock after removing last package = %+v, error %v; expected valid empty selection", lock, err)
	}
	for _, name := range []string{"environment.fish", "environment.bash", "environment.bat", "environment.ps1"} {
		if bytes.Contains(workflowRead(t, filepath.Join(root, name)), []byte("fixture")) {
			t.Errorf("activation script %s still references removed fixture", name)
		}
	}
}

func TestWorkflowFailedRemovalPreservesInstallation(t *testing.T) {
	root, recipe, _ := workflowFixture(t)
	workflowRun(t, root, "--file", recipe)
	obstruction := filepath.Join(root, "environment.ps1")
	if err := os.Rename(obstruction, obstruction+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(obstruction, 0755); err != nil {
		t.Fatal(err)
	}
	before := workflowSnapshot(t, filepath.Join(root, "installs"))
	documents := map[string][]byte{}
	for _, name := range []string{"portprog.lock.json", "environment.bash", "environment.fish", "environment.bat"} {
		documents[name] = workflowRead(t, filepath.Join(root, name))
	}
	if _, err := workflowCommand(root, "--remove", "fixture"); err == nil || !strings.Contains(err.Error(), "environment.ps1") {
		t.Fatalf("removal error = %v; expected obstructed activation path", err)
	}
	if after := workflowSnapshot(t, filepath.Join(root, "installs")); !reflect.DeepEqual(before, after) {
		t.Fatalf("failed removal changed installation: before %v; after %v", before, after)
	}
	for name, data := range documents {
		if after := workflowRead(t, filepath.Join(root, name)); !bytes.Equal(data, after) {
			t.Errorf("failed removal changed %s", name)
		}
	}
	workflowReservationReleased(t, root)
}

func TestWorkflowReservationPreventsMutation(t *testing.T) {
	root, recipe, _ := workflowFixture(t)
	reservation := filepath.Join(root, ".portprog-operation.lock")
	if err := os.WriteFile(reservation, []byte("existing operation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := workflowSnapshot(t, root)
	if _, err := workflowCommand(root, "--file", recipe); err == nil || !strings.Contains(err.Error(), reservation) {
		t.Fatalf("reserved installation returned %v; expected reservation error naming %s", err, reservation)
	}
	if after := workflowSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("competing operation modified reserved root: before %v; after %v", before, after)
	}
}

func TestWorkflowCatalogValidationDoesNotInstall(t *testing.T) {
	root, _, archive := workflowFixture(t)
	if err := os.Rename(archive, archive+".retained"); err != nil {
		t.Fatal(err)
	}
	before := workflowSnapshot(t, root)
	workflowRun(t, root, "--validate")
	if after := workflowSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("catalog validation modified portable root: before %v; after %v", before, after)
	}
}

func TestWorkflowRejectsInvalidSettings(t *testing.T) {
	valid, err := json.Marshal(defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"duplicate": strings.Replace(string(valid), "{", "{\"schema_version\":1,", 1),
		"unknown":   strings.Replace(string(valid), "{", "{\"unexpected\":true,", 1),
		"trailing":  string(valid) + " {}",
		"null":      "null",
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "portprog.json"), []byte(document), 0644); err != nil {
				t.Fatal(err)
			}
			before := workflowSnapshot(t, root)
			if _, err := workflowCommand(root, "--no-install"); err == nil {
				t.Fatalf("accepted %s settings: %s", name, document)
			}
			if after := workflowSnapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("invalid settings changed the portable root")
			}
		})
	}
}

func TestWorkflowRejectsControlCharactersInRoot(t *testing.T) {
	for _, character := range []rune{0, 1, '\t', '\n', '\r', 27, 31, 127} {
		root := filepath.Join(t.TempDir(), "root"+string(character))
		if _, err := workflowCommand(root, "--no-install"); err == nil || !strings.Contains(err.Error(), "control") {
			t.Errorf("root containing character %d returned %v; expected validation before filesystem access", character, err)
		}
	}
}

func TestWorkflowArchitectureWildcardProfile(t *testing.T) {
	profile := Profile{Description: "architecture selection", Packages: map[string][]string{
		"all/all": {"common"}, "all/amd64": {"architecture"}, "linux/all": {"linux"}, "linux/amd64": {"specific"},
	}}
	ids, err := profileIDs(profile, "linux", "amd64")
	expected := []string{"common", "architecture", "linux", "specific"}
	if err != nil || !reflect.DeepEqual(ids, expected) {
		t.Fatalf("profile selection = %q, error %v; expected %q", ids, err, expected)
	}
}

func workflowFixture(t *testing.T) (string, string, string) {
	t.Helper()
	parent := t.TempDir()
	root := filepath.Join(parent, "portable workspace")
	if err := os.MkdirAll(filepath.Join(root, "packages"), 0755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(parent, "fixture.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "bin/hello.txt", Method: zip.Deflate}
	header.SetMode(0644)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("portable fixture\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	urlPath := filepath.ToSlash(archive)
	if runtime.GOOS == "windows" {
		urlPath = "/" + urlPath
	}
	source := (&url.URL{Scheme: "file", Path: urlPath}).String()
	p := Package{ID: "fixture", Name: "fixture", Version: "1", OS: runtime.GOOS, Arch: runtime.GOARCH,
		Fetch: "web", Plan: "zipWithNoDirectory", Url: source, Zip: "fixture.zip", BinDirs: []string{"bin"}}
	recipe := filepath.Join(root, "packages", "fixture.json")
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recipe, encoded, 0644); err != nil {
		t.Fatal(err)
	}
	profile := Profile{Description: "local fixture profile", Packages: map[string][]string{runtime.GOOS + "/" + runtime.GOARCH: {"fixture"}}}
	if err := writeJSONAtomic(filepath.Join(root, "profiles", "default.json"), profile); err != nil {
		t.Fatal(err)
	}
	return root, recipe, archive
}

func workflowRun(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	output, err := workflowCommand(root, arguments...)
	if err != nil {
		t.Fatalf("portprog --root %q %q: %v\n%s", root, arguments, err, output)
	}
	return output
}

func workflowCommand(root string, arguments ...string) (string, error) {
	var output bytes.Buffer
	err := run(append([]string{"--root", root}, arguments...), &output, &output)
	return output.String(), err
}

func workflowRead(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func workflowCheckContent(t *testing.T, filename, expected string) {
	t.Helper()
	if data := workflowRead(t, filename); string(data) != expected {
		t.Fatalf("file %s = %q; expected %q", filename, data, expected)
	}
}

func workflowReservationReleased(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, ".portprog-operation.lock")); !os.IsNotExist(err) {
		t.Fatalf("finished operation retained its installation reservation: %v", err)
	}
}

func workflowSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[relative] = "directory"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result[relative] = fmt.Sprintf("%s %x", info.Mode(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
