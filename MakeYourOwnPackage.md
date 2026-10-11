# Make your own package

A package recipe is one JSON object describing a download, its installation plan, and its supported platform. Portprog validates recipes before downloading or installing them. Invalid JSON, unknown fields, unsupported plans, unsafe paths, missing dependencies, and dependency cycles are errors.

Put a recipe in the `packages-develop` directory under the Portprog root for local development. The other catalog directories are `packages` for Linux, `packages-windows` for Windows, `packages-osx` for macOS, and `packages-data` for data. Each recipe must explicitly declare its platform; its directory does not supply missing fields.

Validate the complete catalog and profiles without accessing the network:

```text
portprog --validate
```

Install only development recipes and their declared dependencies:

```text
portprog --devel
```

A standalone recipe can be selected by its absolute path. Relative paths are resolved from the caller's working directory:

```text
portprog --file /absolute/path/to/package.json
```

## Recipe example

This is the published Go 1.27.2 archive for Linux AMD64. The checksum comes from [Go's release metadata](https://go.dev/dl/?mode=json).

```json
{
    "ID": "go-1.27.2-linux-amd64",
    "Name": "go-1.27.2-linux-amd64",
    "Version": "1.27.2",
    "OS": "linux",
    "Arch": "amd64",
    "Zip": "go1.27.2.linux-amd64.tar.gz",
    "Url": "https://go.dev/dl/go1.27.2.linux-amd64.tar.gz",
    "Fetch": "web",
    "Plan": "zipWithDirectory",
    "Type": "application",
    "ArchiveRoot": "go",
    "BinDir": "bin",
    "SHA256": "ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5"
}
```

Do not copy that ID into another recipe: IDs must be unique throughout the catalog. An installation directory belongs to one selected package. Dependencies are installed before the packages that need them.

## Fields

| Field | Meaning and use |
| --- | --- |
| `ID` | Required stable package identity. Refer to this value in profiles and dependency lists. It must be a safe filename component. |
| `Name` | Required directory owned by this package inside the installation directory. It is independent of the directory inside an archive. Two selected packages cannot share it, including case-only differences. |
| `Version` | Optional version label. The lockfile records exact archive bytes or a Git commit; this label alone does not pin a moving URL. |
| `OS` | Required: `windows`, `darwin`, `linux`, or `all`. Use `all` only for genuinely portable data or sources. |
| `Arch` | Required: `386`, `amd64`, `arm64`, `arm`, or `all`. Use `all` only when the package supports every processor covered by its operating-system declaration. |
| `Url` | Required HTTPS download or repository URL. Absolute local `file:///` URLs are also supported for local fixtures; remote file hosts and URL credentials are rejected. |
| `Fetch` | Required: `web` for an archive or data file; `git` for a repository. |
| `Zip` | Required local filename for `web` downloads, including its actual extension. It must not contain a URL or directory. Omit it for Git. |
| `Plan` | Required installation plan from the table below. Values are case-sensitive. |
| `ArchiveRoot` | Optional relative directory inside an archive whose contents become the package installation. Use it when the layout is known; do not guess the archive root from its filename. |
| `SHA256` | Optional 64-digit published hexadecimal checksum. When present, downloaded bytes must match. Recipes without a published checksum acquire an exact digest when locked; that does not establish upstream authenticity independently of HTTPS. |
| `Branch` | Optional Git branch, tag, or commit to resolve. With no value, use the repository default. The lockfile records the resolved commit. |
| `BinDir` | Optional executable directory relative to the installed package, such as `bin`; use `.` for its root. Omit it when there are no executables to add to PATH. |
| `BinDirs` | Optional array of additional relative executable directories. Both this array and `BinDir` contribute to the generated environment. |
| `LibDir` | Optional relative library-directory metadata. It does not automatically change the system linker configuration. |
| `Dependencies` | Optional array of other package IDs needed first. Missing packages, cycles, duplicate dependencies, and incompatible dependency platforms are rejected. |
| `Deletes` | Optional relative file paths to remove from this package's staged contents before installation. Paths cannot escape the package or identify the whole installation. |
| `Type` | Optional `application`, `source`, or `data`. Data packages require `rawData`. |
| `Command` | Arbitrary command strings are unsupported. Omit this field or leave it empty. |

All local paths must use forward slashes and remain inside the package. Absolute paths, parent traversal, empty path components, control characters, Windows reserved filenames, and PATH separators are rejected. Existing recipes using `/bin` or `/` must use `bin` or `.` instead.

## Installation plans

| Plan | Behavior |
| --- | --- |
| `zipWithDirectory` | Extract an archive whose files are inside one top-level directory. An explicit `ArchiveRoot` selects that directory; otherwise it must be unambiguous. |
| `zipWithNoDirectory` | Extract archive contents directly into the package's installation directory. |
| `rawData` | Store a downloaded data file without treating it as an archive. |
| `standardConfigure` | Build sources using their configure or bootstrap script and install under the staged package prefix. An existing compiler and upstream build prerequisites are required. |
| `gitAndMake` | Build a locked source checkout using make and the staged installation prefix. |
| `goGetAndMake` | Build a locked Git checkout using Go and install executables under the staged package directory. |
| `cmake` | Configure, build, and install sources using CMake under the staged package prefix. |
| `msi` | Extract Windows MSI contents into the package directory. Windows only. |
| `dmg` | Read a macOS disk image and copy application contents into the package directory. macOS only. |
| `pkg` | Expand macOS package payloads into the package directory without running system installation scripts. macOS only. |

Archive extraction supports ZIP, TAR, gzip, and bzip2 directly. Other supported archive formats require 7-Zip. Source recipes may need additional upstream tools and libraries; declaring a recipe does not install those prerequisites unless they are listed as dependencies.

## Profiles and exact versions

Profiles are JSON files in the `profiles` directory under the Portprog root. Each contains a `description` explaining its purpose and a `packages` object mapping platforms to package-ID arrays. Platform keys are `OS/architecture`, `OS/all`, or `all/all`; matching entries are combined without duplicates.

```json
{
    "description": "Go for Linux AMD64.",
    "packages": {
        "linux/amd64": ["go-1.27.2-linux-amd64"]
    }
}
```

The supplied profiles are `default` (one current Go toolchain), `c-cpp` (C/C++ tools and sources), `scripting` (current Windows Python embeddable runtimes), and `legacy` (retained platform-compatible catalog entries). Legacy entries may depend on unavailable historical download servers. The scripting profile supports Windows only; the embeddable Python distribution does not include pip or development headers.

`--profile NAME` chooses a profile. The editable `default_profile` setting defaults to `default`. `--no-gcc` excludes GCC packages and `--no-golang` excludes Go toolchains from selection; these exclusions also apply when dependencies are resolved.

`--lock` resolves selected packages and writes `portprog.lock.json` without installing them:

```text
portprog --profile default --lock
```

`--offline` uses an existing lockfile and already cached artifacts without network access. It fails if required locked content is missing. Offline source builds are unsupported; `--bundle` exports installed images that can be used for offline installation and repair.

`--no-install` only regenerates activation scripts from installation receipts. It does not download, resolve a lockfile, or modify installed package contents:

```text
portprog --no-install
```

After adding or changing recipes, validate the complete catalog and run the regression suite. Tests use local temporary files and archives; they do not depend on internet access.
