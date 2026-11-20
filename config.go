package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

var requiredSettingNames = []string{
	"schema_version",
	"default_profile",
	"install_root",
	"cache_root",
	"state_root",
	"trash_root",
	"catalog_dir",
	"profiles_dir",
	"lock_file",
	"offline",
	"max_download_mb",
	"max_extract_mb",
	"download_timeout_seconds",
}

const (
	maximumConfiguredMebibytes    = int64((1<<63 - 1) / (1024 * 1024))
	maximumDownloadTimeoutSeconds = 86400
)

func locateBaseDir() (string, error) {
	executable, executableErr := os.Executable()
	if executableErr != nil {
		return "", fmt.Errorf("locate Portprog configuration: expected the executable path, got error: %w", executableErr)
	} else {
		executableDir := filepath.Dir(executable)
		executableConfig := filepath.Join(executableDir, configFilename)
		if regularFileExists(executableConfig) {
			return filepath.Clean(executableDir), nil
		} else {
			cwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				return "", fmt.Errorf("locate Portprog configuration: expected %q or the current directory, got error: %w", executableConfig, cwdErr)
			} else {
				cwdConfig := filepath.Join(cwd, configFilename)
				if regularFileExists(cwdConfig) {
					return filepath.Clean(cwd), nil
				} else {
					return "", fmt.Errorf("locate Portprog configuration: expected %q or %q, but neither is a regular file", executableConfig, cwdConfig)
				}
			}
		}
	}
}

func loadSettings(baseDir string) (Settings, error) {
	path := filepath.Join(baseDir, configFilename)
	file, err := os.Open(path)
	if err != nil {
		return Settings{}, fmt.Errorf("open Portprog configuration %q: expected a readable file, got error: %w", path, err)
	} else {
		settings, parseErr := parseSettings(file, baseDir)
		closeErr := file.Close()
		if parseErr != nil {
			return Settings{}, parseErr
		} else if closeErr != nil {
			return Settings{}, fmt.Errorf("close Portprog configuration %q: expected success, got error: %w", path, closeErr)
		} else {
			return settings, nil
		}
	}
}

func parseSettings(reader io.Reader, baseDir string) (Settings, error) {
	values := make(map[string]string, len(requiredSettingNames))
	scanner := bufio.NewScanner(reader)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		rawLine := scanner.Text()
		if !utf8.ValidString(rawLine) {
			return Settings{}, fmt.Errorf("parse Portprog configuration line %d: expected valid UTF-8 text", lineNumber)
		} else {
			line := strings.TrimSpace(strings.TrimPrefix(rawLine, "\ufeff"))
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			} else {
				key, value, splitErr := splitSetting(line, lineNumber)
				if splitErr != nil {
					return Settings{}, splitErr
				} else {
					_, duplicate := values[key]
					if duplicate {
						return Settings{}, fmt.Errorf("parse Portprog configuration line %d: expected key %q once, got a duplicate", lineNumber, key)
					} else {
						values[key] = value
					}
				}
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return Settings{}, fmt.Errorf("read Portprog configuration: expected complete UTF-8 text, got error: %w", scanErr)
	} else {
		return settingsFromValues(values, filepath.Clean(baseDir))
	}
}

func splitSetting(line string, lineNumber int) (string, string, error) {
	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", fmt.Errorf("parse Portprog configuration line %d: expected key = value, got %q", lineNumber, line)
	} else {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return "", "", fmt.Errorf("parse Portprog configuration line %d: expected non-empty key and value, got %q", lineNumber, line)
		} else if !knownSetting(key) {
			return "", "", fmt.Errorf("parse Portprog configuration line %d: expected a documented setting, got unknown key %q", lineNumber, key)
		} else {
			return key, value, nil
		}
	}
}

func knownSetting(name string) bool {
	for _, candidate := range requiredSettingNames {
		if candidate == name {
			return true
		} else {
			continue
		}
	}
	return false
}

func settingsFromValues(values map[string]string, baseDir string) (Settings, error) {
	for _, name := range requiredSettingNames {
		if strings.TrimSpace(values[name]) == "" {
			return Settings{}, fmt.Errorf("validate Portprog configuration: expected required setting %q, got no value", name)
		} else {
			continue
		}
	}

	schemaVersion, schemaErr := parsePositiveInteger(values["schema_version"], "schema_version")
	maxDownloadMB, maximumErr := parsePositiveInteger64(values["max_download_mb"], "max_download_mb")
	maxExtractMB, extractErr := parsePositiveInteger64(values["max_extract_mb"], "max_extract_mb")
	timeoutSeconds, timeoutErr := parsePositiveInteger(values["download_timeout_seconds"], "download_timeout_seconds")
	offline, offlineErr := strconv.ParseBool(values["offline"])
	if schemaErr != nil {
		return Settings{}, schemaErr
	} else if maximumErr != nil {
		return Settings{}, maximumErr
	} else if extractErr != nil {
		return Settings{}, extractErr
	} else if timeoutErr != nil {
		return Settings{}, timeoutErr
	} else if offlineErr != nil {
		return Settings{}, fmt.Errorf("parse setting offline: expected true or false, got %q", values["offline"])
	} else if schemaVersion != currentSchemaVersion {
		return Settings{}, fmt.Errorf("validate setting schema_version: expected %d, got %d", currentSchemaVersion, schemaVersion)
	} else {
		settings := Settings{
			SchemaVersion: schemaVersion, DefaultProfile: values["default_profile"],
			InstallRoot: values["install_root"], CacheRoot: values["cache_root"],
			StateRoot: values["state_root"], TrashRoot: values["trash_root"],
			CatalogDir: values["catalog_dir"], ProfilesDir: values["profiles_dir"],
			LockFile: values["lock_file"], Offline: offline,
			MaxDownloadMB: maxDownloadMB, MaxExtractMB: maxExtractMB,
			DownloadTimeoutSeconds: timeoutSeconds,
			BaseDir:                baseDir,
		}
		return validateSettings(settings)
	}
}

