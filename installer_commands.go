package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		keys[strings.SplitN(setting, "=", 2)[0]] = true
	}
	result := make([]string, 0, len(os.Environ())+len(replacements))
	for _, setting := range os.Environ() {
		if !keys[strings.SplitN(setting, "=", 2)[0]] {
			result = append(result, setting)
		}
	}
	return append(result, replacements...)
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
	logFile, err := commandLog(b)
	if err != nil {
		return err
	}
	var output io.Writer = logFile
	if b.Output != nil {
		output = io.MultiWriter(logFile, b.Output)
	}
	command := exec.Command(executable, arguments...)
	command.Dir = directory
	command.Env = installerEnvironment(environment)
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
	logFile, err := commandLog(b)
	if err != nil {
		return err
	}
	command := exec.Command(executable, arguments...)
	command.Dir = directory
	command.Env = installerEnvironment(nil)
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
		configure := filepath.Join(source, "configure")
		if _, err := os.Stat(configure); os.IsNotExist(err) {
			configure = filepath.Join(source, "bootstrap")
		} else if err != nil {
			return "", err
		}
		info, err := os.Stat(configure)
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("package %q requires configure or bootstrap in %q: %v", p.Name, source, err)
		}
		if err := os.Chmod(configure, info.Mode().Perm()|0100); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, configure, "--prefix="+finalPrefix); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "make"); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "make", "install", "DESTDIR="+stagedRoot); err != nil {
			return "", err
		}
		prefix = stagedPrefix
	case "gitAndMake":
		if err := runInstallerCommand(b, source, "make", "PREFIX="+prefix, "prefix="+prefix, "DESTDIR="); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "make", "install", "PREFIX="+prefix, "prefix="+prefix, "DESTDIR="); err != nil {
			return "", err
		}
	case "goGetAndMake":
		if _, err := os.Stat(filepath.Join(source, "go.mod")); err != nil {
			return "", fmt.Errorf("Go package %q requires go.mod in pinned source %q: %w", p.Name, source, err)
		}
		root := b.RootDir
		if root == "" {
			root = filepath.Dir(b.InstallDir)
		}
		bin := filepath.Join(prefix, "bin")
		if err := os.Mkdir(bin, 0755); err != nil {
			return "", err
		}
		environment := []string{"GOBIN=" + bin, "GOPATH=" + filepath.Join(root, "go"), "GOMODCACHE=" + filepath.Join(root, "go", "pkg", "mod"), "GOCACHE=" + filepath.Join(root, "go", "build-cache"), "GOENV=off", "GOWORK=off"}
		if err := runInstallerCommandEnv(b, source, environment, "go", "install", "./..."); err != nil {
			return "", err
		}
	case "cmake":
		build := filepath.Join(stage, "build")
		if err := runInstallerCommand(b, source, "cmake", "-S", source, "-B", build, "-DCMAKE_INSTALL_PREFIX="+finalPrefix); err != nil {
			return "", err
		}
		if err := runInstallerCommand(b, source, "cmake", "--build", build); err != nil {
			return "", err
		}
		if err := runInstallerCommandEnv(b, source, []string{"DESTDIR=" + stagedRoot}, "cmake", "--install", build); err != nil {
			return "", err
		}
		prefix = stagedPrefix
	default:
		return "", fmt.Errorf("unknown source build plan %q for package %q", p.Plan, p.Name)
	}
	return prefix, nil
}
