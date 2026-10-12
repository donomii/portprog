# Portable Programmer

Portprog creates a directory containing selected development tools, their verified download cache, installation records, and shell activation scripts. A profile selects a small set of tools for the current operating system and processor architecture. A lockfile records the exact artifacts selected. Existing historical recipes remain available individually and through profiles.

The supported installer hosts are Windows, macOS and Linux on the architectures declared by each recipe. Selecting a profile with no matching platform is an error. Source packages additionally require their declared build tools and any upstream build prerequisites. Portprog does not install prerequisites into the operating system or run a global package installer.

## Launching and checking

`run.sh` on Unix or `run.bat` on Windows is the no-argument launcher. In a source checkout it builds Portprog before starting it. In an exported bundle it starts the included executable. The default profile and limits are editable in `portprog.json` beside the executable. A first normal launch resolves the selected profile, writes a lockfile, installs its packages and writes activation scripts. Later launches replay the lockfile and reuse installations whose receipts still match.

`build.sh`/`build.bat` compile the executable. `test.sh`/`test.bat` run the full Go regression suite, compile the program, run its local `-test` installation check and validate the catalog. `demo.sh` runs the local demonstration/checks. `install.sh` starts the normal launcher. The Makefile exposes build, test, demo and install targets. Building Portprog itself needs Go 1.22.9 or later and no third-party Go modules. The Unix launchers explicitly include a build UUID so older supported Go linkers produce executables accepted by macOS 26; direct builds with older Go need `-ldflags=-B=gobuildid` too. Go source installations on macOS include the same linker setting.

The local `-test` check creates an archive, installs and verifies it, exports and replays an offline installation image, moves the installation, damages a file and repairs it without contacting a remote service. It prints the retained diagnostic directory. The test fixtures do not download or execute catalog software.

## User operations

Every invocation completes with status zero on success or a nonzero status and contextual error on failure. There is no indefinite completion loop. `--help` explains each option.

| Option | Meaning |
| --- | --- |
| `--root DIRECTORY` | Use this portable installation directory; default is the executable's directory. |
| `--profile NAME` | Select `profiles/NAME.json` and resolve a new lock for that profile. Without an explicit profile, an existing lock is replayed. |
| `--file FILE` | Select one recipe plus its declared dependencies. Relative filenames refer to the caller's directory. |
| `--devel` | Select only recipes in the installation's `packages-develop` directory and their declared dependencies. |
| `--lock` | Resolve selected artifacts and write `portprog.lock.json`, without installing packages. |
| `--offline` | Replay the existing lock using verified cached artifacts. Remote resolution is forbidden. Exported installation images do not require rebuilding source. |
| `--no-install` | Verify existing receipts and regenerate activation scripts; do not download, build, install or change package files. |
| `--no-gcc` | Exclude GCC compiler recipes from the selected work. |
| `--no-golang` | Exclude Go compiler recipes from the selected work. |
| `--no-git` | Exclude recipes that require Git fetching. |
| `--validate` | Check every catalog recipe, dependency selection and profile without network requests. It does not certify remote URL availability. |
| `--list` | Display installed package identities and whether files match their receipts. Mismatches produce a failure status. |
| `--repair` | Reinstall packages with missing or changed files from their recorded artifact, then regenerate activation. The lockfile recovers packages whose entire directory or receipt is missing. |
| `--remove ID` | Remove one package from activation and the lockfile, preserving its installed directory under `retired`. Refuse removal when an installed or locked package depends on it. |
| `--bundle DIRECTORY` | After installation succeeds, export a new, previously nonexistent offline bundle directory. |
| `-test` | Run the local installation, relocation and repair check. |

Selection options are mutually exclusive. Validation, lock-only resolution, activation-only regeneration, repair, listing, removal and self-test are distinct operations. Exclusions apply before resolution; if they remove a required dependency, the request fails before downloads. Excluding the entire selection creates an empty lock and installs nothing. Removing the final package leaves an empty lock so the next launch does not reinstall it.

