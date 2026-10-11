package main

import (
	"fmt"
	"strings"
)

func writeEnvironment(b Config, packages []LockedPackage) error {
	documents, err := environmentDocuments(packages, b.OS == "windows")
	if err != nil {
		return err
	}
	return publishDocuments(b.RootDir, documents)
}

func environmentDocuments(packages []LockedPackage, windows bool) (map[string][]byte, error) {
	paths, err := environmentPaths(packages, windows)
	if err != nil {
		return nil, err
	}
	scripts := []struct {
		name string
		text string
	}{
		{"environment.fish", fishEnvironment(paths)},
		{"environment.bash", bashEnvironment(paths)},
		{"environment.bat", batchEnvironment(paths)},
		{"environment.ps1", powershellEnvironment(paths)},
	}
	documents := make(map[string][]byte)
	for _, script := range scripts {
		documents[script.name] = []byte(script.text)
	}
	return documents, nil
}

func environmentPaths(packages []LockedPackage, windows bool) ([]string, error) {
	paths := []string{}
	seen := map[string]bool{}
	for _, installed := range packages {
		p := installed.Package
		if err := environmentRelativePath(p.Name, false); err != nil {
			return nil, fmt.Errorf("package %q installation directory: %w", p.ID, err)
		}
		directories := append([]string{}, p.BinDirs...)
		if p.BinDir != "" {
			directories = append([]string{p.BinDir}, directories...)
		}
		for _, directory := range directories {
			if err := environmentRelativePath(directory, true); err != nil {
				return nil, fmt.Errorf("package %q executable directory: %w", p.ID, err)
			}
			relative := "installs/" + p.Name
			if directory != "." {
				relative += "/" + directory
			}
			key := relative
			if windows {
				key = strings.ToLower(key)
			}
			if !seen[key] {
				paths = append(paths, relative)
				seen[key] = true
			}
		}
	}
	return append(paths, "langlibs/gopath/bin"), nil
}

func environmentRelativePath(value string, nested bool) error {
	if value == "" || strings.ContainsAny(value, "\\:;\"\r\n\x00") {
		return fmt.Errorf("expected a nonempty relative path without control characters, quotes or path-list separators; received %q", value)
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return fmt.Errorf("relative path %q contains a control character", value)
		}
	}
	if nested && value == "." {
		return nil
	}
	parts := strings.Split(value, "/")
	if !nested && len(parts) != 1 {
		return fmt.Errorf("expected one directory name; received %q", value)
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("relative path %q contains an empty, current or parent directory component", value)
		}
	}
	return nil
}

func shellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func fishLiteral(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	return "'" + strings.ReplaceAll(escaped, "'", "\\'") + "'"
}

func bashEnvironment(paths []string) string {
	var script strings.Builder
	script.WriteString("# Source this file to use this portable installation in the current Bash shell.\n")
	script.WriteString("_portprog_root=$(CDPATH= cd -- \"$(dirname -- \"${BASH_SOURCE[0]}\")\" && pwd -L) || return\n")
	script.WriteString("if [[ \"$_portprog_root\" == *:* ]]; then\n  printf 'Portprog root %s contains a colon and cannot be represented in PATH.\\n' \"$_portprog_root\" >&2\n  return 1\nfi\n")
	script.WriteString("export GOPATH=\"$_portprog_root/langlibs/gopath\"\n_portprog_entries=(\n")
	for _, relative := range paths {
		fmt.Fprintf(&script, "  \"$_portprog_root\"%s\n", shellLiteral("/"+relative))
	}
	script.WriteString(")\n" + bashPathDeduplication)
	return script.String()
}

const bashPathDeduplication = `_portprog_remaining="${PATH-}"
while [[ "$_portprog_remaining" == *:* ]]; do
  _portprog_entries+=("${_portprog_remaining%%:*}")
  _portprog_remaining="${_portprog_remaining#*:}"
done
_portprog_entries+=("$_portprog_remaining")
_portprog_unique=()
for _portprog_entry in "${_portprog_entries[@]}"; do
  _portprog_present=false
  for _portprog_previous in "${_portprog_unique[@]}"; do
    if [[ "$_portprog_entry" == "$_portprog_previous" ]]; then
      _portprog_present=true
      break
    fi
  done
  if [[ "$_portprog_present" == false ]]; then
    _portprog_unique+=("$_portprog_entry")
  fi
done
PATH=
_portprog_separator=
for _portprog_entry in "${_portprog_unique[@]}"; do
  PATH="$PATH$_portprog_separator$_portprog_entry"
  _portprog_separator=:
done
export PATH
unset _portprog_root _portprog_entries _portprog_remaining _portprog_unique _portprog_entry
unset _portprog_present _portprog_previous _portprog_separator
`

