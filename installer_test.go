package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func installerTestConfig(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	return Config{RootDir: root, ZipDir: filepath.Join(root, "cache"), TempDir: filepath.Join(root, "temp"), InstallDir: filepath.Join(root, "installs"), GoPath: filepath.Join(root, "langlibs", "gopath"), OS: runtime.GOOS, Arch: runtime.GOARCH, MaxDownloadBytes: 1 << 20, MaxExtractBytes: 1 << 20}
}

func installerTestFileURL(filename string) string {
	name := filepath.ToSlash(filename)
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	return (&url.URL{Scheme: "file", Path: name}).String()
}

func installerTestWrite(t *testing.T, filename string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func installerTestDigest(contents []byte) string {
	hash := sha256.Sum256(contents)
	return hex.EncodeToString(hash[:])
}

func installerTestZip(t *testing.T, entries []InstalledFile, contents map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.Path, Method: zip.Deflate}
		header.SetMode(os.FileMode(entry.Mode))
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		text := contents[entry.Path]
		if entry.LinkTarget != "" {
			text = entry.LinkTarget
		}
		if _, err := io.WriteString(file, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestArtifactFetchValidation(t *testing.T) {
	config := installerTestConfig(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/missing":
			http.Error(writer, "missing", http.StatusNotFound)
		case "/large":
			fmt.Fprint(writer, strings.Repeat("a", 1024))
		case "/slow":
			time.Sleep(80 * time.Millisecond)
			fmt.Fprint(writer, "content")
		default:
			fmt.Fprint(writer, "content")
		}
	}))
	defer server.Close()
	client := server.Client()
	digest, size, err := fetchArtifact(config, client, server.URL, installerTestDigest([]byte("content")))
	if err != nil || size != 7 {
		t.Fatalf("valid artifact: digest %q size %d error %v", digest, size, err)
	}
	if err := verifyArtifact(filepath.Join(config.ZipDir, digest), digest, size, 1024); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fetchArtifact(config, client, server.URL+"/missing", ""); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected HTTP status error, received %v", err)
	}
	if _, _, err := fetchArtifact(config, client, server.URL, strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected digest mismatch")
	}
	config.MaxDownloadBytes = 32
	if _, _, err := fetchArtifact(config, client, server.URL+"/large", ""); err == nil {
		t.Fatal("expected download bound")
	}
	client.Timeout = 10 * time.Millisecond
	if _, _, err := fetchArtifact(config, client, server.URL+"/slow", ""); err == nil {
		t.Fatal("expected request timeout")
	}
	config.Offline = true
	if _, _, err := fetchArtifact(config, client, server.URL, ""); err == nil {
		t.Fatal("expected offline refusal")
	}
}

