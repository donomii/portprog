package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type commandOptions struct {
	root, profile, file, bundle, remove string
	validate, lock, offline, noInstall  bool
	noGcc, noGo, noGit, devel           bool
	repair, list, selfTest              bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		logFailure(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output, diagnostics io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the Portprog executable: %w", err)
	}
	options := commandOptions{}
	flags := flag.NewFlagSet("portprog", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&options.root, "root", filepath.Dir(executable), "Portable installation directory containing portprog.json, profiles, cache and installs")
	flags.StringVar(&options.profile, "profile", "", "Select profiles/NAME.json; defaults to default_profile in portprog.json")
	flags.StringVar(&options.file, "file", "", "Install one recipe; relative paths are resolved from the caller's working directory")
	flags.BoolVar(&options.validate, "validate", false, "Validate the complete recipe catalog and every profile locally, then exit")
	flags.BoolVar(&options.lock, "lock", false, "Resolve selected recipes and save verified artifacts in portprog.lock.json without installing")
	flags.BoolVar(&options.offline, "offline", false, "Use only the existing lockfile and verified local cache; never resolve remote artifacts")
	flags.BoolVar(&options.noInstall, "no-install", false, "Regenerate activation from existing receipts without downloads or changes to installed files")
	flags.BoolVar(&options.noGcc, "no-gcc", false, "Exclude GCC compiler recipes from package selection")
	flags.BoolVar(&options.noGo, "no-golang", false, "Exclude Go compiler recipes from package selection")
	flags.BoolVar(&options.noGit, "no-git", false, "Exclude recipes requiring Git source fetching")
	flags.BoolVar(&options.devel, "devel", false, "Select only recipes in packages-develop and their declared dependencies")
	flags.StringVar(&options.bundle, "bundle", "", "After successful installation, export a new directory containing verified offline installation images")
	flags.BoolVar(&options.repair, "repair", false, "Verify installed receipts and reinstall damaged packages from their recorded artifacts")
	flags.BoolVar(&options.list, "list", false, "List installed package identities and verify their recorded files without modifying them")
	flags.StringVar(&options.remove, "remove", "", "Remove an installed package ID from activation and preserve its files under retired")
	flags.BoolVar(&options.selfTest, "test", false, "Run a complete local archive installation, relocation and offline repair check, then exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments %q; use --help to see supported options", flags.Args())
	}
	if err := validateOptions(options); err != nil {
		return err
	}
	root, err := filepath.Abs(options.root)
	if err != nil {
		return fmt.Errorf("resolve installation directory %q: %w", options.root, err)
	}
	if err := validateRoot(root); err != nil {
		return err
	}
	settings, err := loadSettings(root)
	if err != nil {
		return err
	}
	if options.profile == "" {
		options.profile = settings.DefaultProfile
	}
	b := configuration(root, settings, output)
	b.Offline = b.Offline || options.offline
	if options.selfTest {
		return selfTest(b)
	}
	if options.validate {
		return validateCatalogAndProfiles(b)
	}
	if options.list {
		return listInstalled(b)
	}
	selectedExplicitly := options.file != "" || options.devel
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "profile" {
			selectedExplicitly = true
		}
	})
	return withInstallationLock(b, func() error {
		if options.noInstall {
			return activateInstalled(b)
		}
		if options.remove != "" {
			return removeInstalled(b, options.remove)
		}
		if options.repair {
			return repairInstalled(b)
		}
		return installSelected(b, options, selectedExplicitly)
	})
}

func validateOptions(options commandOptions) error {
	modes := 0
	for _, active := range []bool{options.validate, options.lock, options.noInstall, options.repair, options.list, options.remove != "", options.selfTest} {
		if active {
			modes++
		}
	}
	if modes > 1 {
		return fmt.Errorf("select exactly one of --validate, --lock, --no-install, --repair, --list, --remove or --test")
	}
	if options.file != "" && (options.devel || options.profile != "") || options.devel && options.profile != "" {
		return fmt.Errorf("--file, --profile and --devel select different package sources; specify only one")
	}
	if options.bundle != "" && modes != 0 {
		return fmt.Errorf("--bundle accompanies installation and cannot be combined with a separate operation")
	}
	selectionRequested := options.file != "" || options.profile != "" || options.devel || options.noGcc || options.noGo || options.noGit
	if selectionRequested && (options.validate || options.noInstall || options.repair || options.list || options.remove != "" || options.selfTest) {
		return fmt.Errorf("package selectors and compiler/Git exclusions apply only to installation or --lock, not the selected operation")
	}
	return nil
}

