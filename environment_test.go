package main

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestEnvironmentPathsUseInstalledDirectories(t *testing.T) {
	packages := []LockedPackage{
		{Package: Package{ID: "one", Name: "Tool One", BinDir: "bin", BinDirs: []string{"bin", "."}}},
		{Package: Package{ID: "two", Name: "Tool Two", BinDirs: []string{"Scripts"}}},
	}
	paths, err := environmentPaths(packages, false)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"installs/Tool One/bin", "installs/Tool One", "installs/Tool Two/Scripts", "langlibs/gopath/bin"}
	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf("executable paths = %q; expected installed package directories %q", paths, expected)
	}
	packages = append(packages, LockedPackage{Package: Package{ID: "same", Name: "TOOL ONE", BinDir: "BIN"}})
	paths, err = environmentPaths(packages, true)
	if err != nil || !reflect.DeepEqual(paths, expected) {
		t.Fatalf("Windows paths = %q, error = %v; expected case-insensitive deduplication %q", paths, err, expected)
	}
}

func TestEnvironmentRejectsUnsafePaths(t *testing.T) {
	invalid := []string{"", "..", "../bin", "/bin", "bin/../other", "bin//other", "bin\\other", "bin:other", "bin;other", "bin\nother"}
	for _, directory := range invalid {
		t.Run(directory, func(t *testing.T) {
			packages := []LockedPackage{{Package: Package{ID: "invalid", Name: "tool", BinDirs: []string{directory}}}}
			if _, err := environmentPaths(packages, false); err == nil {
				t.Fatalf("accepted unsafe executable directory %q", directory)
			}
		})
	}
	packages := []LockedPackage{{Package: Package{ID: "invalid", Name: "../other", BinDir: "bin"}}}
	if _, err := environmentPaths(packages, false); err == nil {
		t.Fatal("accepted package directory outside installs")
	}
}

func TestEnvironmentReportsWriteFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "environment.fish"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeEnvironment(Config{RootDir: root}, nil); err == nil || !strings.Contains(err.Error(), "environment.fish") {
		t.Fatalf("write error = %v; expected the failed activation script path", err)
	}
}

func TestActivationAfterRelocation(t *testing.T) {
	root, packageName := relocatedEnvironment(t)
	if runtime.GOOS == "windows" {
		t.Run("PowerShell", func(t *testing.T) { testPowerShellActivation(t, root, packageName, "powershell.exe") })
		t.Run("CommandPrompt", func(t *testing.T) { testBatchActivation(t, root, packageName) })
	} else {
		t.Run("Bash", func(t *testing.T) { testBashActivation(t, root, packageName) })
		t.Run("Fish", func(t *testing.T) { testFishActivation(t, root, packageName) })
		if _, err := exec.LookPath("pwsh"); err == nil {
			t.Run("PowerShell", func(t *testing.T) { testPowerShellActivation(t, root, packageName, "pwsh") })
		}
	}
}