func TestArchiveRejectsEscapeAndLinks(t *testing.T) {
	for _, entry := range []InstalledFile{
		{Path: "../outside", Mode: 0644},
		{Path: "/outside", Mode: 0644},
		{Path: "C:/outside", Mode: 0644},
		{Path: "a\\..\\outside", Mode: 0644},
		{Path: "link", Mode: uint32(os.ModeSymlink | 0777), LinkTarget: "../outside"},
		{Path: "link", Mode: uint32(os.ModeSymlink | 0777), LinkTarget: "/outside"},
	} {
		t.Run(strings.ReplaceAll(entry.Path+entry.LinkTarget, "/", "_"), func(t *testing.T) {
			config := installerTestConfig(t)
			archive := filepath.Join(config.RootDir, "input.zip")
			installerTestWrite(t, archive, installerTestZip(t, []InstalledFile{entry}, map[string]string{}), 0644)
			if err := extractArchive(config, archive, "input.zip", filepath.Join(config.RootDir, "extracted")); err == nil {
				t.Fatalf("accepted unsafe entry %+v", entry)
			}
		})
	}
	config := installerTestConfig(t)
	archive := filepath.Join(config.RootDir, "collision.zip")
	entries := []InstalledFile{{Path: "target", Mode: 0644}, {Path: "link", Mode: uint32(os.ModeSymlink | 0777), LinkTarget: "target"}, {Path: "link/child", Mode: 0644}}
	installerTestWrite(t, archive, installerTestZip(t, entries, map[string]string{"target": "safe"}), 0644)
	if err := extractArchive(config, archive, "collision.zip", filepath.Join(config.RootDir, "extracted")); err == nil {
		t.Fatal("accepted symlink as parent directory")
	}
	indirect := filepath.Join(config.RootDir, "indirect.zip")
	entries = []InstalledFile{
		{Path: "dir/", Mode: uint32(os.ModeDir | 0755)},
		{Path: "a", Mode: uint32(os.ModeSymlink | 0777), LinkTarget: "dir"},
		{Path: "dir/b", Mode: uint32(os.ModeSymlink | 0777), LinkTarget: ".."},
		{Path: "escape", Mode: uint32(os.ModeSymlink | 0777), LinkTarget: "a/b/../outside"},
	}
	installerTestWrite(t, indirect, installerTestZip(t, entries, map[string]string{}), 0644)
	if err := extractArchive(config, indirect, "indirect.zip", filepath.Join(config.RootDir, "indirect")); err == nil {
		t.Fatal("accepted indirect symlink escape")
	}
}

