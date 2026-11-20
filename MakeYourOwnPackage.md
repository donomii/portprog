# Make your own Portprog package

Portprog package definitions are strict JSON data. They cannot execute scripts
or installers. A package selects one immutable, prebuilt archive for each
supported operating-system and CPU pair.

## 1. Add the package manifest

Create `catalog/NAME.json`, where `NAME` is the lowercase package name. This is
a valid single-platform example using jq's immutable macOS ARM64 release:

```json
{
  "schema_version": 1,
  "name": "jq",
  "version": "1.8.2",
  "description": "A command-line JSON processor.",
  "homepage": "https://jqlang.org/",
  "license": "MIT",
  "dependencies": [],
  "assets": [
    {
      "os": "darwin",
      "arch": "arm64",
      "url": "https://github.com/jqlang/jq/releases/download/jq-1.8.2/jq-macos-arm64",
      "sha256": "90493b3bbd5e10f91d12153198bf1994fd756399b4fec93b49b0c6e2acdeeb3e",
      "size": 841504,
      "format": "raw",
      "strip_components": 0,
      "raw_path": "bin/jq",
      "bin_dirs": ["bin"],
      "executables": ["bin/jq"]
    }
  ]
}
```

The filename and `name` must match. Every field has a fixed purpose:

- `schema_version` is `1`, the current manifest format.
- `name` is the lowercase package identifier used by profiles and receipts.
- `version` is the immutable version installed in the versioned store.
- `description` explains what the package provides in plans and status output.
- `homepage` is the project's HTTPS page.
- `license` identifies the package's license.
- `dependencies` lists other catalog package names installed first.
- `assets` contains at most one record for each OS and CPU pair. Supported OS
  values are `darwin`, `linux`, and `windows`; CPU values are `amd64` and
  `arm64`. The user's platform must have exactly one matching asset.
- `url` is an immutable HTTPS publisher download URL.
- `sha256` is the publisher archive's exact lowercase SHA-256 digest.
- `size` is its exact byte count, or `0` only when the publisher supplies none.
- `format` is `raw`, `zip`, `tar.gz`, `tar.bz2`, or `tar.xz`.
- `strip_components` removes that many leading archive path components. It must
  be `0` for a raw asset.
- `raw_path` is the destination of a raw executable and is empty for archives.
- `bin_dirs` lists installation-relative directories added to the prepared
  shell's `PATH`. Use `.` for the installation root.
- `executables` lists installation-relative regular files that must exist after
  extraction. Portprog rejects the install if any are missing.

All asset paths use forward slashes and cannot be absolute, unclean, or contain
`..`. Obtain URLs, sizes, and SHA-256 values from the publisher; do not guess
them. Add more asset records to support more platforms.

## 2. Add the package to a profile

Create or edit a file under `profiles/`. For example:

```json
{
  "schema_version": 1,
  "name": "json-tools",
  "description": "Portable tools for working with JSON.",
  "packages": ["jq"]
}
```

The filename must be `json-tools.json`. Package names are unique; dependencies
do not need to be repeated because Portprog resolves them automatically.

## 3. Validate and install it

From the Portprog directory, run:

```sh
./portprog plan json-tools
./portprog update json-tools
```

`plan` strictly validates the catalog and explains the exact selected asset
without changing files. `update` downloads, verifies, installs, and records a
new exact lockfile. On Windows, use `portprog.exe` in place of `./portprog`.

See `SPEC.md` for the complete schemas, safety rules, and installation
transaction.