A per-installation reservation file prevents concurrent modifying operations. It records a start time, not a process identity. An interrupted run can leave the reservation behind; later invocations report its full path and refuse concurrent changes. Listing and catalog validation are read-only operations. Failed package work, previous installed trees and previous metadata files are retained for diagnosis/recovery.

## Settings

`portprog.json` contains a single JSON object. If absent, these same defaults apply. Unknown fields, duplicate fields, trailing JSON values, invalid types and invalid limits are errors.

| Field | Default | Meaning |
| --- | --- | --- |
| `schema_version` | `1` | Version of the settings format; only version 1 is accepted. |
| `default_profile` | `"default"` | Profile used when resolving a first installation or explicitly creating a new lock. |
| `offline` | `false` | Make offline replay the default; exported bundles set this to true. |
| `http_timeout_seconds` | `120` | Positive request deadline, including response-body transfer; maximum 86400 seconds. |
| `max_download_bytes` | `8589934592` | Maximum accepted artifact size in bytes. |
| `max_extract_bytes` | `17179869184` | Maximum expanded content size in bytes for one package. |
| `seven_zip` | `""` | Optional path to an already installed 7-Zip-compatible extractor. Empty searches PATH for `7zz` or `7z`; native ZIP/tar extraction does not need it. |

Installation paths may contain spaces and shell punctuation. Control characters and the host's PATH-list separator are rejected because they cannot be represented safely in the generated environment.

## Package and profile contracts

Package recipes contain identity, version, installation-directory name, operating system, architecture, fetch method, artifact URL, artifact filename, install plan, executable directories and optional dependency IDs. Optional fields describe a published SHA-256, source revision, explicit archive root, library location, package type and files to omit. The exact recipe fields and examples are documented in `MakeYourOwnPackage.md`.

Identity is distinct from the installation directory and archive root. Each selected package owns one installation directory. Conflicting IDs, dependency cycles, missing dependencies and competing ownership of the same directory are errors. Dependencies are installed before dependents. Paths within packages are relative and must remain within the package directory. A recipe cannot request an arbitrary command string.

A profile is an object containing a human-readable `description` and a `packages` map. Each map key is `operating-system/architecture`; values are package ID lists. Selection combines `all/all`, `all/architecture`, `operating-system/all` and the exact target, removing duplicate IDs. Supported operating-system values are `windows`, `darwin`, `linux` and `all`; supported architecture values are `386`, `amd64`, `arm64` and `all`.

The default profile contains a current Go toolchain. The C/C++ and scripting profiles choose compatible tools where recipes exist. The legacy profile and individual recipe selection retain older versions without installing every historical version by default. Catalog validation checks all declared platforms, even when run on another host.

## Resolution and installation

1. Read and validate all selected recipes and their dependency graph.
2. Resolve each artifact. Network downloads require HTTPS with normal certificate verification and a successful HTTP response. Local file URLs refer to local files only. Git sources are resolved to exact commits, with submodules at the revisions recorded by their parent.
3. Check published hashes when supplied. Otherwise a first HTTPS resolution records the observed SHA-256; this is an integrity pin for future use, not independent publisher authentication. Save completed, verified artifacts under their content hash. Failed or incomplete downloads never become valid cache entries.
4. Save the complete lock only after every selected artifact resolves successfully.
5. Before installation, verify each cached artifact's recorded size and SHA-256. Work in a separate staging directory. Archive paths, link targets and expanded content are checked before publication.
6. Run only the selected plan with an explicit working directory and installation destination. A failed command stops dependent work and preserves diagnostics.
7. Record installed files and their hashes, permissions and link targets. Publish the complete package directory, preserving an older installation under `backups`.
8. Generate activation from verified installation receipts. Directories without receipts are reported and excluded from activation.