func TestInstallReceiptBackupAndFailedReplacement(t *testing.T) {
	config := installerTestConfig(t)
	config.Offline = true
	archive := installerTestZip(t, []InstalledFile{
		{Path: "release/bin/program", Mode: 0755},
		{Path: "release/bin/alias", Mode: uint32(os.ModeSymlink | 0777), LinkTarget: "program"},
	}, map[string]string{"release/bin/program": "first"})
	digest, size, err := cacheArtifact(config, bytes.NewReader(archive), "")
	if err != nil {
		t.Fatal(err)
	}
	locked := LockedPackage{Package: Package{Name: "sample", Zip: "program.zip", Fetch: "web", Plan: "zipWithDirectory", BinDir: "bin"}, SHA256: digest, Size: size}
	if err := installPackage(config, locked); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(config.InstallDir, "sample")
	data, err := os.ReadFile(filepath.Join(installed, ".portprog-receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	actual, err := collectInstalledFiles(installed)
	if err != nil || !reflect.DeepEqual(receipt.Files, actual) {
		t.Fatalf("receipt mismatch: recorded %+v actual %+v error %v", receipt.Files, actual, err)
	}
	if err := installPackage(config, locked); err != nil {
		t.Fatal(err)
	}
	backups, err := os.ReadDir(filepath.Join(config.RootDir, "backups"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected retained backup: %v %v", backups, err)
	}
	installerTestWrite(t, filepath.Join(config.ZipDir, digest), []byte("damaged"), 0644)
	if err := installPackage(config, locked); err == nil {
		t.Fatal("accepted corrupt cache")
	}
	content, err := os.ReadFile(filepath.Join(installed, "bin", "program"))
	if err != nil || string(content) != "first" {
		t.Fatalf("failed replacement changed existing installation: %q %v", content, err)
	}
}

func TestResolveLocalArtifactAndOfflineCache(t *testing.T) {
	config := installerTestConfig(t)
	filename := filepath.Join(config.RootDir, "data.txt")
	installerTestWrite(t, filename, []byte("local data"), 0644)
	address := installerTestFileURL(filename)
	requested := Package{Name: "data", Zip: "data.txt", Url: address, Fetch: "web", Plan: "rawData", SHA256: installerTestDigest([]byte("local data"))}
	locked, err := resolvePackage(config, requested)
	if err != nil {
		t.Fatal(err)
	}
	config.Offline = true
	resolved, err := resolvePackage(config, requested)
	if err != nil || resolved.SHA256 != locked.SHA256 {
		t.Fatalf("offline cache lookup: %+v %v", resolved, err)
	}
	if err := installPackage(config, resolved); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(config.InstallDir, "data", "data.txt"))
	if err != nil || string(content) != "local data" {
		t.Fatalf("raw data install: %q %v", content, err)
	}
	requested.Url = "http://example.invalid/artifact"
	requested.SHA256 = ""
	config.Offline = false
	if _, err := resolvePackage(config, requested); err == nil {
		t.Fatal("accepted insecure remote URL")
	}
}

func TestTarIntegrityAndExtractionLimits(t *testing.T) {
	config := installerTestConfig(t)
	var output bytes.Buffer
	compressed := gzip.NewWriter(&output)
	writer := tar.NewWriter(compressed)
	if err := writer.WriteHeader(&tar.Header{Name: "data", Mode: 0644, Size: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(config.RootDir, "input.tar.gz")
	installerTestWrite(t, filename, output.Bytes(), 0644)
	if err := extractArchive(config, filename, "input.tar.gz", filepath.Join(config.RootDir, "good")); err != nil {
		t.Fatal(err)
	}
	config.MaxExtractBytes = 3
	if err := extractArchive(config, filename, "input.tar.gz", filepath.Join(config.RootDir, "bounded")); err == nil {
		t.Fatal("accepted extraction beyond limit")
	}
	config.MaxExtractBytes = 1 << 20
	corrupted := append([]byte{}, output.Bytes()...)
	corrupted[len(corrupted)-8] ^= 0xff
	broken := filepath.Join(config.RootDir, "broken.tar.gz")
	installerTestWrite(t, broken, corrupted, 0644)
	if err := extractArchive(config, broken, "broken.tar.gz", filepath.Join(config.RootDir, "broken")); err == nil {
		t.Fatal("accepted gzip checksum corruption")
	}
}

func TestGoSourceBuildAndOfflineRefusal(t *testing.T) {
	config := installerTestConfig(t)
	stage := filepath.Join(config.TempDir, "go-build")
	source := filepath.Join(stage, "source")
	installerTestWrite(t, filepath.Join(source, "go.mod"), []byte("module example.invalid/localprogram\n\ngo 1.22\n"), 0644)
	installerTestWrite(t, filepath.Join(source, "main.go"), []byte("package main\nimport \"fmt\"\nfunc main() { fmt.Println(\"built locally\") }\n"), 0644)
	prefix, err := buildSource(config, Package{Name: "localprogram", Plan: "goGetAndMake"}, source, stage)
	if err != nil {
		t.Fatal(err)
	}
	executable := "localprogram"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	output, err := exec.Command(filepath.Join(prefix, "bin", executable)).Output()
	if err != nil || string(output) != "built locally\n" {
		t.Fatalf("built program: %q %v", output, err)
	}
	config.Offline = true
	if _, err := buildSource(config, Package{Name: "localprogram", Plan: "goGetAndMake"}, source, filepath.Join(config.TempDir, "offline")); err == nil {
		t.Fatal("accepted an offline source build")
	}
}

func installerTestRun(t *testing.T, directory, executable string, arguments ...string) string {
	t.Helper()
	command := exec.Command(executable, arguments...)
	command.Dir = directory
	command.Env = installerEnvironment([]string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_AUTHOR_NAME=Portprog test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Portprog test", "GIT_COMMITTER_EMAIL=test@example.invalid"})
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("command %q %q in %q: %v\n%s", executable, arguments, directory, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestPinnedGitSnapshotAndSubmodule(t *testing.T) {
	config := installerTestConfig(t)
	library := filepath.Join(config.RootDir, "library")
	installerTestWrite(t, filepath.Join(library, "library.txt"), []byte("pinned dependency"), 0644)
	installerTestRun(t, library, "git", "init", "-b", "main")
	installerTestRun(t, library, "git", "add", "library.txt")
	installerTestRun(t, library, "git", "commit", "-m", "First dependency revision")
	source := filepath.Join(config.RootDir, "repository")
	installerTestWrite(t, filepath.Join(source, "main.txt"), []byte("pinned parent"), 0644)
	installerTestWrite(t, filepath.Join(source, "Makefile"), []byte("all:\n\ttrue\ninstall:\n\tmkdir -p '$(PREFIX)/dependency'\n\tcp main.txt '$(PREFIX)/main.txt'\n\tcp dependency/library.txt '$(PREFIX)/dependency/library.txt'\n"), 0644)
	installerTestRun(t, source, "git", "init", "-b", "main")
	installerTestRun(t, source, "git", "-c", "protocol.file.allow=always", "submodule", "add", library, "dependency")
	installerTestRun(t, source, "git", "add", "main.txt", "Makefile", ".gitmodules", "dependency")
	installerTestRun(t, source, "git", "commit", "-m", "Parent with pinned dependency")
	commit := installerTestRun(t, source, "git", "rev-parse", "HEAD")
	installerTestWrite(t, filepath.Join(library, "library.txt"), []byte("new incompatible dependency"), 0644)
	installerTestRun(t, library, "git", "add", "library.txt")
	installerTestRun(t, library, "git", "commit", "-m", "Dependency changes independently")
	requested := Package{ID: "source", Name: "source", OS: "all", Arch: "all", Fetch: "git", Url: installerTestFileURL(source), Branch: "main", Plan: "gitAndMake"}
	locked, err := resolvePackage(config, requested)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Commit != commit {
		t.Fatalf("parent commit %q, expected %q", locked.Commit, commit)
	}
	second, err := resolvePackage(config, requested)
	if err != nil || second.SHA256 != locked.SHA256 {
		t.Fatalf("snapshot was not deterministic: first %s second %s error %v", locked.SHA256, second.SHA256, err)
	}
	config.Offline = true
	extracted := filepath.Join(config.RootDir, "offline-source")
	if err := extractArchive(config, filepath.Join(config.ZipDir, locked.SHA256), "source.zip", extracted); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(extracted, "dependency", "library.txt"))
	if err != nil || string(content) != "pinned dependency" {
		t.Fatalf("submodule did not honor pinned revision: %q %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(extracted, ".git")); !os.IsNotExist(err) {
		t.Fatalf("snapshot included Git metadata: %v", err)
	}
	if err := installPackage(config, locked); err == nil {
		t.Fatal("accepted source build in offline mode")
	}
	pinned := requested
	pinned.Branch, pinned.SHA256 = locked.Commit, locked.SHA256
	fromCache, err := resolvePackage(config, pinned)
	if err != nil || fromCache.Commit != locked.Commit {
		t.Fatalf("cached Git resolution lost commit: %+v %v", fromCache, err)
	}
	installerTestWrite(t, filepath.Join(source, "main.txt"), []byte("later parent"), 0644)
	installerTestRun(t, source, "git", "add", "main.txt")
	installerTestRun(t, source, "git", "commit", "-m", "Parent branch advances")
	if err := os.Rename(filepath.Join(config.ZipDir, locked.SHA256), filepath.Join(config.RootDir, "retained-cache")); err != nil {
		t.Fatal(err)
	}
	config.Offline = false
	if err := installPackage(config, locked); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(filepath.Join(config.InstallDir, "source", "main.txt"))
	if err != nil || string(content) != "pinned parent" {
		t.Fatalf("cache restoration floated parent revision: %q %v", content, err)
	}
}

func TestWindowsFileURLPath(t *testing.T) {
	address, err := url.Parse("file:///C:/Packages/My%20Archive.zip")
	if err != nil {
		t.Fatal(err)
	}
	filename, err := localArtifactPath(address, true)
	if err != nil || filename != "C:\\Packages\\My Archive.zip" {
		t.Fatalf("Windows file URL: filename %q error %v", filename, err)
	}
	address.Path = "/relative/archive.zip"
	if _, err := localArtifactPath(address, true); err == nil {
		t.Fatal("accepted Windows local URL without a drive")
	}
}

func TestInstallerSelectsReceiptDependencyBinary(t *testing.T) {
	config := installerTestConfig(t)
	config.MaxDownloadBytes = 64 << 20
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	name := "portprog-receipt-tool"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	installed := filepath.Join(config.InstallDir, "tool")
	binary := filepath.Join(installed, "bin", name)
	installerTestWrite(t, binary, contents, 0755)
	files, err := collectInstalledFiles(installed)
	if err != nil {
		t.Fatal(err)
	}
	locked := LockedPackage{Package: Package{ID: "tool", Name: "tool", OS: "all", Arch: "all", Fetch: "web", Plan: "zipWithNoDirectory", Url: "https://example.invalid/tool.zip", Zip: "tool.zip", BinDir: "bin"}, SHA256: installerTestDigest(contents), Size: int64(len(contents))}
	receipt, err := json.Marshal(Receipt{SchemaVersion: 1, Package: locked, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	installerTestWrite(t, filepath.Join(installed, ".portprog-receipt.json"), receipt, 0644)
	command, err := installerCommand(config, config.RootDir, nil, name)
	if err != nil || command.Path != binary {
		t.Fatalf("dependency executable lookup: command %+v error %v; expected %s", command, err, binary)
	}
}

func TestConfigureAndCMakeStageInstallation(t *testing.T) {
	config := installerTestConfig(t)
	stage := filepath.Join(config.TempDir, "configure-build")
	source := filepath.Join(stage, "source")
	configure := "#!/bin/sh\nset -eu\nprintf 'prefix=%s\\n' \"${1#--prefix=}\" > prefix.mk\n"
	makefile := "include prefix.mk\nall:\n\tprintf 'built with prefix %s' '$(prefix)' > program\ninstall:\n\tmkdir -p '$(DESTDIR)$(prefix)/bin'\n\tcp program '$(DESTDIR)$(prefix)/bin/program'\n"
	installerTestWrite(t, filepath.Join(source, "configure"), []byte(configure), 0755)
	installerTestWrite(t, filepath.Join(source, "Makefile"), []byte(makefile), 0644)
	prefix, err := buildSource(config, Package{Name: "configured", Plan: "standardConfigure"}, source, stage)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(prefix, "bin", "program"))
	expected := "built with prefix " + filepath.Join(config.InstallDir, "configured")
	if runtime.GOOS == "windows" {
		expected = "built with prefix " + filepath.ToSlash(filepath.Join(stage, "prefix"))
	}
	if err != nil || string(content) != expected {
		t.Fatalf("configure installed wrong prefix: %q expected %q error %v", content, expected, err)
	}
	if _, err := os.Stat(filepath.Join(config.InstallDir, "configured")); !os.IsNotExist(err) {
		t.Fatalf("configure wrote to final install before publication: %v", err)
	}
	cmakeStage := filepath.Join(config.TempDir, "cmake-build")
	cmakeSource := filepath.Join(cmakeStage, "source")
	installerTestWrite(t, filepath.Join(cmakeSource, "program.txt"), []byte("cmake installed file"), 0644)
	installerTestWrite(t, filepath.Join(cmakeSource, "CMakeLists.txt"), []byte("cmake_minimum_required(VERSION 3.15)\nproject(LocalFixture NONE)\ninstall(FILES program.txt DESTINATION bin)\n"), 0644)
	prefix, err = buildSource(config, Package{Name: "cmake-built", Plan: "cmake"}, cmakeSource, cmakeStage)
	if err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(filepath.Join(prefix, "bin", "program.txt"))
	if err != nil || string(content) != "cmake installed file" {
		t.Fatalf("cmake staging: %q %v", content, err)
	}
}

func TestAutogenSourceBuild(t *testing.T) {
	config := installerTestConfig(t)
	stage := filepath.Join(config.TempDir, "autogen-build")
	source := filepath.Join(stage, "source")
	installerTestWrite(t, filepath.Join(source, "autogen.sh"), []byte("#!/bin/sh\nset -eu\nprintf 'prefix=%s\\n' \"${1#--prefix=}\" > prefix.mk\n"), 0755)
	installerTestWrite(t, filepath.Join(source, "Makefile"), []byte("include prefix.mk\nall:\n\tprintf 'autogen configured' > program\ninstall:\n\tmkdir -p '$(DESTDIR)$(prefix)/bin'\n\tcp program '$(DESTDIR)$(prefix)/bin/program'\n"), 0644)
	prefix, err := buildSource(config, Package{Name: "autogen", Plan: "standardConfigure"}, source, stage)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(prefix, "bin", "program"))
	if err != nil || string(content) != "autogen configured" {
		t.Fatalf("autogen build produced %q: %v", content, err)
	}
}

func TestMacPackagePayloadExtraction(t *testing.T) {
	config := installerTestConfig(t)
	if runtime.GOOS != "darwin" {
		if _, err := prepareNativePackage(config, Package{Name: "pkg-fixture", Plan: "pkg"}, "unused.pkg", config.TempDir); err == nil || !strings.Contains(err.Error(), "requires macOS") {
			t.Fatalf("expected macOS platform requirement, received %v", err)
		}
		return
	}
	payload := filepath.Join(config.RootDir, "package-payload")
	installerTestWrite(t, filepath.Join(payload, "fixtures", "installed.txt"), []byte("local pkg payload\n"), 0644)
	archive := filepath.Join(config.RootDir, "fixture.pkg")
	if err := runInstallerCommand(config, config.RootDir, "pkgbuild", "--root", payload, "--identifier", "invalid.example.portprog.fixture", "--version", "1", "--install-location", "/", archive); err != nil {
		t.Fatal(err)
	}
	recipe := Package{ID: "pkg-fixture", Name: "pkg-fixture", OS: "darwin", Arch: "all", Fetch: "web", Plan: "pkg", Zip: "fixture.pkg", Url: installerTestFileURL(archive)}
	locked, err := resolvePackage(config, recipe)
	if err != nil {
		t.Fatal(err)
	}
	if err := installPackage(config, locked); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(config.InstallDir, "pkg-fixture")
	content, err := os.ReadFile(filepath.Join(installed, "fixtures", "installed.txt"))
	if err != nil || string(content) != "local pkg payload\n" {
		t.Fatalf("pkg extraction produced %q: %v", content, err)
	}
	locked.Package.ArchiveRoot = "fixtures"
	if err := installPackage(config, locked); err != nil {
		t.Fatal(err)
	}
	for _, archiveRoot := range []string{"missing", "../outside"} {
		locked.Package.ArchiveRoot = archiveRoot
		if err := installPackage(config, locked); err == nil {
			t.Fatalf("accepted invalid pkg ArchiveRoot %q", archiveRoot)
		}
		content, err := os.ReadFile(filepath.Join(installed, "installed.txt"))
		if err != nil || string(content) != "local pkg payload\n" {
			t.Fatalf("failed pkg extraction changed previous install: %q %v", content, err)
		}
	}
}

func TestFailedBuildStopsBeforeInstall(t *testing.T) {
	config := installerTestConfig(t)
	stage := filepath.Join(config.TempDir, "failed-build")
	source := filepath.Join(stage, "source")
	installerTestWrite(t, filepath.Join(source, "Makefile"), []byte("all:\n\tfalse\ninstall:\n\tmkdir -p '$(PREFIX)/bin'\n\tprintf incorrect > '$(PREFIX)/bin/installed'\n"), 0644)
	if _, err := buildSource(config, Package{Name: "failed", Plan: "gitAndMake"}, source, stage); err == nil || !strings.Contains(err.Error(), "complete output:") {
		t.Fatalf("expected failure with retained command log, received %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "prefix", "bin", "installed")); !os.IsNotExist(err) {
		t.Fatalf("install ran after failed build: %v", err)
	}
}

func TestSevenZipListingPreflight(t *testing.T) {
	root := t.TempDir()
	valid := []sevenZipEntry{{name: "bin", dir: true}, {name: "bin/tool", size: 4}, {name: "data/naïve.txt", size: 5}}
	if err := validateSevenZipEntries(valid, root, 9); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]sevenZipEntry{
		"parent escape":       {{name: "../outside", size: 1}},
		"absolute":            {{name: "/outside", size: 1}},
		"device":              {{name: "NUL", size: 1}},
		"noncanonical":        {{name: "a/../b", size: 1}},
		"duplicate":           {{name: "file", size: 1}, {name: "file", size: 1}},
		"case collision":      {{name: "File", size: 1}, {name: "file", size: 1}},
		"implicit case":       {{name: "Bin/a", size: 1}, {name: "bin/b", size: 1}},
		"file parent":         {{name: "parent", size: 1}, {name: "parent/file", size: 1}},
		"parent becomes file": {{name: "parent/file", size: 1}, {name: "parent", size: 1}},
		"aggregate bytes":     {{name: "one", size: 6}, {name: "two", size: 6}},
		"negative bytes":      {{name: "one", size: -1}},
		"directory bytes":     {{name: "directory", size: 1, dir: true}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateSevenZipEntries(entries, root, 10); err == nil {
				t.Fatalf("accepted invalid listing %+v", entries)
			}
		})
	}
	if err := validateSevenZipEntries(make([]sevenZipEntry, 200001), root, 10); err == nil {
		t.Fatal("accepted excessive entry count")
	}
	for _, metadata := range []string{"Attributes = A lrwxrwxrwx", "Symbolic Link = target", "Hard Link = target"} {
		listing := []byte("7z listing\n----------\nPath = link\nSize = 6\n" + metadata + "\n\n")
		if _, err := sevenZipEntries(listing); err == nil {
			t.Fatalf("accepted link metadata %q", metadata)
		}
	}
	listing := []byte("7z listing\n----------\nPath = data/naïve.txt\nSize = 5\nAttributes = A -rw-r--r--\n\n")
	entries, err := sevenZipEntries(listing)
	if err != nil || len(entries) != 1 || entries[0].name != "data/naïve.txt" {
		t.Fatalf("UTF-8 listing decode: %+v %v", entries, err)
	}
}

func TestSevenZipSolidArchiveIntegration(t *testing.T) {
	config := installerTestConfig(t)
	executable, err := sevenZipCommand(config)
	if err != nil {
		if os.Getenv("PORTPROG_REQUIRE_7ZIP") == "1" {
			t.Fatalf("real 7z integration is required: %v", err)
		}
		if !strings.Contains(err.Error(), "neither executable was found") {
			t.Fatal(err)
		}
		t.Log("7zz/7z is unavailable; verified the explicit missing-extractor error. Real solid-archive extraction requires PORTPROG_REQUIRE_7ZIP=1 and an installed extractor.")
		return
	}
	source := filepath.Join(config.RootDir, "solid-source")
	for index := 0; index < 64; index++ {
		name := filepath.Join(source, "release", "data", fmt.Sprintf("file-%03d.txt", index))
		installerTestWrite(t, name, []byte(fmt.Sprintf("solid archive file %d\n", index)), 0644)
	}
	installerTestWrite(t, filepath.Join(source, "release", "bin", "naïve-tool"), []byte("complete UTF-8 path\n"), 0755)
	archive := filepath.Join(config.RootDir, "solid.7z")
	if err := runInstallerCommand(config, source, executable, "a", "-t7z", "-ms=on", "-mx=1", "--", archive, "release"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(config.TempDir)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(config.RootDir, "solid-extracted")
	if err := extractArchive(config, archive, "solid.7z", destination); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(config.TempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after)-len(before) != 2 {
		t.Fatalf("solid archive required %d command logs; expected one listing and one extraction", len(after)-len(before))
	}
	expected, err := collectInstalledFiles(source)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := collectInstalledFiles(destination)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("solid archive contents differ: expected %+v actual %+v error %v", expected, actual, err)
	}
}
