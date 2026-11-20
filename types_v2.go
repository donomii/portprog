package main

import (
	"io"
	"net/http"
	"time"
)

const (
	currentSchemaVersion = 1
	configFilename       = "portprog.conf"
	defaultFileMode      = 0o644
	defaultDirMode       = 0o755
	executableFileMode   = 0o755
	sha256HexLength      = 64
)

// Settings contains every user-editable Portprog setting.
type Settings struct {
	SchemaVersion          int
	DefaultProfile         string
	InstallRoot            string
	CacheRoot              string
	StateRoot              string
	TrashRoot              string
	CatalogDir             string
	ProfilesDir            string
	LockFile               string
	Offline                bool
	MaxDownloadMB          int64
	MaxExtractMB           int64
	DownloadTimeoutSeconds int
	BaseDir                string
}

// AppPaths contains absolute paths derived from Settings and the base directory.
type AppPaths struct {
	BaseDir      string
	InstallRoot  string
	StoreRoot    string
	CacheRoot    string
	CacheObjects string
	StateRoot    string
	ReceiptsRoot string
	TrashRoot    string
	CatalogDir   string
	ProfilesDir  string
	LockFile     string
}

// Platform identifies one supported operating-system and CPU pair.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// PackageManifest is the strict catalog definition for one package version.
type PackageManifest struct {
	SchemaVersion int            `json:"schema_version"`
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	Description   string         `json:"description"`
	Homepage      string         `json:"homepage"`
	License       string         `json:"license"`
	Dependencies  []string       `json:"dependencies"`
	Assets        []PackageAsset `json:"assets"`
}

// PackageAsset describes one immutable download for one platform.
type PackageAsset struct {
	OS              string   `json:"os"`
	Arch            string   `json:"arch"`
	URL             string   `json:"url"`
	SHA256          string   `json:"sha256"`
	Size            int64    `json:"size"`
	Format          string   `json:"format"`
	StripComponents int      `json:"strip_components"`
	RawPath         string   `json:"raw_path"`
	BinDirs         []string `json:"bin_dirs"`
	Executables     []string `json:"executables"`
}

// Profile names an ordered set of packages to install and activate together.
type Profile struct {
	SchemaVersion int      `json:"schema_version"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Packages      []string `json:"packages"`
}

// LockFile freezes one profile to exact current-platform package assets.
type LockFile struct {
	SchemaVersion      int             `json:"schema_version"`
	Profile            string          `json:"profile"`
	ProfileDescription string          `json:"profile_description"`
	OS                 string          `json:"os"`
	Arch               string          `json:"arch"`
	GeneratedAt        string          `json:"generated_at"`
	Packages           []LockedPackage `json:"packages"`
}

// LockedPackage is the immutable package record used during installation.
type LockedPackage struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Description  string       `json:"description"`
	License      string       `json:"license"`
	Dependencies []string     `json:"dependencies"`
	Asset        PackageAsset `json:"asset"`
}

// Receipt records the exact package content published in the local store.
type Receipt struct {
	SchemaVersion int      `json:"schema_version"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	URL           string   `json:"url"`
	SHA256        string   `json:"sha256"`
	StorePath     string   `json:"store_path"`
	BinDirs       []string `json:"bin_dirs"`
	Executables   []string `json:"executables"`
	InstalledAt   string   `json:"installed_at"`
}

// PackManifest identifies and validates an offline pack.
type PackManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Profile       string `json:"profile"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	CreatedAt     string `json:"created_at"`
}

// PackageHealth is a read-only result explaining one installed package state.
type PackageHealth struct {
	Name    string
	Version string
	State   string
	Detail  string
}

// Dependencies contains replaceable process boundaries for tests.
type Dependencies struct {
	HTTPClient *http.Client
	Now        func() time.Time
	Input      io.Reader
	Output     io.Writer
	Error      io.Writer
}

// App contains resolved settings, paths, platform, and process dependencies.
type App struct {
	Settings          Settings
	Paths             AppPaths
	Platform          Platform
	Deps              Dependencies
	AllowHTTPForTests bool
}

// SettingExplanation pairs a configuration value with user-facing guidance.
type SettingExplanation struct {
	Name        string
	Value       string
	Explanation string
}