Native ZIP, gzip-compressed tar, bzip2-compressed tar and tar extraction use local archive readers. Other archive formats require a suitable already installed extractor; Portprog does not download and execute an extractor installer. Native installer plans extract payloads into staging rather than perform a system installation. Source build plans require host tools and remain subject to upstream build-system behavior. A source program that embeds absolute paths needs upstream relocation support; generating a relative activation script cannot make such a program relocatable by itself.

On Windows, configure and CMake builds install into a staging prefix because Unix `DESTDIR` cannot represent a Windows drive prefix. Programs that embed that prefix need upstream relocation support. On Unix, these builds use the final installation prefix with a separate staging root.

Staging is not a sandbox: source builds and MSI administrative actions execute upstream code. External extractors may expand temporary files before Portprog checks the payload-copy size limit. 7-Zip listings are checked for paths, links, collisions and advertised sizes before extraction. Package recipes and executable payloads therefore remain trusted inputs even when their downloads are pinned by hash.

Failed directory selection never falls through to a different directory. Download, archive, command and publication errors include the affected package/path and the underlying failure. A package is not reported installed until its payload and receipt have been published.

## On-disk records

| Location | Contents |
| --- | --- |
| `portprog.lock.json` | Schema version 1, operating system, architecture, selected profile and ordered locked packages. Each locked package contains its complete recipe, artifact SHA-256, byte size and Git commit where applicable. |
| `zips/HASH` | Completed artifact whose contents match the filename SHA-256. |
| `installs/NAME` | One published package directory. |
| `installs/NAME/.portprog-receipt.json` | Schema version 1, the locked package and installed-file records. Each file record has a relative path, SHA-256 or link target, and file mode. The receipt does not include itself. |
| `temp` | Retained download/build/extraction staging and failure diagnostics. |
| `backups` | Prior installation trees retained when replacing a package. |
| `retired` | Package trees preserved by removal. |
| `langlibs/gopath` | Consistent Go workspace used by activation. |
| `environment.fish`, `environment.bash`, `environment.bat`, `environment.ps1` | Activation scripts for the corresponding shells. |
| `*.previous-TIMESTAMP` | Previous metadata/activation files retained during replacement. |
| `.portprog-operation.lock` | Reservation for one modifying operation. |

JSON documents are bounded to 32 MiB and decoded strictly. Receipt paths are relative; recorded package ownership must match their containing directory. Lock target platform must match the host. Verification reports missing, changed and unrecorded files with their package identities.

## Activation and offline export

Activation derives the installation root from the script's location, quotes directory names, prepends selected executable directories, avoids adding repeated Portprog PATH entries and preserves the caller's existing PATH. Every shell uses the same Go workspace. Sourcing activation does not start another terminal. Command Prompt activation requires delayed expansion to be disabled and reports this explicitly when necessary.

An offline bundle contains the host executable, no-argument launchers, offline settings, a new lockfile and verified ZIP images of the selected installed packages. `source.lock.json` preserves the original artifact identities. The first bundle launch installs those images locally; it does not rebuild source or contact upstream servers. A bundle is specific to its recorded OS/architecture. The entire bundle directory can be moved; activation is generated at its current location.

Export verifies package receipts and rechecks file content while archiving. A failed export is not published at the requested destination; its staging directory is retained and reported. Existing destinations are never replaced. Bundles of third-party programs with embedded absolute paths retain that upstream limitation.

## Validation limits

Automated checks use local files, real temporary archives, local HTTP/TLS servers and local build tools. CI covers compilation, catalog validation, archive/error cases, lifecycle behavior and native shell activation on Windows, macOS and Linux. It does not install the entire historical software catalog or contact its download servers.

Some historical upstream downloads can disappear or have unusable TLS. Their recipes remain selectable, but resolution fails with the upstream error rather than disabling verification or substituting another artifact. Catalog validity, successful artifact resolution and successful installation are separate outcomes.
