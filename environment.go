package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func shellActivation(shell string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("create %s activation: expected at least one installed binary directory, got none", shell)
	} else {
		switch shell {
		case "bash", "zsh":
			quoted := make([]string, 0, len(paths))
			for _, value := range paths {
				quoted = append(quoted, quotePOSIX(value))
			}
			return "export PATH=" + strings.Join(quoted, ":") + ":\"$PATH\"\n", nil
		case "fish":
			quoted := make([]string, 0, len(paths))
			for _, value := range paths {
				quoted = append(quoted, quoteFish(value))
			}
			return "set -gx PATH " + strings.Join(quoted, " ") + " $PATH\n", nil
		case "powershell":
			quoted := strings.ReplaceAll(strings.Join(paths, ";")+";", "'", "''")
			return "$env:Path = '" + quoted + "' + $env:Path\n", nil
		case "cmd":
			for _, value := range paths {
				if strings.ContainsAny(value, "&|<>^%!\"\r\n;") {
					return "", fmt.Errorf("create cmd activation: expected paths without cmd metacharacters or semicolons, got %q", value)
				} else {
					continue
				}
			}
			return "set \"PATH=" + strings.Join(paths, ";") + ";%PATH%\"\n", nil
		default:
			return "", fmt.Errorf("create shell activation: expected bash, zsh, fish, powershell, or cmd, got %q", shell)
		}
	}
}

func quotePOSIX(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func quoteFish(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "'", "\\'")
	return "'" + escaped + "'"
}

func profileBinPaths(app App, lock LockFile) ([]string, error) {
	paths := make([]string, 0)
	seen := make(map[string]bool)
	for _, pkg := range lock.Packages {
		health := packageHealth(app, pkg)
		if health.State != "installed" {
			return nil, fmt.Errorf("activate package %q: expected installed state, got %s: %s", pkg.Name, health.State, health.Detail)
		} else {
			receipt, err := readReceipt(app, pkg.Name)
			if err != nil {
				return nil, err
			} else if addErr := appendReceiptBinPaths(app, receipt, &paths, seen); addErr != nil {
				return nil, addErr
			} else {
				continue
			}
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("activate profile %q: expected installed binary directories, got none", lock.Profile)
	} else {
		return paths, nil
	}
}

func appendReceiptBinPaths(app App, receipt Receipt, paths *[]string, seen map[string]bool) error {
	storePath, err := containedPath(app.Paths.BaseDir, receipt.StorePath)
	if err != nil {
		return err
	} else {
		for _, binDir := range receipt.BinDirs {
			var candidate string
			if binDir == "." {
				candidate = storePath
			} else {
				resolved, pathErr := containedPortablePath(storePath, binDir)
				if pathErr != nil {
					return pathErr
				} else {
					candidate = resolved
				}
			}
			info, statErr := os.Lstat(candidate)
			if statErr != nil {
				return fmt.Errorf("activate binary directory %q for package %q: expected directory, got error: %w", candidate, receipt.Name, statErr)
			} else if !info.IsDir() {
				return fmt.Errorf("activate binary directory %q for package %q: expected directory, got mode %v", candidate, receipt.Name, info.Mode())
			} else if seen[candidate] {
				continue
			} else {
				seen[candidate] = true
				*paths = append(*paths, candidate)
			}
		}
		return nil
	}
}

func openProfileShell(app App, lock LockFile) error {
	paths, err := profileBinPaths(app, lock)
	if err != nil {
		return err
	} else {
		shell := configuredUserShell()
		command := exec.Command(shell)
		command.Env = environmentWithPath(paths)
		command.Stdin = app.Deps.Input
		command.Stdout = app.Deps.Output
		command.Stderr = app.Deps.Error
		fmt.Fprintf(app.Deps.Output, "Opening %s with %d Portprog binary directories. Exit that shell to return.\n", shell, len(paths))
		if runErr := command.Run(); runErr != nil {
			return fmt.Errorf("run configured shell %q for profile %q: expected normal exit, got error: %w", shell, lock.Profile, runErr)
		} else {
			return nil
		}
	}
}

func configuredUserShell() string {
	if runtime.GOOS == "windows" {
		value := strings.TrimSpace(os.Getenv("COMSPEC"))
		if value == "" {
			return "cmd.exe"
		} else {
			return value
		}
	} else {
		value := strings.TrimSpace(os.Getenv("SHELL"))
		if value == "" || !filepath.IsAbs(value) || !regularFileExists(value) {
			return "/bin/sh"
		} else {
			return value
		}
	}
}

func environmentWithPath(paths []string) []string {
	separator := string(os.PathListSeparator)
	prefix := strings.Join(paths, separator)
	current := os.Getenv("PATH")
	combined := prefix
	if current == "" {
		combined = prefix
	} else {
		combined = prefix + separator + current
	}
	environment := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		key, _, found := strings.Cut(item, "=")
		if found && strings.EqualFold(key, "PATH") {
			continue
		} else {
			environment = append(environment, item)
		}
	}
	return append(environment, "PATH="+combined)
}
