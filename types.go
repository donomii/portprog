package main

import (
	"io"
	"time"
)

type Config struct {
	GoPath           string // Path to Go library files.
	InstallDir       string // The directory we install all package directories into.
	SzPath           string // Path to an already installed 7-Zip-compatible extractor; empty searches PATH.
	ZipDir           string
	TempDir          string
	RootDir          string
	OS               string
	Arch             string
	Offline          bool
	HTTPTimeout      time.Duration
	MaxDownloadBytes int64
	MaxExtractBytes  int64
	Output           io.Writer
}

type Package struct {
	ID           string
	Version      string
	Name         string // Name of the directory owned by this package under installs.
	Zip          string // Full archive name, including extension.
	Url          string // URL to download the archive from.
	Fetch        string // Fetch method: web or git.
	Plan         string // Build plan.
	Branch       string // Requested Git branch or tag, resolved to a commit in the lockfile.
	Command      string
	BinDir       string // Executable directory relative to the installed package.
	LibDir       string
	BinDirs      []string // Both BinDir and BinDirs are added to the path.
	Deletes      []string
	Type         string
	OS           string
	Arch         string
	ArchiveRoot  string
	SHA256       string
	Dependencies []string
	Patches      []Patch `json:",omitempty"`
	RelocateFrom string  `json:",omitempty"`
}

type Patch struct {
	Phase   string
	File    string
	Find    string
	Replace string
	Count   int
}

type LockedPackage struct {
	Package Package `json:"package"`
	SHA256  string  `json:"sha256"`
	Size    int64   `json:"size"`
	Commit  string  `json:"commit,omitempty"`
}

type LockFile struct {
	SchemaVersion int             `json:"schema_version"`
	OS            string          `json:"os"`
	Arch          string          `json:"arch"`
	Profile       string          `json:"profile"`
	Packages      []LockedPackage `json:"packages"`
}

type Profile struct {
	Description string              `json:"description"`
	Packages    map[string][]string `json:"packages"`
}

type InstalledFile struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
	Mode       uint32 `json:"mode"`
}

type Receipt struct {
	SchemaVersion int             `json:"schema_version"`
	Package       LockedPackage   `json:"package"`
	Files         []InstalledFile `json:"files"`
	Root          string          `json:"root,omitempty"`
}

type Settings struct {
	SchemaVersion    int    `json:"schema_version"`
	DefaultProfile   string `json:"default_profile"`
	Offline          bool   `json:"offline"`
	HTTPTimeoutSecs  int    `json:"http_timeout_seconds"`
	MaxDownloadBytes int64  `json:"max_download_bytes"`
	MaxExtractBytes  int64  `json:"max_extract_bytes"`
	SevenZip         string `json:"seven_zip"`
}
