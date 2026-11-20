# Portprog v1 Specification

## Summary

Portprog creates a portable programming environment inside its own directory.
It downloads immutable prebuilt tools, verifies every download, extracts each
tool without allowing files to escape the installation directory, records what
was installed, and opens a shell whose `PATH` contains the selected tools.

Portprog does not require an account or administrator access. It does not write
to system package directories, shell startup files, the registry, or locations
outside its configured base directory. Moving or deleting the complete Portprog
directory moves or removes the environment.

The first version supports raw executables and ZIP, tar.gz, tar.bz2, and tar.xz
archives. It deliberately does not run package-provided installers, build from
source, execute post-install scripts, or evaluate package definitions as code.

## User interactions

### No-argument launcher

`run.sh` on macOS and Linux, or `run.bat` on Windows, builds Portprog when
necessary and starts it without command-line arguments.

When attached to a terminal, Portprog displays these explained actions:

1. Install the configured default profile.
2. Open a shell using the installed tools from that profile.
3. Show the profile and installation status.
4. Check the configuration, catalog, cache, receipts, and installed files.
5. Show every configuration setting and its purpose.
6. Exit without changing anything.

The user enters the action number and presses Enter. Invalid input is rejected
with the list of valid choices. End-of-input exits without performing work.

When standard input is not a terminal, no-argument Portprog prints command help
and exits successfully instead of waiting for input.

### Commands

- `portprog install [profile]` installs the named profile. With no profile, it
  installs `default_profile` from `portprog.conf`.
- `portprog update [profile]` resolves the current catalog into a new lockfile,
  then installs it. Existing versions are retained until replacement succeeds.
- `portprog list [profile]` explains which packages are selected, installed,
  missing, or damaged. It does not change files.
- `portprog plan [profile]` displays package order, version, platform asset,
  download size when known, and installation destination. It does not download.
- `portprog remove PACKAGE` moves the installed package and its receipt into the
  configured trash directory. Removal is recoverable and does not delete data.
- `portprog doctor` validates all local configuration and state. It never
  downloads or repairs automatically.
- `portprog config` prints every effective setting, its current value, and an
  explanation of how it affects Portprog.
- `portprog env SHELL [profile]` prints activation statements for `bash`, `zsh`,
  `fish`, `powershell`, or `cmd`. Paths are quoted for the selected shell.
- `portprog shell [profile]` opens the current user shell with the profile's
  installed binary directories prepended to `PATH`. It does not edit startup
  files and returns when the child shell exits.
- `portprog pack FILE [profile]` writes a ZIP offline pack containing the
  current-platform lockfile and all verified cached package archives.
- `portprog restore FILE` verifies and imports an offline pack, writes its
  lockfile, and installs its profile without network access.
- `portprog help` prints the command list and explanations.
- `portprog -test` runs a fast built-in test of configuration parsing, strict
  package validation, path containment, and safe archive extraction.

All failures print the attempted task, the expected result, the actual result,
and supporting paths or package names. A failed command returns a non-zero exit
status. Successful commands return zero.

## Configuration file

`portprog.conf` is beside the source checkout or distributed executable.
Portprog prefers the file beside its executable, then checks the current
directory to support `go run`. It is a UTF-8 line-oriented file. Blank lines and lines beginning with `#` are
ignored. Every other line is `key = value`. Keys are unique and case-sensitive.
Unknown keys, duplicate keys, malformed integers, malformed booleans, absolute
paths, and paths containing `..` are rejected.

Required settings:

- `schema_version`: configuration format version; must be `1`.
- `default_profile`: profile used when a command omits a profile name.
- `install_root`: relative directory containing the versioned package store.
- `cache_root`: relative directory containing content-addressed downloads.
- `state_root`: relative directory containing installation receipts.
- `trash_root`: relative directory receiving recoverably removed or replaced
  installations.
- `catalog_dir`: relative directory containing package manifests.
- `profiles_dir`: relative directory containing profile files.
- `lock_file`: relative current-platform lockfile path.
- `offline`: when `true`, network requests are forbidden and missing cached
  archives are errors; when `false`, missing archives may be downloaded.
