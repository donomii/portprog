package main

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallLockedProfileIsVerifiedTransactionalAndIdempotent(t *testing.T) {
	payload := []byte("#!/bin/sh\necho tiny\n")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	hits := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		hits++
		response.WriteHeader(http.StatusOK)
		_, err := response.Write(payload)
		if err != nil {
			t.Errorf("serve installer fixture: expected success, got %v", err)
		} else {
			return
		}
	}))
	defer server.Close()

	app := testApp(t, false)
	app.Deps.HTTPClient = server.Client()
	lock := rawTestLock(server.URL+"/tiny", digest, int64(len(payload)), "1.0.0")
	installErr := installLockedProfile(app, lock)
	if installErr != nil {
		t.Fatalf("install valid locked profile: expected success, got %v", installErr)
	} else {
		health := packageHealth(app, lock.Packages[0])
		if health.State != "installed" {
			t.Fatalf("check installed package: expected installed, got %s: %s", health.State, health.Detail)
		} else {
			secondErr := installLockedProfile(app, lock)
			if secondErr != nil {
				t.Fatalf("repeat installed profile: expected idempotent success, got %v", secondErr)
			} else if hits != 1 {
				t.Fatalf("repeat installed profile: expected one download request, got %d", hits)
			} else {
				assertFileExists(t, filepath.Join(app.Paths.StoreRoot, "tiny-tool", "1.0.0", "darwin-arm64", "bin", "tiny-tool"))
				assertFileExists(t, receiptFilename(app, "tiny-tool"))
				assertFileExists(t, app.Paths.LockFile)
				assertFileExists(t, filepath.Join(app.Paths.InstallRoot, "go.mod"))
			}
		}
	}
}

func TestStateSetupDoesNotOverwriteForeignModuleBoundary(t *testing.T) {
	app := testApp(t, true)
	if err := os.MkdirAll(app.Paths.InstallRoot, defaultDirMode); err != nil {
		t.Fatalf("prepare foreign install root: expected success, got %v", err)
	} else {
		filename := filepath.Join(app.Paths.InstallRoot, "go.mod")
		foreign := []byte("module example.com/foreign\n")
		if err := os.WriteFile(filename, foreign, defaultFileMode); err != nil {
			t.Fatalf("write foreign module boundary: expected success, got %v", err)
		} else if err := ensureStateDirectories(app); err == nil {
			t.Fatal("prepare state with foreign module boundary: expected rejection, got success")
		} else {
			actual, readErr := os.ReadFile(filename)
			if readErr != nil {
				t.Fatalf("read retained foreign module boundary: expected success, got %v", readErr)
			} else if string(actual) != string(foreign) {
				t.Fatalf("retain foreign module boundary: expected %q, got %q", foreign, actual)
			} else {
				return
			}
		}
	}
}

func TestPackageHealthRejectsUnexpectedReceiptStorePath(t *testing.T) {
	app := testApp(t, true)
	lock := rawTestLock("https://example.com/tiny-tool", strings.Repeat("b", sha256HexLength), 1, "1.0.0")
	if err := ensureStateDirectories(app); err != nil {
		t.Fatalf("prepare receipt path test: expected success, got %v", err)
	} else {
		unexpected := filepath.Join(app.Paths.StoreRoot, "another-tool", "1.0.0", "darwin-arm64")
		receipt, receiptErr := makeReceipt(app, lock.Packages[0], unexpected)
		if receiptErr != nil {
			t.Fatalf("create unexpected-store receipt: expected success, got %v", receiptErr)
		} else if err := writeReceipt(app, receipt); err != nil {
			t.Fatalf("write unexpected-store receipt: expected schema-valid receipt, got %v", err)
		} else {
			health := packageHealth(app, lock.Packages[0])
			if health.State != "outdated" || !strings.Contains(health.Detail, "store path") {
				t.Fatalf("check unexpected receipt store: expected outdated store-path result, got %s: %s", health.State, health.Detail)
			} else {
				return
			}
		}
	}
}

func TestFailedReplacementRetainsPriorInstallation(t *testing.T) {
	payload := []byte("working version")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, err := response.Write(payload)
		if err != nil {
			t.Errorf("serve replacement fixture: expected success, got %v", err)
		} else {
			return
		}
	}))
	defer server.Close()
	app := testApp(t, false)
	app.Deps.HTTPClient = server.Client()
	firstLock := rawTestLock(server.URL, digest, int64(len(payload)), "1.0.0")
	if err := installLockedProfile(app, firstLock); err != nil {
		t.Fatalf("prepare prior installation: expected success, got %v", err)
	} else {
		brokenLock := rawTestLock(server.URL, digest, int64(len(payload)), "2.0.0")
		brokenLock.Packages[0].Asset.Executables = []string{"bin/missing-tool"}
		err := installLockedProfile(app, brokenLock)
		if err == nil {
			t.Fatal("install replacement missing expected executable: expected failure, got success")
		} else {
			receipt, receiptErr := readReceipt(app, "tiny-tool")
			if receiptErr != nil {
				t.Fatalf("read retained prior receipt: expected success, got %v", receiptErr)
			} else if receipt.Version != "1.0.0" {
				t.Fatalf("check retained prior receipt: expected version 1.0.0, got %q", receipt.Version)
			} else {
				assertFileExists(t, filepath.Join(app.Paths.StoreRoot, "tiny-tool", "1.0.0", "darwin-arm64", "bin", "tiny-tool"))
			}
		}
	}
}

func TestRemovePackageMovesInstallationAndReceiptToTrash(t *testing.T) {
	payload := []byte("remove me")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, err := response.Write(payload)
		if err != nil {
			t.Errorf("serve removal fixture: expected success, got %v", err)
		} else {
			return
		}
	}))
	defer server.Close()
	app := testApp(t, false)
	app.Deps.HTTPClient = server.Client()
	lock := rawTestLock(server.URL, digest, int64(len(payload)), "1.0.0")
	if err := installLockedProfile(app, lock); err != nil {
		t.Fatalf("prepare removable installation: expected success, got %v", err)
	} else {
		storeTrash, receiptTrash, removeErr := removePackage(app, "tiny-tool")
		if removeErr != nil {
			t.Fatalf("remove installed package: expected recoverable move, got %v", removeErr)
		} else {
			assertFileExists(t, filepath.Join(storeTrash, "bin", "tiny-tool"))
			assertFileExists(t, receiptTrash)
			_, receiptErr := os.Stat(receiptFilename(app, "tiny-tool"))
			if !os.IsNotExist(receiptErr) {
				t.Fatalf("check active receipt after removal: expected no file, got %v", receiptErr)
			} else {
				return
			}
		}
	}
}

func rawTestLock(url string, digest string, size int64, version string) LockFile {
	return LockFile{
		SchemaVersion: currentSchemaVersion,
		Profile:       "starter", ProfileDescription: "Installer test profile.",
		OS: "darwin", Arch: "arm64", GeneratedAt: "2026-08-27T01:02:03Z",
		Packages: []LockedPackage{{
			Name: "tiny-tool", Version: version, Description: "Tiny installer fixture.",
			License: "MIT", Dependencies: []string{},
			Asset: PackageAsset{
				OS: "darwin", Arch: "arm64", URL: url, SHA256: digest, Size: size,
				Format: "raw", StripComponents: 0, RawPath: "bin/tiny-tool",
				BinDirs: []string{"bin"}, Executables: []string{"bin/tiny-tool"},
			},
		}},
	}
}
