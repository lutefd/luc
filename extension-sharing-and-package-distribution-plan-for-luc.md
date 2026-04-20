# Extension Sharing and Package Distribution Plan for luc

## Summary

Add a **Go-install-like package distribution layer** on top of the hybrid extension architecture, but do it in **three stages**:

1. **Package format + install/remove/list CLI first**
2. **Git/path/archive sharing second**
3. **Hosted registry/marketplace later**

The package system should be:

- **CLI-first**, not marketplace-first
- **module-path identified**, e.g. `github.com/user/luc-cowboy@v1.2.0`
- **registry-ready**, even before a registry exists
- **self-contained**, with **no package-to-package dependencies in v1**
- installable at **user scope** and **project scope**
- compatible with luc’s existing package asset layering under `.luc/packages/*`

This gives users a real sharing workflow early without forcing registry, signing, moderation, or package graph complexity too soon.

## Goals

- Let users share themes, prompts, hooks, tools, UI manifests, providers, skills, and programmable extensions.
- Keep the sharing workflow simple and reproducible.
- Reuse luc’s current package asset layering model.
- Avoid central marketplace complexity in the first version.
- Make a future registry/marketplace additive, not architecture-breaking.

## Non-Goals

- Full marketplace in v1
- Package dependency resolution in v1
- Multi-package monorepo support in v1
- Signed trust chains in v1
- Remote code sandboxing in v1
- Dynamic package installation during a running turn

## Distribution Model

## 1. Package Identity

Use **module-path identity** from day one.

Examples:

- `github.com/user/luc-cowboy@v1.2.0`
- `codeberg.org/acme/luc-theme-sunrise@v0.4.1`

Rules:

- Identity is stable and globally unique.
- Version is semver.
- `@latest` resolves to the latest semver release.
- Registry support later must preserve the same identity model.

## 2. Package Source Types

v1 install sources:

- local path
- git repo via module-path identity
- explicit git URL
- archive URL

CLI examples:

- `luc pkg install github.com/user/luc-cowboy@v1.2.0`
- `luc pkg install github.com/user/luc-cowboy@latest`
- `luc pkg install git+https://github.com/user/luc-cowboy.git@v1.2.0`
- `luc pkg install https://example.com/luc-cowboy-v1.2.0.tar.gz`
- `luc pkg install ./my-local-package`

## 3. Install Scopes

Support both from the start:

- `--scope user`
- `--scope project`

Default:

- `user` scope by default for explicit CLI installs unless the user passes `--scope project`

Behavior:

- user scope installs under the user runtime
- project scope installs under the workspace runtime
- project scope overrides user scope through the existing runtime layering model

## Package Format

## 1. Package Manifest

Add a new package root manifest:

- `luc.pkg.yaml`

Required fields:

- `schema: luc.pkg/v1`
- `module`
- `version`
- `luc_version`

Optional fields:

- `name`
- `description`
- `license`
- `homepage`
- `repository`
- `keywords`

Example:

```yaml
schema: luc.pkg/v1
module: github.com/user/luc-cowboy
version: v1.2.0
luc_version: ">=0.1.0"
name: luc-cowboy
description: Cowboy-themed runtime bundle for luc.
license: MIT
repository: https://github.com/user/luc-cowboy
```

## 2. Package Layout

One package per repo root in v1.

Allowed top-level directories in a package:

- `tools/`
- `providers/`
- `ui/`
- `hooks/`
- `prompts/`
- `skills/`
- `themes/`
- `extensions/`
- `README.md`

Example:

```text
luc-cowboy/
  luc.pkg.yaml
  README.md
  themes/
    cowboy.yaml
  prompts/
    cowboy-tight-loop.yaml
  ui/
    cowboy.yaml
  extensions/
    cowboy/
      manifest.yaml
      host.py
  tools/
    sheriff_status.yaml
```

Rule:

- package contents are copied or unpacked into luc’s package asset layer as-is
- runtime loading continues to work by scanning package asset directories

## 3. No Dependencies in v1

Packages are self-contained.

Rules:

- no `dependencies` field in `luc.pkg/v1`
- if users want multiple packages, they install them explicitly
- no transitive resolution
- no solver
- no lock graph complexity

This keeps the install model simple and avoids package-manager behavior before the package format is stable.

## Installation Model

## 1. Package Store Layout

User scope install location:

- `~/.luc/packages/<normalized-module>@<version>/`

Project scope install location:

- `<workspace>/.luc/packages/<normalized-module>@<version>/`

Normalization rule:

- preserve human-readable module path as much as possible
- replace path separators only where the filesystem requires it
- package directory name must be deterministic from `module + version`

Examples:

- `~/.luc/packages/github.com_user_luc-cowboy@v1.2.0/`
- `<workspace>/.luc/packages/codeberg.org_acme_luc-theme-sunrise@v0.4.1/`

## 2. Metadata Tracking

Track installs separately from unpacked assets.

User scope metadata file:

- `~/.luc/packages/installed.json`

Project scope metadata file:

- `<workspace>/.luc/packages/installed.json`

Each record includes:

- `module`
- `version`
- `scope`
- `source_type`
- `source`
- `installed_at`
- `package_dir`
- `manifest_digest`

Purpose:

- list/remove/inspect installed packages reliably
- detect drift or manual corruption
- enable future upgrade commands

## 3. Compatibility Check

At install time, luc validates:

- package manifest schema
- semver validity
- `luc_version` compatibility
- top-level directory allowlist
- required files for package-contained assets

If incompatible, install fails before unpacking.

## CLI Surface

## 1. v1 Commands

Implement these first:

- `luc pkg install <source>@<version> [--scope user|project]`
- `luc pkg remove <module> [--scope user|project]`
- `luc pkg list [--scope user|project|all]`
- `luc pkg inspect <module> [--scope user|project|all]`
- `luc pkg pack <path>`
- `luc pkg validate <path>`

## 2. Command Semantics

`install`

- resolves source
- fetches package
- validates manifest and compatibility
- unpacks into scope package store
- updates `installed.json`
- prompts reload if luc is running interactively

`remove`

- removes exact installed package by module within the selected scope
- deletes package dir
- updates `installed.json`

`list`

- shows installed packages by scope
- output includes `module`, `version`, `scope`, `source`

`inspect`

- shows manifest metadata and exported asset categories
- shows installation source and path

`pack`

- validates package dir
- emits a tar.gz archive for sharing or release upload
- archive name defaults to `<package-name>-<version>.tar.gz`

`validate`

- validates a package dir or archive without installing
- useful for package authors and CI

## 3. Commands Deferred to Registry Phase

Do **not** implement in v1:

- `luc pkg search`
- `luc pkg publish`
- `luc pkg upgrade`
- `luc pkg outdated`

These belong to the registry phase.

## Source Resolution Rules

## 1. Local Path

- path points to a package root containing `luc.pkg.yaml`
- version is taken from manifest
- optional `@version` suffix is rejected for local path installs to avoid ambiguity

## 2. Module Path Install

For module-path installs:

- luc resolves the module path to a git repository URL
- package must live at repo root in v1
- exact version installs check out the semver tag
- `@latest` resolves to the highest semver tag
- only immutable tag installs are accepted for module-path installs in v1

Rule:

- installing from bare branch names is not allowed in v1 for module-path installs

## 3. Explicit Git URL

- supports exact tag or commit
- if installed from a non-semver commit, the package still must contain a valid manifest version
- metadata records both manifest version and source revision

## 4. Archive URL

- archive must unpack to a single package root
- package root must contain `luc.pkg.yaml`

## Runtime Integration

## 1. Loader Behavior

No architectural change to existing runtime loading order.

Installed packages are simply additional package asset roots under:

- `~/.luc/packages/*/...`
- `<workspace>/.luc/packages/*/...`

This reuses luc’s current package asset loading behavior.

## 2. Reload Behavior

After successful install/remove:

- interactive luc should show a note: reload required to pick up package changes
- `ctrl+r` or `luc reload` loads new assets
- no live hot-install mutation during a turn in v1

## 3. Package Asset Types Supported

Packages may ship any combination of:

- themes
- prompt extensions
- skills
- tools
- providers
- UI manifests
- hooks
- programmable extension hosts under `extensions/`

The package system is asset-agnostic. It distributes runtime bundles; it does not care which asset categories are present.

## Marketplace / Registry Phase

## 1. Marketplace Timing

Do **not** build a central marketplace first.

Only start registry work after:

- package format is stable
- install/remove/list/pack flow is proven
- direct git/path/archive sharing is working
- package identity and compatibility semantics are validated

## 2. Registry Model

When added later, the registry should be:

- optional
- additive
- identity-compatible with module-path installs
- archive-oriented, not source-control-coupled

The registry should host:

- package metadata index
- immutable package archives
- release metadata per version

The registry should not redefine package identity.

## 3. CLI Additions in Registry Phase

Add later:

- `luc pkg search <query>`
- `luc pkg publish <path>`
- `luc pkg login`
- `luc pkg upgrade`
- `luc pkg outdated`

`publish` belongs here, not in v1. Before registry support, authors share packages through git tags and archives.

## Trust and Security Model

v1 trust model:

- trusted local first
- installing a package is equivalent to trusting its code and scripts
- no signing or verification chain in v1
- install command must show a clear trust warning for packages that include executable assets:
  - tools
  - hooks
  - providers
  - extension hosts

Suggested UX:

- first install from remote source requires confirmation unless `--yes` is passed
- warning text explicitly states that package assets may execute local code with user permissions

## Failure and Edge Cases

- installing a package with incompatible `luc_version` fails
- installing a package with duplicate `module + version` in the same scope is a no-op unless `--force` is added later
- project install of the same module/version as user install is allowed and shadows the user install
- removing a package from project scope does not affect user scope
- malformed package archives fail before unpack
- packages containing unsupported top-level directories fail validation
- package install interrupted mid-way must roll back partial unpack and metadata writes

## Test Cases and Scenarios

- install from local path to user scope
- install from local path to project scope
- install from git module path with exact semver tag
- install from git module path with `@latest`
- install from archive URL
- install fails on missing `luc.pkg.yaml`
- install fails on invalid manifest schema
- install fails on incompatible `luc_version`
- install fails on disallowed top-level package layout
- package contents are loaded through existing runtime package asset scan
- project scope package overrides same user scope package
- remove deletes installed dir and metadata entry correctly
- list shows packages by scope accurately
- inspect shows asset categories accurately
- pack creates deterministic archive contents
- validate works on both directory and archive inputs
- interrupted install rolls back cleanly
- remote executable package install shows trust warning
- non-executable package install still works without extra runtime requirements

## Acceptance Criteria

- A user can create a package containing themes, prompts, tools, hooks, UI manifests, or extension hosts and share it via git or archive.
- Another user can install that package with a single `luc pkg install` command.
- Installed packages load through luc’s existing runtime layering and reload mechanisms.
- Package identity is stable and compatible with a future registry.
- No package dependency solver exists in v1.
- Both user and project install scopes work correctly and predictably.

## Assumptions and Defaults

- Distribution model: CLI-first, not marketplace-first
- Package identity: module-path based
- Package dependencies: none in v1
- Install scopes: user and project
- One package per repo root in v1
- `publish/search/upgrade` are deferred until registry phase
- Registry, signing, and marketplace governance are out of scope for the first implementation