- `max_download_mb`: maximum accepted response size in mebibytes.
- `max_extract_mb`: maximum total uncompressed bytes produced by one archive,
  expressed in mebibytes, to limit archive expansion.
- `download_timeout_seconds`: whole-request HTTP timeout in seconds.

The two mebibyte limits must fit when converted to bytes. The download timeout
must be from 1 through 86,400 seconds, preventing duration overflow and
accidental multi-day hangs.

The Portprog base directory is resolved to its canonical existing location.
All relative paths must remain inside it after cleaning. Existing symbolic
links in configured, state, cache, package, and extraction paths are rejected
so a link cannot redirect reads or writes outside the portable directory.
The install, cache, state, trash, catalog, and profile roots must not overlap;
the lockfile must also remain outside each root.

## Package manifest format

Package files are UTF-8 JSON in `catalog_dir` with extension `.json`. JSON is
decoded strictly: unknown fields, duplicate top-level content, missing fields,
invalid values, and trailing data are errors. Metadata files are limited to 4
MiB and JSON nesting is limited to 256 levels.

`PackageManifest` fields:

- `schema_version`: integer, exactly `1`.
- `name`: lowercase package identifier using letters, digits, hyphens, or dots.
- `version`: non-empty immutable package version.
- `description`: user-facing explanation of the installed tool.
- `homepage`: HTTPS project page.
- `license`: non-empty SPDX license expression or documented license name.
- `dependencies`: package identifiers installed before this package.
- `assets`: one or more `PackageAsset` records.

`PackageAsset` fields:

- `os`: `darwin`, `linux`, or `windows`.
- `arch`: `amd64` or `arm64`.
- `url`: immutable HTTPS download URL.
- `sha256`: lowercase 64-character SHA-256 digest.
- `size`: expected bytes, or zero when the publisher does not provide a size.
- `format`: `raw`, `zip`, `tar.gz`, `tar.bz2`, or `tar.xz`.
- `strip_components`: count of leading archive path elements removed during
  extraction; zero for raw assets.
- `raw_path`: installation-relative destination for a raw asset; empty for an
  archive.
- `bin_dirs`: installation-relative directories prepended to `PATH`.
- `executables`: installation-relative regular files required after extraction.

Exactly one asset may match a given package, operating system, and architecture.
All asset-relative paths are clean, non-absolute, non-empty where required, and
cannot contain `..`, control characters, Windows-invalid characters or device
names, or components ending in a dot or space.

## Profile format

Profile files are strict UTF-8 JSON in `profiles_dir`.

`Profile` fields:

- `schema_version`: integer, exactly `1`.
- `name`: identifier matching its filename without `.json`.
- `description`: explanation shown in menus, plans, and status.
- `packages`: non-empty ordered package identifier list without duplicates.

Dependencies are resolved depth-first before dependents. A missing dependency or
dependency cycle rejects the entire plan before downloads begin.

## Lockfile format

`LockFile` is strict JSON with:

- `schema_version`: integer, exactly `1`.
- `profile`: profile name.
- `profile_description`: copied profile description.
- `os` and `arch`: platform for every selected asset.
- `generated_at`: UTC RFC 3339 timestamp.
- `packages`: dependency-ordered `LockedPackage` records.

Each `LockedPackage` contains the manifest name, version, description, license,
dependencies, and the complete selected asset. Install uses a compatible
existing lockfile. Update deliberately resolves and replaces the lockfile.

## Receipt format

One strict JSON `Receipt` is stored per installed package under
`state_root/receipts`:

- schema version, name, version, OS, architecture;
- source URL and SHA-256 digest;
- installation-relative store path;
- binary directories and required executables;
- UTC installation timestamp.

A package is healthy only when its receipt matches the lock, its recorded store
path is the exact versioned destination for that package, and every expected
executable is a runnable regular file there. Windows does not use Unix
executable permission bits. Lock, receipt, and pack timestamps must be RFC 3339
times at UTC offset zero.

