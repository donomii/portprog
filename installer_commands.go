package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type boundedWriter struct {
	destination io.Writer
	remaining   int64
}

func (writer *boundedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > writer.remaining {
		return 0, fmt.Errorf("command output exceeds remaining limit %d bytes", writer.remaining)
	}
	written, err := writer.destination.Write(data)
	writer.remaining -= int64(written)
	return written, err
}

func installerEnvironment(extra []string) []string {
	replacements := append([]string{"GIT_TERMINAL_PROMPT=0"}, extra...)
	keys := make(map[string]bool)
	for _, setting := range replacements {
		key := strings.SplitN(setting, "=", 2)[0]
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		keys[key] = true
	}
	result := make([]string, 0, len(os.Environ())+len(replacements))
	for _, setting := range os.Environ() {
		key := strings.SplitN(setting, "=", 2)[0]
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		if !keys[key] {
			result = append(result, setting)
		}
	}
	return append(result, replacements...)
}

func installerCommand(b Config, directory string, environment []string, executable string, arguments ...string) (*exec.Cmd, error) {
	receipts, err := installedReceipts(b)
	if err != nil {
		return nil, fmt.Errorf("read installed dependencies before running %q: %w", executable, err)
	}
	packages := make([]LockedPackage, 0, len(receipts))
	for _, receipt := range receipts {
		packages = append(packages, receipt.Package)
	}
	relativePaths, err := environmentPaths(packages, runtime.GOOS == "windows")
	if err != nil {
		return nil, err
	}
	root := b.RootDir
	if root == "" {
		root = filepath.Dir(b.InstallDir)
	}
	paths := make([]string, 0, len(relativePaths)+1)
	for _, relative := range relativePaths {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(relative)))
	}
	paths = append(paths, filepath.SplitList(os.Getenv("PATH"))...)
	selected := executable
	if !filepath.IsAbs(executable) && !strings.ContainsAny(executable, "/\\") {
		selected = ""
		for _, directory := range paths {
			if directory == "" {
				continue
			}
			candidate, err := exec.LookPath(filepath.Join(directory, executable))
			if err == nil {
				selected = candidate
				break
			}
		}
		if selected == "" {
			return nil, fmt.Errorf("executable %q was not found in installed package binaries or PATH %q: %w", executable, strings.Join(paths, string(os.PathListSeparator)), exec.ErrNotFound)
		}
	} else if !filepath.IsAbs(executable) {
		selected = filepath.Join(directory, executable)
	}
	settings := append([]string{"PATH=" + strings.Join(paths, string(os.PathListSeparator))}, environment...)
	command := exec.Command(selected, arguments...)
	command.Dir = directory
	command.Env = installerEnvironment(settings)
	return command, nil
}

func commandLog(b Config) (*os.File, error) {
	if err := os.MkdirAll(b.TempDir, 0755); err != nil {
		return nil, fmt.Errorf("create command log directory %q: %w", b.TempDir, err)
	}
	return os.CreateTemp(b.TempDir, "command-*.log")
}

func runInstallerCommand(b Config, directory, executable string, arguments ...string) error {
	return runInstallerCommandEnv(b, directory, nil, executable, arguments...)
}

func runInstallerCommandEnv(b Config, directory string, environment []string, executable string, arguments ...string) error {
	command, err := installerCommand(b, directory, environment, executable, arguments...)
	if err != nil {
		return err
	}
	logFile, err := commandLog(b)
	if err != nil {
		return err
	}
	var output io.Writer = logFile
	if b.Output != nil {
		output = io.MultiWriter(logFile, b.Output)
	}
	command.Stdout, command.Stderr = output, output
	runErr := command.Run()
	closeErr := logFile.Close()
	if runErr != nil {
		return fmt.Errorf("command %q %q in %q failed: %w; complete output: %s", executable, arguments, directory, runErr, logFile.Name())
	}
	if closeErr != nil {
		return fmt.Errorf("close output log %q for %q: %w", logFile.Name(), executable, closeErr)
	}
	return nil
}

func streamInstallerCommand(b Config, directory string, output io.Writer, executable string, arguments ...string) error {
	command, err := installerCommand(b, directory, nil, executable, arguments...)
	if err != nil {
		return err
	}
	logFile, err := commandLog(b)
	if err != nil {
		return err
	}
	command.Stdout, command.Stderr = output, logFile
	runErr := command.Run()
	closeErr := logFile.Close()
	if runErr != nil {
		return fmt.Errorf("command %q %q in %q failed: %w; complete errors: %s", executable, arguments, directory, runErr, logFile.Name())
	}
	return closeErr
}

