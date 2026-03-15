# Repository Layout

This document describes what belongs in the repository root, what is considered source
material, and what is treated as a release artifact.

## Top-Level Structure

### `cmd/`

Entry points for compiled binaries.

- `cmd/vpngw/` contains the CLI application main package.

### `internal/`

Application source code.

- `internal/app/` contains process startup and runtime orchestration.
- `internal/bootstrap/` contains bootstrap/install generation logic.
- `internal/config/` contains config structures, defaults, and validation.
- `internal/wg/` contains WireGuard-specific operations such as client generation and peer
  synchronization.

### `deploy/`

Source templates used to build installation bundles.

- `deploy/config.example.json` is the source example configuration.
- `deploy/wireguard/` contains source WireGuard templates.
- `deploy/scripts/` contains source install/bootstrap scripts.
- `deploy/systemd/` contains systemd unit templates.

Files in `deploy/` are editable source assets. If they change, matching release files in
`dist/` should be refreshed intentionally.

### `dist/`

Release-oriented output that should remain in the repository.

- `dist/vpngw-linux-amd64/` is the full Linux bundle.
- `dist/vpngw-linux-amd64-single/` is the single-binary distribution.
- `dist/*.zip` and `dist/*.tar.gz` are packaged release archives.
- helper scripts in `dist/` are operational artifacts for already deployed hosts.

`dist/` is the canonical place for packaged deliverables in this project. It is preserved
even when cleaning the repository.

### `docs/`

Long-form documentation.

- architecture and control flow
- configuration reference
- privacy and network behavior
- operations and maintenance
- repository layout and cleanup rules

### Root files

- `README.md` is the short index and quick-start.
- `go.mod` and `go.sum` define the Go module.
- `.gitignore` prevents disposable local artifacts from polluting the root.

## What Should Not Live In The Root

The repository root should stay limited to source, documentation, and module metadata.

The following are considered disposable clutter and should not be kept there:

- downloaded Go SDK archives such as `go.zip`
- local one-off binaries such as `vpngw` or `vpngw.exe`
- ad-hoc test outputs that are not part of `dist/`

## Cleanup Policy

When cleaning the project:

1. Keep `dist/` and everything inside it.
2. Keep source trees: `cmd/`, `internal/`, `deploy/`, and `docs/`.
3. Remove disposable archives or binaries from the repository root.
4. Do not store secrets, real uplink peer credentials, or machine-specific temporary files
   in source templates.

## Practical Workflow

Use these rules to avoid reintroducing clutter:

- for a verification build, prefer `go build ./...`
- if a one-off binary is needed, place it outside the repository root or package it
  deliberately into `dist/`
- edit source templates in `deploy/`, not inside `dist/`, unless you are intentionally
  updating the staged release bundle as well

## Notes About Local Metadata

Local IDE metadata such as `.idea/` is not runtime data for `vpngw`, but it may still be
useful for the working environment. It is separate from build clutter and should be
handled intentionally rather than deleted blindly.