## Directory layout

```text
portprog.conf
portprog.lock.json
catalog/
profiles/
portable/store/NAME/VERSION/OS-ARCH/
portable/go.mod
cache/sha256/DIGEST
state/receipts/NAME.json
trash/TIMESTAMP-NAME-VERSION/
```

The exact root directory names come from `portprog.conf`.

Portprog owns `go.mod` directly under `install_root` and requires it to contain
`module portprog.local/installed`. This nested module boundary prevents Go's
repository-wide package discovery from treating installed SDK source trees as
Portprog source. Portprog creates the marker when absent and rejects different
existing content instead of overwriting it.

## Download algorithm

```text
resolve the digest-named cache path
if a regular cached file has the expected digest and optional size
    reuse it
else
    preserve an invalid cache entry with an invalid timestamp suffix
    if offline mode is active
        report that a verified cached archive was required
    else
        require an HTTPS URL
        reject a known publisher size above the configured maximum before requesting
        request the identity representation with the configured whole-request timeout
        require every redirect to remain HTTPS and credential-free
        require HTTP status 200
        reject a declared response larger than the configured maximum
        stream at most maximum plus one byte into a temporary cache file
        sync and close the temporary file
        require expected size when non-zero
        require expected SHA-256
        atomically rename the temporary file to its digest path
return the verified cache path
```

Temporary download files remain inside the cache directory. Failed temporary
files are moved to trash where possible or removed only when they contain newly
created, unverified temporary data.

## Extraction algorithm

```text
create a staging directory beside the final store directory
for every archive entry
    remove the configured number of leading path components
    skip an entry removed completely by stripping
    reject absolute paths, parent traversal, links, devices, and unsupported types
    require the resulting path to remain under staging
    create directories with safe permissions
    copy regular file bytes without following links
    normalize file permissions to 0644 or 0755
    reject an archive containing more than 100,000 entries
for a raw asset
    copy the verified cache file to raw_path
require every declared binary directory to be a contained directory
require every declared executable to be a regular contained file
make declared executables mode 0755
move any prior final directory to trash
atomically rename staging to the final store directory
write the receipt atomically
```

If any operation fails before the final rename, the prior installation and
receipt remain active. Newly created staging data is moved to trash.

## Offline packs

An offline pack is a ZIP with `lock.json`, `pack.json`, and one file named
`archives/SHA256` for each locked package. `pack.json` identifies schema version,
profile, OS, architecture, and creation time. Pack creation re-verifies every
cache file. Restore rejects duplicate paths, links, unexpected files, platform
mismatches, missing archives, size mismatches, and digest mismatches before
importing anything. Imported files use the same content-addressed cache paths.

## Public implementation functions

Portprog is an executable rather than a Go library. Its public interface is the
command behavior and disk formats above. Shared implementation data structures
live in `types_v2.go`. Important module responsibilities are:

- configuration: locate, parse, explain, and validate settings;
- catalog: strictly load packages and profiles, validate assets, resolve
  dependencies, and produce locks;
- downloader: obtain one verified cache object;
- archive: safely extract one verified object into staging;
- state: atomically read and write locks and receipts and move data to trash;
- installer: coordinate download, extraction, verification, publishing, pack,
  restore, status, and removal;
- environment: quote activation output and run a child shell;
- CLI: parse user commands, display explanations, and provide the interactive
  no-argument launcher.

## Validation

`go test ./...` covers configuration rejection, strict JSON, manifest rules,
dependency ordering and cycles, platform selection, HTTP failures, maximum
sizes, checksum failures, offline behavior, archive traversal and links,
transaction rollback, idempotent install, receipts, recoverable removal,
activation quoting, pack verification, and restore.

`portprog -test` is offline and completes without modifying project state.
Release validation also runs `go vet ./...`, `go fmt ./...`, `go build .`, all
launcher tests, and a real HTTPS installation of the small starter profile on
each supported operating-system and architecture combination.
