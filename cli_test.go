package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorCacheAcceptsAbsenceAndRejectsCorruption(t *testing.T) {
	app := testApp(t, true)
	var output bytes.Buffer
	app.Deps.Output = &output
	payload := []byte("verified cache object")
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	lock := rawTestLock("https://example.com/tiny-tool", digest, int64(len(payload)), "1.0.0")
	if err := ensureStateDirectories(app); err != nil {
		t.Fatalf("prepare doctor cache test: expected success, got %v", err)
	} else if err := doctorCacheState(app, lock); err != nil {
		t.Fatalf("check absent cache: expected informational success, got %v", err)
	} else if err := os.WriteFile(cachePathForTest(app, digest), []byte("corrupt"), defaultFileMode); err != nil {
		t.Fatalf("write corrupt cache fixture: expected success, got %v", err)
	} else if err := doctorCacheState(app, lock); err == nil {
		t.Fatal("check corrupt cache: expected rejection, got success")
	} else if !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("check corrupt cache: expected damaged explanation, got %v", err)
	} else {
		return
	}
}

func TestDoctorValidatesCatalogEvenWhenLockExists(t *testing.T) {
	app := testApp(t, true)
	var output bytes.Buffer
	app.Deps.Output = &output
	if err := ensureStateDirectories(app); err != nil {
		t.Fatalf("prepare doctor state: expected success, got %v", err)
	} else if err := writeLock(app, rawTestLock("https://example.com/tiny-tool", strings.Repeat("a", sha256HexLength), 1, "1.0.0")); err != nil {
		t.Fatalf("write doctor lock fixture: expected success, got %v", err)
	} else if err := os.MkdirAll(app.Paths.CatalogDir, defaultDirMode); err != nil {
		t.Fatalf("prepare invalid catalog directory: expected success, got %v", err)
	} else if err := os.MkdirAll(app.Paths.ProfilesDir, defaultDirMode); err != nil {
		t.Fatalf("prepare profile directory: expected success, got %v", err)
	} else if err := os.WriteFile(filepath.Join(app.Paths.CatalogDir, "broken.json"), []byte("{not-json}"), defaultFileMode); err != nil {
		t.Fatalf("write invalid catalog fixture: expected success, got %v", err)
	} else if err := commandDoctor(app, nil); err == nil {
		t.Fatal("doctor invalid catalog with valid lock: expected rejection, got success")
	} else {
		return
	}
}