func validateRoot(root string) error {
	for _, character := range root {
		if character < 32 || character == 127 {
			return fmt.Errorf("installation directory %q contains control character U+%04X", root, character)
		}
	}
	if strings.ContainsRune(root, os.PathListSeparator) {
		return fmt.Errorf("installation directory %q contains a control character or the PATH separator %q", root, string(os.PathListSeparator))
	}
	return nil
}

func configuration(root string, settings Settings, output io.Writer) Config {
	return Config{
		RootDir: root, InstallDir: filepath.Join(root, "installs"), SzPath: settings.SevenZip,
		GoPath: filepath.Join(root, "langlibs", "gopath"), ZipDir: filepath.Join(root, "zips"),
		TempDir: filepath.Join(root, "temp"), OS: runtime.GOOS, Arch: runtime.GOARCH,
		Offline: settings.Offline, HTTPTimeout: time.Duration(settings.HTTPTimeoutSecs) * time.Second,
		MaxDownloadBytes: settings.MaxDownloadBytes, MaxExtractBytes: settings.MaxExtractBytes, Output: output,
	}
}

func installSelected(b Config, options commandOptions, selectedExplicitly bool) error {
	lockPath := filepath.Join(b.RootDir, "portprog.lock.json")
	lock, err := readLock(lockPath, b)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if b.Offline {
		if err != nil {
			return fmt.Errorf("offline installation requires lockfile %s: %w", lockPath, err)
		}
		if options.lock || selectedExplicitly {
			return fmt.Errorf("offline installation replays %s; package selection and --lock require online resolution", lockPath)
		}
	} else if options.lock || selectedExplicitly || errors.Is(err, os.ErrNotExist) {
		packages, selectionErr := selectedPackages(b, options)
		if selectionErr != nil {
			return selectionErr
		}
		lock, err = resolveLock(b, options.profile, packages)
		if err != nil {
			return err
		}
		if err := writeJSONAtomic(lockPath, lock); err != nil {
			return err
		}
		logLockSaved(b.Output, lockPath, len(lock.Packages))
	}
	lock.Packages, err = filterLocked(lock.Packages, options)
	if err != nil {
		return err
	}
	packages := make([]Package, 0, len(lock.Packages))
	for _, locked := range lock.Packages {
		packages = append(packages, locked.Package)
	}
	if err := checkInstalledOwnership(b, packages); err != nil {
		return err
	}
	if options.lock {
		return nil
	}
	if err := ensureInstallationDirectories(b); err != nil {
		return err
	}
	for _, p := range lock.Packages {
		receipt, readErr := readReceipt(filepath.Join(b.InstallDir, p.Package.Name), b)
		if readErr == nil && receipt.Package.Package.ID != p.Package.ID {
			return fmt.Errorf("package %s cannot replace directory %s owned by installed package %s", p.Package.ID, p.Package.Name, receipt.Package.Package.ID)
		}
		if readErr == nil && receipt.Package.SHA256 == p.SHA256 && packagesEqual(receipt.Package.Package, p.Package) {
			if verifyErr := verifyReceipt(filepath.Join(b.InstallDir, p.Package.Name), receipt); verifyErr == nil {
				logInstalled(b.Output, p.Package)
				continue
			}
		} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		logInstalling(b.Output, p.Package, filepath.Join(b.InstallDir, p.Package.Name))
		if err := installPackage(b, p); err != nil {
			return fmt.Errorf("install %s (%s): %w", p.Package.ID, p.Package.Version, err)
		}
		logInstalled(b.Output, p.Package)
	}
	if err := activateInstalled(b); err != nil {
		return err
	}
	if options.bundle != "" {
		return exportBundle(b, lock, options.bundle)
	}
	return nil
}

func ensureInstallationDirectories(b Config) error {
	for _, path := range []string{b.RootDir, b.InstallDir, b.ZipDir, b.TempDir, b.GoPath} {
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("create installation directory %s: %w", path, err)
		}
	}
	return nil
}
