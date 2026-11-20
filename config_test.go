package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSettingsText = `
schema_version = 1
default_profile = starter
install_root = portable
cache_root = cache
state_root = state
trash_root = trash
catalog_dir = catalog
profiles_dir = profiles
lock_file = portprog.lock.json
offline = false
max_download_mb = 64
max_extract_mb = 256
download_timeout_seconds = 30
`

func TestParseSettingsAcceptsDocumentedConfiguration(t *testing.T) {
	settings, err := parseSettings(strings.NewReader(validSettingsText), "/tmp/portprog-base")
	if err != nil {
		t.Fatalf("parse valid settings: expected success, got %v", err)
	} else {
		if settings.DefaultProfile != "starter" {
			t.Fatalf("parse default profile: expected starter, got %q", settings.DefaultProfile)
		} else {
			if settings.MaxDownloadMB != 64 {
				t.Fatalf("parse maximum download: expected 64 MiB, got %d", settings.MaxDownloadMB)
			} else {
				if settings.BaseDir != "/tmp/portprog-base" {
					t.Fatalf("record base directory: expected /tmp/portprog-base, got %q", settings.BaseDir)
				} else {
					return
				}
			}
		}
	}
}

func TestParseSettingsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "unknown key", text: validSettingsText + "mystery = value\n"},
		{name: "duplicate key", text: validSettingsText + "offline = true\n"},
		{name: "absolute path", text: strings.Replace(validSettingsText, "cache_root = cache", "cache_root = /tmp/cache", 1)},
		{name: "parent traversal", text: strings.Replace(validSettingsText, "state_root = state", "state_root = ../state", 1)},
		{name: "bad boolean", text: strings.Replace(validSettingsText, "offline = false", "offline = perhaps", 1)},
		{name: "bad schema", text: strings.Replace(validSettingsText, "schema_version = 1", "schema_version = 2", 1)},
		{name: "missing setting", text: strings.Replace(validSettingsText, "default_profile = starter\n", "", 1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseSettings(strings.NewReader(test.text), "/tmp/portprog-base")
			if err == nil {
				t.Fatalf("parse invalid settings %q: expected an explanatory error, got success", test.name)
			} else {
				return
			}
		})
	}
}

func TestParseSettingsRejectsLimitsThatOverflowRuntimeUnits(t *testing.T) {
	tests := []struct {
		name         string
		oldSetting   string
		newSetting   string
		expectedText string
	}{
		{
			name:         "download byte limit",
			oldSetting:   "max_download_mb = 64",
			newSetting:   fmt.Sprintf("max_download_mb = %d", maximumConfiguredMebibytes+1),
			expectedText: "max_download_mb",
		},
		{
			name:         "extraction byte limit",
			oldSetting:   "max_extract_mb = 256",
			newSetting:   fmt.Sprintf("max_extract_mb = %d", maximumConfiguredMebibytes+1),
			expectedText: "max_extract_mb",
		},
		{
			name:         "request timeout",
			oldSetting:   "download_timeout_seconds = 30",
			newSetting:   fmt.Sprintf("download_timeout_seconds = %d", maximumDownloadTimeoutSeconds+1),
			expectedText: "download_timeout_seconds",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := strings.Replace(validSelfTestSettings(), test.oldSetting, test.newSetting, 1)
			_, err := parseSettings(strings.NewReader(input), t.TempDir())
			if err == nil {
				t.Fatalf("parse unsafe configuration: expected rejection containing %q, got success", test.expectedText)
			} else if !strings.Contains(err.Error(), test.expectedText) {
				t.Fatalf("parse unsafe configuration: expected error containing %q, got %v", test.expectedText, err)
			} else {
				return
			}
		})
	}
}

func TestDerivePathsKeepsAllWritableStateInsideBase(t *testing.T) {
	baseDir := t.TempDir()
	settings, err := parseSettings(strings.NewReader(validSettingsText), baseDir)
	if err != nil {
		t.Fatalf("prepare path test: expected valid settings, got %v", err)
	} else {
		paths, pathErr := derivePaths(settings)
		if pathErr != nil {
			t.Fatalf("derive configured paths: expected success, got %v", pathErr)
		} else {
			for _, path := range []string{paths.InstallRoot, paths.CacheRoot, paths.StateRoot, paths.TrashRoot, paths.LockFile} {
				contained, containErr := pathIsInside(paths.BaseDir, path)
				if containErr != nil {
					t.Fatalf("check configured path %q: expected success, got %v", path, containErr)
				} else {
					if !contained {
						t.Fatalf("check configured path %q: expected it under %q", path, paths.BaseDir)
					} else {
						continue
					}
				}
			}
		}
	}
}

func TestDerivePathsRejectsConfiguredSymlinkRoot(t *testing.T) {
	baseDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(baseDir, "cache")); err != nil {
		t.Skipf("create configured symlink fixture: %v", err)
	} else {
		settings, parseErr := parseSettings(strings.NewReader(validSettingsText), baseDir)
		if parseErr != nil {
			t.Fatalf("prepare symlink path test: expected valid settings, got %v", parseErr)
		} else if _, err := derivePaths(settings); err == nil {
			t.Fatal("derive configured symlink root: expected rejection, got success")
		} else {
			return
		}
	}
}

func TestDerivePathsRejectsOverlappingConfiguredRoots(t *testing.T) {
	baseDir := t.TempDir()
	text := strings.Replace(validSettingsText, "cache_root = cache", "cache_root = portable/cache", 1)
	settings, err := parseSettings(strings.NewReader(text), baseDir)
	if err != nil {
		t.Fatalf("prepare overlapping path test: expected valid individual settings, got %v", err)
	} else if _, err := derivePaths(settings); err == nil {
		t.Fatal("derive overlapping configured roots: expected rejection, got success")
	} else {
		return
	}
}
