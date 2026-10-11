package main

import (
	"fmt"
	"io"
)

func logFailure(output io.Writer, err error) {
	fmt.Fprintf(output, "Portprog failed: %v\n", err)
}

func logResolving(output io.Writer, p Package) {
	fmt.Fprintf(output, "Resolving %s (%s) from %s\n", p.ID, p.Version, p.Url)
}

func logInstalling(output io.Writer, p Package, root string) {
	fmt.Fprintf(output, "Installing %s (%s) into %s\n", p.ID, p.Version, root)
}

func logInstalled(output io.Writer, p Package) {
	fmt.Fprintf(output, "Installed and verified %s (%s)\n", p.ID, p.Version)
}

func logLockSaved(output io.Writer, path string, count int) {
	fmt.Fprintf(output, "Saved %d verified package artifacts in lockfile %s\n", count, path)
}

func logActivation(output io.Writer, root string, count int) {
	fmt.Fprintf(output, "Generated activation scripts for %d installed packages in %s; source environment.fish or environment.bash, run environment.bat, or dot-source environment.ps1 in your shell.\n", count, root)
}

func logValidated(output io.Writer, packages, profiles int) {
	fmt.Fprintf(output, "Validated %d package recipes and %d profiles without network access\n", packages, profiles)
}

func logReceipt(output io.Writer, receipt Receipt, err error) {
	if err != nil {
		fmt.Fprintf(output, "%s (%s): verification failed: %v\n", receipt.Package.Package.ID, receipt.Package.Package.Version, err)
	} else {
		fmt.Fprintf(output, "%s (%s): installed files match their receipt\n", receipt.Package.Package.ID, receipt.Package.Package.Version)
	}
}

func logRetired(output io.Writer, id, destination string) {
	fmt.Fprintf(output, "Removed %s from the active installation; preserved its files at %s\n", id, destination)
}

func logBundle(output io.Writer, destination string, count int) {
	fmt.Fprintf(output, "Exported %d installed packages as a verified offline bundle at %s; its launcher installs from the included cache without rebuilding source packages.\n", count, destination)
}

func logSelfTest(output io.Writer, destination string) {
	fmt.Fprintf(output, "Self-test passed: local archive installation, receipt verification, activation, relocation and offline repair in %s\n", destination)
}

func logUnmanaged(output io.Writer, path string) {
	fmt.Fprintf(output, "Directory %s has no Portprog installation receipt and is excluded from activation; installing its recipe creates a verified record.\n", path)
}

func logNoInstalled(output io.Writer, path string) {
	fmt.Fprintf(output, "No packages with installation receipts were found in %s\n", path)
}
