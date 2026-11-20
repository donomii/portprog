package main

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"
)

func main() {
	os.Exit(safeMain())
}

func safeMain() (exitCode int) {
	exitCode = 0
	defer func() {
		recovered := recover()
		if recovered != nil {
			fmt.Fprintf(os.Stderr, "Portprog stopped after an unexpected failure: %v\n", recovered)
			exitCode = 1
		} else {
			return
		}
	}()
	app, err := createApp()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Portprog could not start: %v\n", err)
		return 1
	} else if err := runCLI(app, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Portprog could not complete the requested task: %v\n", err)
		return 1
	} else {
		return 0
	}
}

func createApp() (App, error) {
	baseDir, baseErr := locateBaseDir()
	if baseErr != nil {
		return App{}, baseErr
	} else {
		settings, settingsErr := loadSettings(baseDir)
		if settingsErr != nil {
			return App{}, settingsErr
		} else {
			paths, pathsErr := derivePaths(settings)
			platform := Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
			if pathsErr != nil {
				return App{}, pathsErr
			} else if !supportedOS(platform.OS) || !supportedArch(platform.Arch) {
				return App{}, fmt.Errorf("detect host platform: expected darwin, linux, or windows on amd64 or arm64, got %s/%s", platform.OS, platform.Arch)
			} else {
				return App{Settings: settings, Paths: paths, Platform: platform, Deps: createDefaultDeps(settings)}, nil
			}
		}
	}
}

func createDefaultDeps(settings Settings) Dependencies {
	return Dependencies{
		HTTPClient: &http.Client{
			Timeout:       time.Duration(settings.DownloadTimeoutSeconds) * time.Second,
			CheckRedirect: redirectPolicy(false),
		},
		Now: time.Now, Input: os.Stdin, Output: os.Stdout, Error: os.Stderr,
	}
}