func relocatedEnvironment(t *testing.T) (string, string) {
	t.Helper()
	parent := t.TempDir()
	original := filepath.Join(parent, "original")
	root := filepath.Join(parent, "moved 工具 café & (portable) %ROOT% ! dollar$ 'quote'")
	packageName := "Tool 工具 café & suite's $% (x)!"
	if err := os.Mkdir(original, 0755); err != nil {
		t.Fatal(err)
	}
	packages := []LockedPackage{{Package: Package{ID: "tool", Name: packageName, BinDir: ".", BinDirs: []string{"bin", "bin"}}}}
	for _, relative := range []string{filepath.Join("installs", packageName, "bin"), filepath.Join("langlibs", "gopath", "bin")} {
		if err := os.MkdirAll(filepath.Join(original, relative), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeEnvironment(Config{RootDir: original, OS: runtime.GOOS}, packages); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, root); err != nil {
		t.Fatal(err)
	}
	return root, packageName
}

func testBashActivation(t *testing.T, root, packageName string) {
	command := exec.Command("bash", "--noprofile", "--norc", "-c",
		"source \"$1\"; source \"$1\"; printf '%s\\n%s\\n' \"$GOPATH\" \"$PATH\"", "activation", filepath.Join(root, "environment.bash"))
	checkActivation(t, command, root, packageName)
}

func testFishActivation(t *testing.T, root, packageName string) {
	if _, err := exec.LookPath("fish"); err != nil {
		if os.Getenv("PORTPROG_REQUIRE_FISH") == "1" {
			t.Fatalf("Fish activation coverage is required but fish was not found: %v", err)
		}
		t.Skip("Fish is unavailable; CI requires Fish activation coverage on Linux and macOS")
	}
	command := exec.Command("fish", "--no-config", "-c",
		"source $argv[1]; source $argv[1]; printf '%s\\n%s\\n' \"$GOPATH\" (string join : $PATH)", filepath.Join(root, "environment.fish"))
	checkActivation(t, command, root, packageName)
}

func testPowerShellActivation(t *testing.T, root, packageName, executable string) {
	command := exec.Command(executable, "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command",
		"$ErrorActionPreference = 'Stop'; [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); . $env:PORTPROG_ACTIVATION; . $env:PORTPROG_ACTIVATION; [Console]::WriteLine($env:GOPATH); [Console]::WriteLine($env:PATH)")
	command.Env = append(os.Environ(), "PORTPROG_ACTIVATION="+filepath.Join(root, "environment.ps1"))
	checkActivation(t, command, root, packageName)
}

func testBatchActivation(t *testing.T, root, packageName string) {
	command := exec.Command("cmd.exe", "/D", "/V:OFF", "/Q", "/U")
	command.Env = append(os.Environ(), "PORTPROG_ACTIVATION="+filepath.Join(root, "environment.bat"))
	command.Stdin = strings.NewReader("for /f \"tokens=2 delims=:\" %C in ('chcp') do for %N in (%C) do set PORTPROG_CODEPAGE_BEFORE=%N\r\n\"%PORTPROG_ACTIVATION%\"\r\nif errorlevel 1 exit /b 1\r\n\"%PORTPROG_ACTIVATION%\"\r\nif errorlevel 1 exit /b 1\r\nfor /f \"tokens=2 delims=:\" %C in ('chcp') do for %N in (%C) do set PORTPROG_CODEPAGE_AFTER=%N\r\nset PORTPROG_CODEPAGE_\r\nset GOPATH\r\nset PATH\r\n")
	output, err := command.CombinedOutput()
	if len(output)%2 != 0 {
		t.Fatalf("Command Prompt returned %d bytes; expected UTF-16 output: %q", len(output), output)
	}
	characters := make([]uint16, len(output)/2)
	for index := range characters {
		characters[index] = binary.LittleEndian.Uint16(output[index*2:])
	}
	text := string(utf16.Decode(characters))
	if err != nil {
		t.Fatalf("Command Prompt activation failed: %v\n%s", err, text)
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		name, value, found := strings.Cut(line, "=")
		if found {
			values[strings.ToUpper(name)] = value
		}
	}
	if values["PORTPROG_CODEPAGE_BEFORE"] == "" || values["PORTPROG_CODEPAGE_BEFORE"] != values["PORTPROG_CODEPAGE_AFTER"] {
		t.Errorf("Command Prompt code page changed: before %q, after %q", values["PORTPROG_CODEPAGE_BEFORE"], values["PORTPROG_CODEPAGE_AFTER"])
	}
	checkActivationValues(t, values["GOPATH"], values["PATH"], root, packageName, os.Getenv("PATH"))
}

func checkActivation(t *testing.T, command *exec.Cmd, root, packageName string) {
	t.Helper()
	if command.Env == nil {
		command.Env = os.Environ()
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s activation failed: %v\n%s", command.Path, err, output)
	}
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("%s activation output = %q; expected GOPATH and PATH on two lines", command.Path, output)
	}
	checkActivationValues(t, lines[0], lines[1], root, packageName, os.Getenv("PATH"))
}

func checkActivationValues(t *testing.T, goPath, pathValue, root, packageName, originalPath string) {
	t.Helper()
	expectedGoPath := filepath.Join(root, "langlibs", "gopath")
	if filepath.Clean(goPath) != expectedGoPath {
		t.Errorf("GOPATH = %q; expected relocated workspace %q", goPath, expectedGoPath)
	}
	expected := []string{filepath.Join(root, "installs", packageName), filepath.Join(root, "installs", packageName, "bin"), filepath.Join(expectedGoPath, "bin")}
	paths := filepath.SplitList(pathValue)
	for i, directory := range expected {
		if len(paths) <= i || filepath.Clean(paths[i]) != directory {
			t.Errorf("PATH position %d = %q; expected %q", i, paths, directory)
		}
		count := 0
		for _, entry := range paths {
			if filepath.Clean(entry) == directory {
				count++
			}
		}
		if count != 1 {
			t.Errorf("installed directory %q occurs %d times in PATH; expected once after repeated activation", directory, count)
		}
	}
	for _, previous := range filepath.SplitList(originalPath) {
		if !containsEnvironmentPath(paths, previous) {
			t.Errorf("original PATH directory %q was lost; resulting PATH = %q", previous, paths)
		}
	}
}

func containsEnvironmentPath(paths []string, value string) bool {
	for _, candidate := range paths {
		if candidate == value || runtime.GOOS == "windows" && strings.EqualFold(candidate, value) {
			return true
		}
	}
	return false
}