func fishEnvironment(paths []string) string {
	var script strings.Builder
	script.WriteString("# Source this file to use this portable installation in the current Fish shell.\n")
	script.WriteString("set -l _portprog_root (cd -- (dirname -- (status filename)); and pwd -L)\n")
	script.WriteString("if test (count $_portprog_root) -ne 1\n  return 1\nend\n")
	script.WriteString("if string match -q '*:*' -- \"$_portprog_root\"\n  printf 'Portprog root %s contains a colon and cannot be represented in PATH.\\n' \"$_portprog_root\" >&2\n  return 1\nend\n")
	script.WriteString("set -gx GOPATH \"$_portprog_root/langlibs/gopath\"\nset -l _portprog_entries")
	for _, relative := range paths {
		fmt.Fprintf(&script, " \"$_portprog_root\"%s", fishLiteral("/"+relative))
	}
	script.WriteString(" $PATH\nset -l _portprog_unique\n")
	script.WriteString("for _portprog_entry in $_portprog_entries\n")
	script.WriteString("  if not contains -- \"$_portprog_entry\" $_portprog_unique\n")
	script.WriteString("    set -a _portprog_unique \"$_portprog_entry\"\n  end\nend\n")
	script.WriteString("set -gx PATH $_portprog_unique\n")
	return script.String()
}

func batchEnvironment(paths []string) string {
	var script strings.Builder
	script.WriteString("@echo off\nrem Call this file to use this portable installation in the current Command Prompt.\n")
	script.WriteString("if \"!\"==\"\" (\n  echo Portprog activation requires Command Prompt delayed expansion to be disabled so paths containing exclamation marks remain intact. 1>&2\n  exit /b 1\n)\n")
	script.WriteString("set \"_portprog_root=%~dp0\"\n")
	script.WriteString("if not \"%_portprog_root:;=%\"==\"%_portprog_root%\" (\n  echo Portprog root \"%_portprog_root%\" contains a semicolon and cannot be represented in PATH. 1>&2\n  set \"_portprog_root=\"\n  exit /b 1\n)\n")
	script.WriteString("set \"GOPATH=%~dp0langlibs\\gopath\"\n")
	script.WriteString("set \"_portprog_existing=%PATH:\"=%\"\n")
	for i := len(paths) - 1; i >= 0; i-- {
		relative := strings.ReplaceAll(strings.ReplaceAll(paths[i], "/", "\\"), "%", "%%")
		script.WriteString("set \"_portprog_present=\"\n")
		fmt.Fprintf(&script, "for %%%%P in (\"%%_portprog_existing:;=\" \"%%\") do if /I \"%%%%~P\"==\"%%~dp0%s\" set \"_portprog_present=1\"\n", relative)
		fmt.Fprintf(&script, "if not defined _portprog_present set \"PATH=%%~dp0%s;%%PATH%%\"\n", relative)
	}
	script.WriteString("set \"_portprog_root=\"\nset \"_portprog_existing=\"\nset \"_portprog_present=\"\nexit /b 0\n")
	return strings.ReplaceAll(script.String(), "\n", "\r\n")
}

func powershellEnvironment(paths []string) string {
	var script strings.Builder
	script.WriteString("# Dot-source this file to use this portable installation in the current PowerShell session.\n")
	script.WriteString("$portprogRoot = $PSScriptRoot\n")
	script.WriteString("if ($portprogRoot.IndexOf([IO.Path]::PathSeparator) -ge 0) {\n  throw \"Portprog root '$portprogRoot' contains the PATH separator and cannot be represented in PATH.\"\n}\n")
	script.WriteString("$env:GOPATH = Join-Path $portprogRoot 'langlibs/gopath'\n")
	script.WriteString("$portprogPaths = @(\n")
	for _, relative := range paths {
		fmt.Fprintf(&script, "  (Join-Path $portprogRoot '%s')\n", strings.ReplaceAll(relative, "'", "''"))
	}
	script.WriteString(") + @($env:PATH -split [IO.Path]::PathSeparator)\n")
	script.WriteString("$portprogComparison = [StringComparer]::Ordinal\n")
	script.WriteString("if ([IO.Path]::PathSeparator -eq ';') { $portprogComparison = [StringComparer]::OrdinalIgnoreCase }\n")
	script.WriteString("$portprogSeen = [Collections.Generic.HashSet[string]]::new($portprogComparison)\n")
	script.WriteString("$portprogUnique = @(foreach ($portprogEntry in $portprogPaths) {\n")
	script.WriteString("  if ($portprogSeen.Add($portprogEntry)) { $portprogEntry }\n})\n")
	script.WriteString("$env:PATH = [string]::Join([IO.Path]::PathSeparator, [string[]]$portprogUnique)\n")
	script.WriteString("Remove-Variable portprogRoot, portprogPaths, portprogComparison, portprogSeen, portprogUnique, portprogEntry\n")
	return script.String()
}
