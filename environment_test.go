package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestShellActivationQuotesPaths(t *testing.T) {
	paths := []string{"/tmp/Portable Tools/bin", "/tmp/user's-tool/bin"}
	tests := []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: `export PATH='/tmp/Portable Tools/bin':'/tmp/user'"'"'s-tool/bin':"$PATH"`},
		{shell: "zsh", want: `export PATH='/tmp/Portable Tools/bin':'/tmp/user'"'"'s-tool/bin':"$PATH"`},
		{shell: "fish", want: `set -gx PATH '/tmp/Portable Tools/bin' '/tmp/user\'s-tool/bin' $PATH`},
		{shell: "powershell", want: `$env:Path = '/tmp/Portable Tools/bin;/tmp/user''s-tool/bin;' + $env:Path`},
		{shell: "cmd", want: `set "PATH=/tmp/Portable Tools/bin;/tmp/user's-tool/bin;%PATH%"`},
	}
	for _, test := range tests {
		t.Run(test.shell, func(t *testing.T) {
			got, err := shellActivation(test.shell, paths)
			if err != nil {
				t.Fatalf("create %s activation: expected success, got %v", test.shell, err)
			} else if strings.TrimSpace(got) != test.want {
				t.Fatalf("create %s activation: expected %q, got %q", test.shell, test.want, strings.TrimSpace(got))
			} else {
				return
			}
		})
	}
}

func TestConfiguredUserShellUsesPortableUnixFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell fallback does not apply on Windows")
	} else {
		t.Setenv("SHELL", "/path/that/does/not/exist")
		if shell := configuredUserShell(); shell != "/bin/sh" {
			t.Fatalf("choose Unix shell fallback: expected /bin/sh, got %q", shell)
		} else {
			return
		}
	}
}

func TestShellActivationRejectsUnsupportedShellAndUnsafeCmdPath(t *testing.T) {
	_, unsupportedErr := shellActivation("csh", []string{"/tmp/bin"})
	if unsupportedErr == nil {
		t.Fatal("create unsupported shell activation: expected rejection, got success")
	} else {
		_, unsafeErr := shellActivation("cmd", []string{`C:\tools&bad`})
		if unsafeErr == nil {
			t.Fatal("create cmd activation with metacharacter: expected rejection, got success")
		} else {
			return
		}
	}
}
