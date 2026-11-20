package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runCLI(app App, args []string) error {
	if len(args) == 0 {
		if inputIsTerminal(app.Deps.Input) {
			return runInteractive(app)
		} else {
			printHelp(app.Deps.Output)
			return nil
		}
	} else if args[0] == "-test" {
		if len(args) != 1 {
			return fmt.Errorf("run built-in tests: expected only -test, got %q", strings.Join(args, " "))
		} else {
			return runSelfTest(app.Deps.Output)
		}
	} else {
		return runCommand(app, args[0], args[1:])
	}
}

func runCommand(app App, command string, args []string) error {
	switch command {
	case "help", "--help", "-h":
		if len(args) != 0 {
			return commandArityError(command, "no arguments", args)
		} else {
			printHelp(app.Deps.Output)
			return nil
		}
	case "install":
		return commandInstall(app, optionalProfile(app, args), false)
	case "update":
		return commandInstall(app, optionalProfile(app, args), true)
	case "list":
		return commandList(app, optionalProfile(app, args))
	case "plan":
		return commandPlan(app, optionalProfile(app, args))
	case "remove":
		return commandRemove(app, args)
	case "doctor":
		return commandDoctor(app, args)
	case "config":
		return commandConfig(app, args)
	case "env":
		return commandEnv(app, args)
	case "shell":
		return commandShell(app, optionalProfile(app, args))
	case "pack":
		return commandPack(app, args)
	case "restore":
		return commandRestore(app, args)
	default:
		return fmt.Errorf("choose Portprog command: expected install, update, list, plan, remove, doctor, config, env, shell, pack, restore, or help, got %q", command)
	}
}

func optionalProfile(app App, args []string) string {
	if len(args) == 0 {
		return app.Settings.DefaultProfile
	} else if len(args) == 1 {
		return args[0]
	} else {
		return ""
	}
}

func commandInstall(app App, profileName string, refresh bool) error {
	if profileName == "" {
		return fmt.Errorf("install profile: expected zero or one profile name, got too many arguments")
	} else {
		lock, err := resolveRequestedLock(app, profileName, refresh)
		if err != nil {
			return err
		} else {
			fmt.Fprintf(app.Deps.Output, "Installing profile %s: %s\n", lock.Profile, lock.ProfileDescription)
			return installLockedProfile(app, lock)
		}
	}
}

func commandList(app App, profileName string) error {
	if profileName == "" {
		return fmt.Errorf("list profile: expected zero or one profile name, got too many arguments")
	} else {
		lock, err := resolveRequestedLock(app, profileName, false)
		if err != nil {
			return err
		} else {
			fmt.Fprintf(app.Deps.Output, "Profile %s for %s/%s: %s\n", lock.Profile, lock.OS, lock.Arch, lock.ProfileDescription)
			for _, pkg := range lock.Packages {
				health := packageHealth(app, pkg)
				fmt.Fprintf(app.Deps.Output, "- %s %s — %s. %s\n", pkg.Name, pkg.Version, health.State, health.Detail)
			}
			return nil
		}
	}
}

func commandPlan(app App, profileName string) error {
	if profileName == "" {
		return fmt.Errorf("plan profile: expected zero or one profile name, got too many arguments")
	} else {
		lock, err := resolveCatalogLock(app, profileName)
		if err != nil {
			return err
		} else {
			fmt.Fprintf(app.Deps.Output, "Plan for profile %s on %s/%s: %s\n", lock.Profile, lock.OS, lock.Arch, lock.ProfileDescription)
			for index, pkg := range lock.Packages {
				size := assetSizeDescription(pkg.Asset)
				fmt.Fprintf(app.Deps.Output, "%d. %s %s — %s Download: %s. Destination: %s\n", index+1, pkg.Name, pkg.Version, pkg.Description, size, packageStorePath(app, pkg))
			}
			fmt.Fprintln(app.Deps.Output, "Plan only: no files were downloaded or installed.")
			return nil
		}
	}
}

func assetSizeDescription(asset PackageAsset) string {
	if asset.Size != 0 {
		return fmt.Sprintf("%d bytes", asset.Size)
	} else {
		return "publisher size unavailable"
	}
}

func commandRemove(app App, args []string) error {
	if len(args) != 1 {
		return commandArityError("remove", "one package name", args)
	} else {
		storeTrash, receiptTrash, err := removePackage(app, args[0])
		if err != nil {
			return err
		} else {
			fmt.Fprintf(app.Deps.Output, "Removed %s recoverably. Store: %s Receipt: %s\n", args[0], storeTrash, receiptTrash)
			return nil
		}
	}
}

