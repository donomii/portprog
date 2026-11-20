package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflinePackRestoresVerifiedProfileWithoutNetwork(t *testing.T) {
	payload := []byte("offline portable tool")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	sourceApp := testApp(t, true)
	lock := rawTestLock("https://example.com/tiny-tool", digest, int64(len(payload)), "1.0.0")
	if err := os.MkdirAll(sourceApp.Paths.CacheObjects, defaultDirMode); err != nil {
		t.Fatalf("prepare source cache: expected success, got %v", err)
	} else if err := os.WriteFile(cachePathForTest(sourceApp, digest), payload, defaultFileMode); err != nil {
		t.Fatalf("write source cache object: expected success, got %v", err)
	} else {
		packPath, packErr := createOfflinePack(sourceApp, "packs/starter.zip", lock)
		if packErr != nil {
			t.Fatalf("create verified offline pack: expected success, got %v", packErr)
		} else {
			info, statErr := os.Stat(packPath)
			if statErr != nil {
				t.Fatalf("inspect verified offline pack: expected success, got %v", statErr)
			} else if info.Mode().Perm()&0o444 == 0 {
				t.Fatalf("inspect verified offline pack permissions: expected readable file, got mode %v", info.Mode())
			} else {
				targetApp := testApp(t, true)
				restoreErr := restoreOfflinePack(targetApp, packPath)
				if restoreErr != nil {
					t.Fatalf("restore verified offline pack: expected success without network, got %v", restoreErr)
				} else {
					assertFileExists(t, filepath.Join(targetApp.Paths.StoreRoot, "tiny-tool", "1.0.0", "darwin-arm64", "bin", "tiny-tool"))
					assertFileExists(t, targetApp.Paths.LockFile)
				}
			}
		}
	}
}

func TestOfflinePackCreationRejectsMissingCacheObject(t *testing.T) {
	app := testApp(t, true)
	lock := rawTestLock("https://example.com/tiny-tool", fmt.Sprintf("%064d", 1), 1, "1.0.0")
	_, err := createOfflinePack(app, "packs/starter.zip", lock)
	if err == nil {
		t.Fatal("create offline pack with missing cache object: expected rejection, got success")
	} else {
		return
	}
}

func TestOfflinePackCreationDoesNotReplaceDirectory(t *testing.T) {
	app := testApp(t, true)
	destination := filepath.Join(app.Paths.BaseDir, "packs", "starter.zip")
	if err := os.MkdirAll(destination, defaultDirMode); err != nil {
		t.Fatalf("prepare directory at pack destination: expected success, got %v", err)
	} else {
		lock := rawTestLock("https://example.com/tiny-tool", fmt.Sprintf("%064d", 1), 1, "1.0.0")
		_, err := createOfflinePack(app, "packs/starter.zip", lock)
		if err == nil {
			t.Fatal("create pack over directory: expected rejection, got success")
		} else {
			return
		}
	}
}