func validateSettings(settings Settings) (Settings, error) {
	paths := []struct {
		name  string
		value string
	}{
		{name: "install_root", value: settings.InstallRoot},
		{name: "cache_root", value: settings.CacheRoot},
		{name: "state_root", value: settings.StateRoot},
		{name: "trash_root", value: settings.TrashRoot},
		{name: "catalog_dir", value: settings.CatalogDir},
		{name: "profiles_dir", value: settings.ProfilesDir},
		{name: "lock_file", value: settings.LockFile},
	}
	for _, path := range paths {
		if err := validateRelativePath(path.name, path.value); err != nil {
			return Settings{}, err
		} else {
			continue
		}
	}
	if err := validateIdentifier("default_profile", settings.DefaultProfile); err != nil {
		return Settings{}, err
	} else if settings.MaxDownloadMB > maximumConfiguredMebibytes {
		return Settings{}, fmt.Errorf("validate setting max_download_mb: expected at most %d to prevent byte-limit overflow, got %d", maximumConfiguredMebibytes, settings.MaxDownloadMB)
	} else if settings.MaxExtractMB > maximumConfiguredMebibytes {
		return Settings{}, fmt.Errorf("validate setting max_extract_mb: expected at most %d to prevent byte-limit overflow, got %d", maximumConfiguredMebibytes, settings.MaxExtractMB)
	} else if settings.DownloadTimeoutSeconds > maximumDownloadTimeoutSeconds {
		return Settings{}, fmt.Errorf("validate setting download_timeout_seconds: expected at most %d seconds, got %d", maximumDownloadTimeoutSeconds, settings.DownloadTimeoutSeconds)
	} else {
		return settings, nil
	}
}

func parsePositiveInteger(value string, name string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("parse setting %s: expected a positive whole number, got %q", name, value)
	} else {
		return parsed, nil
	}
}

func parsePositiveInteger64(value string, name string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("parse setting %s: expected a positive whole number, got %q", name, value)
	} else {
		return parsed, nil
	}
}

func settingExplanations(settings Settings) []SettingExplanation {
	return []SettingExplanation{
		{Name: "schema_version", Value: strconv.Itoa(settings.SchemaVersion), Explanation: "Selects the configuration format. Portprog v1 requires 1."},
		{Name: "default_profile", Value: settings.DefaultProfile, Explanation: "Chooses the profile used when install, list, plan, env, or shell omits a profile name."},
		{Name: "install_root", Value: settings.InstallRoot, Explanation: "Stores versioned installed tools inside this base-directory-relative folder."},
		{Name: "cache_root", Value: settings.CacheRoot, Explanation: "Stores verified downloads by SHA-256 so later installs can reuse them."},
		{Name: "state_root", Value: settings.StateRoot, Explanation: "Stores receipts describing exactly what Portprog installed."},
		{Name: "trash_root", Value: settings.TrashRoot, Explanation: "Receives replaced and removed packages so removal remains recoverable."},
		{Name: "catalog_dir", Value: settings.CatalogDir, Explanation: "Contains strict JSON package manifests."},
		{Name: "profiles_dir", Value: settings.ProfilesDir, Explanation: "Contains strict JSON lists of packages installed together."},
		{Name: "lock_file", Value: settings.LockFile, Explanation: "Records exact package versions, assets, and checksums for repeatable installs."},
		{Name: "offline", Value: strconv.FormatBool(settings.Offline), Explanation: "When true, forbids network access and requires every archive in the verified cache."},
		{Name: "max_download_mb", Value: strconv.FormatInt(settings.MaxDownloadMB, 10), Explanation: "Rejects any single download larger than this many mebibytes."},
		{Name: "max_extract_mb", Value: strconv.FormatInt(settings.MaxExtractMB, 10), Explanation: "Stops archive extraction after this many uncompressed mebibytes to limit archive expansion."},
		{Name: "download_timeout_seconds", Value: strconv.Itoa(settings.DownloadTimeoutSeconds), Explanation: "Limits the complete HTTP request, including reading its body."},
	}
}