func commandDoctor(app App, args []string) error {
	if len(args) != 0 {
		return commandArityError("doctor", "no arguments", args)
	} else {
		lock, hasLock, err := resolveDoctorLock(app)
		if err != nil {
			return err
		} else {
			fmt.Fprintf(app.Deps.Output, "Configuration: valid schema %d at %s.\n", app.Settings.SchemaVersion, filepath.Join(app.Paths.BaseDir, configFilename))
			fmt.Fprintf(app.Deps.Output, "Catalog and default profile: valid for %s/%s.\n", app.Platform.OS, app.Platform.Arch)
			if hasLock {
				fmt.Fprintf(app.Deps.Output, "Lockfile: valid profile %s with %d exact package assets.\n", lock.Profile, len(lock.Packages))
			} else {
				fmt.Fprintf(app.Deps.Output, "Lockfile: absent; checking the current %s catalog plan.\n", lock.Profile)
			}
			if cacheErr := doctorCacheState(app, lock); cacheErr != nil {
				return cacheErr
			} else {
				return doctorPackageState(app, lock)
			}
		}
	}
}

func resolveDoctorLock(app App) (LockFile, bool, error) {
	catalogLock, catalogErr := resolveCatalogLock(app, app.Settings.DefaultProfile)
	if catalogErr != nil {
		return LockFile{}, false, catalogErr
	} else if !regularFileExists(app.Paths.LockFile) {
		return catalogLock, false, nil
	} else {
		lock, lockErr := readLock(app.Paths.LockFile)
		if lockErr != nil {
			return LockFile{}, false, lockErr
		} else if compatibilityErr := validateCompatibleLock(app, lock); compatibilityErr != nil {
			return LockFile{}, false, compatibilityErr
		} else {
			return lock, true, nil
		}
	}
}

func doctorCacheState(app App, lock LockFile) error {
	damaged := make([]string, 0)
	for _, pkg := range lock.Packages {
		cachePath := filepath.Join(app.Paths.CacheObjects, pkg.Asset.SHA256)
		valid, err := verifyCachedFile(cachePath, pkg.Asset)
		if err != nil {
			return fmt.Errorf("doctor cache for package %q: %w", pkg.Name, err)
		} else if valid {
			fmt.Fprintf(app.Deps.Output, "Cache %s: verified — size and SHA-256 match the lockfile.\n", pkg.Name)
		} else if regularFileExists(cachePath) {
			fmt.Fprintf(app.Deps.Output, "Cache %s: damaged — size or SHA-256 differs from the lockfile.\n", pkg.Name)
			damaged = append(damaged, pkg.Name)
		} else {
			fmt.Fprintf(app.Deps.Output, "Cache %s: absent — no verified archive is cached; an online install can download it.\n", pkg.Name)
		}
	}
	if len(damaged) != 0 {
		return fmt.Errorf("check verified package cache: expected no damaged objects, got %s", strings.Join(damaged, ", "))
	} else {
		return nil
	}
}

func doctorPackageState(app App, lock LockFile) error {
	problems := make([]string, 0)
	for _, pkg := range lock.Packages {
		health := packageHealth(app, pkg)
		fmt.Fprintf(app.Deps.Output, "Package %s %s: %s — %s\n", pkg.Name, pkg.Version, health.State, health.Detail)
		if health.State == "damaged" || health.State == "outdated" {
			problems = append(problems, pkg.Name+":"+health.State)
		} else {
			continue
		}
	}
	if len(problems) != 0 {
		return fmt.Errorf("check installed package state: expected no damaged or outdated packages, got %s", strings.Join(problems, ", "))
	} else {
		fmt.Fprintln(app.Deps.Output, "Doctor finished: configuration, catalog, lock, cache, receipts, and installed tools are consistent; absent items can be installed normally.")
		return nil
	}
}

func commandConfig(app App, args []string) error {
	if len(args) != 0 {
		return commandArityError("config", "no arguments", args)
	} else {
		fmt.Fprintf(app.Deps.Output, "Configuration file: %s\n", filepath.Join(app.Paths.BaseDir, configFilename))
		for _, setting := range settingExplanations(app.Settings) {
			fmt.Fprintf(app.Deps.Output, "- %s = %s\n  %s\n", setting.Name, setting.Value, setting.Explanation)
		}
		return nil
	}
}

func commandEnv(app App, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return commandArityError("env", "a shell name and optional profile", args)
	} else {
		profileName := optionalSecondArgument(args, app.Settings.DefaultProfile)
		lock, err := resolveRequestedLock(app, profileName, false)
		if err != nil {
			return err
		} else {
			paths, pathErr := profileBinPaths(app, lock)
			if pathErr != nil {
				return pathErr
			} else {
				activation, activationErr := shellActivation(strings.ToLower(args[0]), paths)
				if activationErr != nil {
					return activationErr
				} else {
					fmt.Fprint(app.Deps.Output, activation)
					return nil
				}
			}
		}
	}
}

func commandShell(app App, profileName string) error {
	if profileName == "" {
		return fmt.Errorf("open profile shell: expected zero or one profile name, got too many arguments")
	} else {
		lock, err := resolveRequestedLock(app, profileName, false)
		if err != nil {
			return err
		} else {
			return openProfileShell(app, lock)
		}
	}
}

func commandPack(app App, args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return commandArityError("pack", "a relative .zip filename and optional profile", args)
	} else {
		profileName := optionalSecondArgument(args, app.Settings.DefaultProfile)
		lock, err := resolveRequestedLock(app, profileName, false)
		if err != nil {
			return err
		} else {
			filename, packErr := createOfflinePack(app, args[0], lock)
			if packErr != nil {
				return packErr
			} else {
				fmt.Fprintf(app.Deps.Output, "Created verified offline pack: %s\n", filename)
				return nil
			}
		}
	}
}