func captureInstallerCommand(b Config, directory, executable string, arguments ...string) ([]byte, error) {
	var output bytes.Buffer
	writer := &boundedWriter{destination: &output, remaining: 16 << 20}
	err := streamInstallerCommand(b, directory, writer, executable, arguments...)
	return output.Bytes(), err
}

func configureScript(source string) (string, error) {
	for _, name := range []string{"configure", "bootstrap", "autogen.sh"} {
		filename := filepath.Join(source, name)
		info, err := os.Stat(filename)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("configuration script %q must be a regular file", filename)
		}
		if err := os.Chmod(filename, info.Mode().Perm()|0100); err != nil {
			return "", err
		}
		return filename, nil
	}
	return "", fmt.Errorf("source directory %q requires configure, bootstrap, or autogen.sh", source)
}

func buildSource(b Config, p Package, source, stage string) (string, error) {
	if b.Offline {
		return "", fmt.Errorf("package %q uses source build plan %q; offline installation requires an installed-image bundle", p.Name, p.Plan)
	}
	prefix := filepath.Join(stage, "prefix")
	if err := os.Mkdir(prefix, 0755); err != nil {
		return "", err
	}
	finalPrefix := filepath.Join(b.InstallDir, p.Name)
	stagedRoot := filepath.Join(stage, "staged-root")
	stagedPrefix := filepath.Join(stagedRoot, strings.TrimLeft(strings.TrimPrefix(finalPrefix, filepath.VolumeName(finalPrefix)), string(filepath.Separator)))
	switch p.Plan {
	case "standardConfigure":
		configure, err := configureScript(source)
		if err != nil {
			return "", err
		}
		executable := configure
		arguments := []string{"--prefix=" + finalPrefix}
		installArguments := []string{"install", "DESTDIR=" + stagedRoot}
		if runtime.GOOS == "windows" {
			executable = "sh"
			arguments = []string{filepath.ToSlash(configure), "--prefix=" + filepath.ToSlash(prefix)}
			installArguments = []string{"install", "DESTDIR="}
			stagedPrefix = prefix
		}
		if err := runInstallerCommand(b, source, executable, arguments...); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "make"); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "make", installArguments...); err != nil {
			return "", err
		}
		prefix = stagedPrefix
	case "gitAndMake":
		makePrefix := filepath.ToSlash(prefix)
		if err := runInstallerCommand(b, source, "make", "PREFIX="+makePrefix, "prefix="+makePrefix, "DESTDIR="); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "make", "install", "PREFIX="+makePrefix, "prefix="+makePrefix, "DESTDIR="); err != nil {
			return "", err
		}
	case "goGetAndMake":
		if _, err := os.Stat(filepath.Join(source, "go.mod")); err != nil {
			return "", fmt.Errorf("Go package %q requires go.mod in pinned source %q: %w", p.Name, source, err)
		}
		goPath := b.GoPath
		if goPath == "" {
			goPath = filepath.Join(b.RootDir, "langlibs", "gopath")
		}
		bin := filepath.Join(prefix, "bin")
		if err := os.Mkdir(bin, 0755); err != nil {
			return "", err
		}
		environment := []string{"GOBIN=" + bin, "GOPATH=" + goPath, "GOMODCACHE=" + filepath.Join(goPath, "pkg", "mod"), "GOCACHE=" + filepath.Join(goPath, "build-cache"), "GOENV=off", "GOWORK=off"}
		arguments := []string{"install"}
		if runtime.GOOS == "darwin" {
			arguments = append(arguments, "-ldflags=-B=gobuildid")
		}
		arguments = append(arguments, "./...")
		if err := runInstallerCommandEnv(b, source, environment, "go", arguments...); err != nil {
			return "", err
		}
	case "cmake":
		build := filepath.Join(stage, "build")
		configuredPrefix := finalPrefix
		installArguments := []string{"--install", build}
		installEnvironment := []string{"DESTDIR=" + stagedRoot}
		if runtime.GOOS == "windows" {
			configuredPrefix = prefix
			installArguments = append(installArguments, "--prefix", prefix)
			installEnvironment = []string{"DESTDIR="}
			stagedPrefix = prefix
		}
		if err := runInstallerCommand(b, source, "cmake", "-S", source, "-B", build, "-DCMAKE_INSTALL_PREFIX="+configuredPrefix); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "cmake", "--build", build); err != nil {
			return "", err
		}
		if err := runInstallerCommandEnv(b, source, installEnvironment, "cmake", installArguments...); err != nil {
			return "", err
		}
		prefix = stagedPrefix
	default:
		return "", fmt.Errorf("unknown source build plan %q for package %q", p.Plan, p.Name)
	}
	return prefix, nil
}
