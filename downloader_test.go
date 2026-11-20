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
	"time"
)

func TestDownloadAssetVerifiesAndReusesCache(t *testing.T) {
	payload := []byte("verified portable tool")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		hits++
		response.WriteHeader(http.StatusOK)
		_, err := response.Write(payload)
		if err != nil {
			t.Errorf("serve download fixture: expected success, got %v", err)
		} else {
			return
		}
	}))
	defer server.Close()

	app := testApp(t, false)
	app.AllowHTTPForTests = true
	asset := PackageAsset{URL: server.URL + "/tool", SHA256: digest, Size: int64(len(payload))}
	firstPath, firstErr := downloadAsset(app, "tiny-tool", asset)
	if firstErr != nil {
		t.Fatalf("download verified asset: expected success, got %v", firstErr)
	} else {
		secondPath, secondErr := downloadAsset(app, "tiny-tool", asset)
		if secondErr != nil {
			t.Fatalf("reuse verified cache asset: expected success, got %v", secondErr)
		} else {
			if firstPath != secondPath || hits != 1 {
				t.Fatalf("reuse verified cache asset: expected one request and identical paths, got hits=%d first=%q second=%q", hits, firstPath, secondPath)
			} else {
				return
			}
		}
	}
}

func TestDownloadAssetRejectsBadStatusDigestAndOversize(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		digest     string
		maximumMB  int64
		expectText string
	}{
		{name: "status", status: http.StatusNotFound, body: "missing", digest: strings.Repeat("0", sha256HexLength), maximumMB: 1, expectText: "status"},
		{name: "digest", status: http.StatusOK, body: "wrong", digest: strings.Repeat("0", sha256HexLength), maximumMB: 1, expectText: "SHA-256"},
		{name: "maximum", status: http.StatusOK, body: strings.Repeat("x", 2*1024*1024), digest: strings.Repeat("0", sha256HexLength), maximumMB: 1, expectText: "maximum"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				response.WriteHeader(test.status)
				_, err := response.Write([]byte(test.body))
				if err != nil && test.name != "maximum" {
					t.Errorf("serve rejection fixture: expected success, got %v", err)
				} else {
					// The oversized-response test deliberately lets the client stop reading.
					return
				}
			}))
			defer server.Close()
			app := testApp(t, false)
			app.AllowHTTPForTests = true
			app.Settings.MaxDownloadMB = test.maximumMB
			_, err := downloadAsset(app, "tiny-tool", PackageAsset{URL: server.URL, SHA256: test.digest})
			if err == nil {
				t.Fatalf("download invalid %s response: expected rejection, got success", test.name)
			} else {
				if !strings.Contains(err.Error(), test.expectText) {
					t.Fatalf("download invalid %s response: expected error containing %q, got %v", test.name, test.expectText, err)
				} else {
					return
				}
			}
		})
	}
}

func TestDownloadAssetOfflineRequiresVerifiedCache(t *testing.T) {
	app := testApp(t, true)
	_, err := downloadAsset(app, "tiny-tool", PackageAsset{URL: "https://example.com/tool", SHA256: strings.Repeat("c", sha256HexLength)})
	if err == nil {
		t.Fatal("download missing asset in offline mode: expected rejection, got success")
	} else {
		if !strings.Contains(err.Error(), "offline") {
			t.Fatalf("download missing asset in offline mode: expected offline explanation, got %v", err)
		} else {
			return
		}
	}
}

func TestDownloadRedirectRejectsHTTPDowngradeBeforeFollowing(t *testing.T) {
	payload := []byte("unsafe redirect target")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	targetHits := 0
	insecure := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		targetHits++
		response.WriteHeader(http.StatusOK)
		_, err := response.Write(payload)
		if err != nil {
			t.Errorf("serve downgrade target: expected success, got %v", err)
		} else {
			return
		}
	}))
	defer insecure.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, insecure.URL, http.StatusFound)
	}))
	defer secure.Close()
	app := testApp(t, false)
	app.Deps.HTTPClient = secure.Client()
	app.Deps.HTTPClient.CheckRedirect = redirectPolicy(false)
	_, err := downloadAsset(app, "tiny-tool", PackageAsset{URL: secure.URL, SHA256: digest, Size: int64(len(payload))})
	if err == nil {
		t.Fatal("follow HTTPS-to-HTTP redirect: expected rejection, got success")
	} else if targetHits != 0 {
		t.Fatalf("follow HTTPS-to-HTTP redirect: expected target not contacted, got %d requests", targetHits)
	} else if !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("follow HTTPS-to-HTTP redirect: expected HTTPS explanation, got %v", err)
	} else {
		return
	}
}

func testApp(t *testing.T, offline bool) App {
	t.Helper()
	baseDir := t.TempDir()
	settings := Settings{
		SchemaVersion: currentSchemaVersion, DefaultProfile: "starter",
		InstallRoot: "portable", CacheRoot: "cache", StateRoot: "state",
		TrashRoot: "trash", CatalogDir: "catalog", ProfilesDir: "profiles",
		LockFile: "portprog.lock.json", Offline: offline,
		MaxDownloadMB: 4, MaxExtractMB: 16, DownloadTimeoutSeconds: 5, BaseDir: baseDir,
	}
	paths, err := derivePaths(settings)
	if err != nil {
		t.Fatalf("derive test application paths: expected success, got %v", err)
		return App{}
	} else {
		return App{
			Settings: settings,
			Paths:    paths,
			Platform: Platform{OS: "darwin", Arch: "arm64"},
			Deps: Dependencies{
				HTTPClient: &http.Client{Timeout: 5 * time.Second},
				Now:        func() time.Time { return time.Date(2026, time.August, 27, 1, 2, 3, 0, time.UTC) },
				Input:      os.Stdin, Output: os.Stdout, Error: os.Stderr,
			},
		}
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("find expected file %q: got %v", path, err)
	} else {
		if !info.Mode().IsRegular() {
			t.Fatalf("check expected file %q: expected regular file, got mode %v", path, info.Mode())
		} else {
			return
		}
	}
}

func cachePathForTest(app App, digest string) string {
	return filepath.Join(app.Paths.CacheObjects, digest)
}