func commandRestore(app App, args []string) error {
	if len(args) != 1 {
		return commandArityError("restore", "one offline .zip filename", args)
	} else {
		if err := restoreOfflinePack(app, args[0]); err != nil {
			return err
		} else {
			fmt.Fprintf(app.Deps.Output, "Restored and installed verified offline pack: %s\n", args[0])
			return nil
		}
	}
}

func optionalSecondArgument(args []string, fallback string) string {
	if len(args) == 2 {
		return args[1]
	} else {
		return fallback
	}
}

func commandArityError(command string, expected string, args []string) error {
	return fmt.Errorf("run %s: expected %s, got %d arguments: %q", command, expected, len(args), strings.Join(args, " "))
}

func printHelp(output io.Writer) {
	fmt.Fprintln(output, "Portprog installs a verified programming toolkit entirely inside this directory.")
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  install [profile]       Install the locked profile, or resolve it when no compatible lock exists.")
	fmt.Fprintln(output, "  update [profile]        Resolve the current catalog into a new lock, then install it.")
	fmt.Fprintln(output, "  list [profile]          Explain whether each selected package is installed, missing, outdated, or damaged.")
	fmt.Fprintln(output, "  plan [profile]          Show exact current catalog assets without downloading or changing files.")
	fmt.Fprintln(output, "  remove PACKAGE          Move one installed package and its receipt to recoverable local trash.")
	fmt.Fprintln(output, "  doctor                 Validate configuration, catalog, lock, cache, receipts, and installed files without repair.")
	fmt.Fprintln(output, "  config                 Show every setting, its effective value, and how to use it.")
	fmt.Fprintln(output, "  env SHELL [profile]     Print activation for bash, zsh, fish, powershell, or cmd.")
	fmt.Fprintln(output, "  shell [profile]         Open the user shell with verified profile tools on PATH.")
	fmt.Fprintln(output, "  pack FILE [profile]     Create a verified current-platform offline ZIP inside this directory.")
	fmt.Fprintln(output, "  restore FILE            Verify, import, and install an offline pack without network access.")
	fmt.Fprintln(output, "  -test                   Run fast offline built-in tests.")
	fmt.Fprintln(output, "  help                    Show this explanation.")
}

func runInteractive(app App) error {
	scanner := bufio.NewScanner(app.Deps.Input)
	for {
		printInteractiveMenu(app)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read launcher selection: expected a menu number, got error: %w", err)
			} else {
				fmt.Fprintln(app.Deps.Output, "Input ended; Portprog exited without another action.")
				return nil
			}
		} else {
			selection := strings.TrimSpace(scanner.Text())
			if selection == "6" {
				fmt.Fprintln(app.Deps.Output, "Portprog exited without another action.")
				return nil
			} else if err := runInteractiveSelection(app, selection); err != nil {
				fmt.Fprintf(app.Deps.Error, "Action %s failed: %v\n", selection, err)
			} else {
				fmt.Fprintln(app.Deps.Output, "Action finished. Choose another action or 6 to exit.")
			}
		}
	}
}

func printInteractiveMenu(app App) {
	fmt.Fprintf(app.Deps.Output, "\nPortprog portable developer kit — default profile: %s\n", app.Settings.DefaultProfile)
	fmt.Fprintln(app.Deps.Output, "1. Install profile — download, verify, and install every missing default-profile tool locally.")
	fmt.Fprintln(app.Deps.Output, "2. Open prepared shell — use installed default-profile tools without editing shell startup files.")
	fmt.Fprintln(app.Deps.Output, "3. Show status — explain which default-profile tools are installed, missing, outdated, or damaged.")
	fmt.Fprintln(app.Deps.Output, "4. Run doctor — validate configuration, package definitions, cache, receipts, and installed files.")
	fmt.Fprintln(app.Deps.Output, "5. Explain configuration — show every editable setting, current value, and purpose.")
	fmt.Fprintln(app.Deps.Output, "6. Exit — leave all files unchanged.")
	fmt.Fprint(app.Deps.Output, "Choose 1-6: ")
}

func runInteractiveSelection(app App, selection string) error {
	switch selection {
	case "1":
		return commandInstall(app, app.Settings.DefaultProfile, false)
	case "2":
		return commandShell(app, app.Settings.DefaultProfile)
	case "3":
		return commandList(app, app.Settings.DefaultProfile)
	case "4":
		return commandDoctor(app, nil)
	case "5":
		return commandConfig(app, nil)
	default:
		return fmt.Errorf("choose launcher action: expected a number from 1 through 6, got %q", selection)
	}
}

func inputIsTerminal(input io.Reader) bool {
	file, isFile := input.(*os.File)
	if !isFile {
		return false
	} else {
		info, err := file.Stat()
		if err != nil {
			return false
		} else {
			return info.Mode()&os.ModeCharDevice != 0
		}
	}
}
