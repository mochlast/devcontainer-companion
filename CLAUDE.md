# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Devcontainer Companion (`dcc`) is an interactive CLI tool written in Go that helps developers create and configure devcontainers through a persistent TUI hub/dashboard. It scrapes the [containers.dev](https://containers.dev) catalogs, fetches OCI metadata from ghcr.io, and generates `.devcontainer/devcontainer.json`.

## Build Commands

- `make build` — Compile the `dcc` binary (embeds git version via ldflags)
- `make test` — Run all tests (`go test ./...`)
- `make lint` — Static analysis (`go vet ./...`)
- `make install` — Build and install to `$GOPATH/bin/`
- `make clean` — Remove compiled binary

## Architecture

### Entry Point

`cmd/root.go` — bare `dcc` (no subcommand) opens the hub. Also supports `dcc init` and `dcc -w <folder>`. The root command resolves the workspace folder, ensures `.devcontainer/` exists, and calls `runHub()`.

### Hub Loop (`cmd/hub.go`)

The hub uses an exit-and-re-enter pattern. Each iteration re-reads config from disk so the preview always reflects the latest state:

```
for {
    config = readConfig()
    action, dirty, preloaded = ShowHub(config, callbacks)
    switch action:
        template/features → sub-flow with preloaded catalog data
        extensions/plugins/settings → sub-flow (no preloading)
        build/open → handled async within the hub TUI
        exit → return
}
```

The `Preload` callback loads catalog data (templates/features) while still inside the hub's AltScreen, avoiding a visible screen flash between programs. The preloaded data is passed to the sub-flow.

### Sub-Flow Pattern (`cmd/hub.go`)

Each sub-flow follows the same structure:
1. Use preloaded catalog data (or fetch if not available)
2. Open a picker (own `tea.NewProgram` with AltScreen)
3. For items with options: call `ShowHubForm(ctx, FormConfig{...})` which runs load → form → post-action in a single `tea.NewProgram`
4. Write results to disk, return to hub

### Key Packages

**`cmd/`** — Cobra CLI setup (`root.go`), hub loop + sub-flows (`hub.go`), shell command helpers (`helpers.go`).

**`internal/ui/`** — All TUI components, built with Bubble Tea + Lipgloss + Huh:
- `hub.go` — Split-pane dashboard: left menu + right config preview (colorized JSON). Build/Open run async inside the hub via `HubCallbacks`. `HubContext` carries shared state across phases.
- `hub_phases.go` — `ShowHubForm` with `FormConfig` state machine (loading → huh form → post-action) in a single AltScreen program. Builds huh fields dynamically from `registry.OptionDefinition` maps.
- `filter.go` — Custom `list.FilterFunc` that wraps `list.DefaultFilter` and re-sorts results so official devcontainers entries (`ghcr.io/devcontainers/`) appear first.
- `template_picker.go` — Fuzzy-search list for templates with `?` README preview.
- `feature_picker.go` — Multi-select fuzzy-search list for features with `?` README preview. Pre-selected items pinned to top.
- `extension_picker.go` — VS Code Marketplace search with async results.
- `plugin_picker.go` — JetBrains Plugin Repository search with async results.
- `customizations.go` — Settings submenu: list of editable fields → individual huh forms per field. Handles strings, bools, selects, CSV, env vars, ports.
- `readme_preview.go` — Fetches and renders README markdown in a viewport.
- `option_form.go` — `defaultToString` helper for converting option defaults.

**`internal/catalog/`** — Scrapes template/feature catalogs from containers.dev HTML pages. File-based cache (`~/.cache/dcc/`, 1-hour TTL) with fallback to expired cache on network errors. `sortOfficialFirst` puts `ghcr.io/devcontainers/` entries at the top.

**`internal/registry/`** — OCI registry client: bearer token auth flow, manifest/blob fetching, tar/gzip layer extraction. `FetchItemMetadata(ociRef)` returns `(*TemplateDefinition, *FeatureDefinition, error)` with option definitions parsed from `devcontainer-template.json` / `devcontainer-feature.json`.

**`internal/template/`** — `Apply()` shells out to `devcontainer templates apply` (uses `cmd.Dir` instead of `-w` flag to work around VS Code CLI bug). `CreateEmpty()` generates minimal Ubuntu-based config. `DetectCLI()` probes for devcontainer CLI capabilities (installed, has `open` subcommand).

**`internal/feature/`** — `ReplaceAll()` replaces the `features` section in devcontainer.json with configured features and their options.

**`internal/devcontainer/`** — `ReadConfig()` / `WriteConfig()` for devcontainer.json with JSONC comment stripping (via `tidwall/jsonc`). `Exists()` checks for `.devcontainer/` directory.

**`internal/marketplace/`** — HTTP clients for VS Code Marketplace API and JetBrains Plugin Repository API. Returns extension/plugin metadata for the picker.

### Key Design Patterns

- **`map[string]any` for config** — devcontainer.json is always manipulated as `map[string]any` to preserve unknown fields during read-modify-write cycles.
- **Catalog → OCI → Metadata** — `CatalogEntry.OciRef` → `registry.FetchItemMetadata()` → `OptionDefinition` map → dynamically built huh form.
- **`FormConfig` state machine** — Combines async loading, form display, and post-action into one `tea.NewProgram`, reducing AltScreen transitions from ~12 to ~6 per flow.
- **`HubCallbacks`** — Build/Open/Preload functions passed from `cmd/` to `ui/`, keeping the UI package free of direct shell dependencies.
- **Dirty flag** — Tracks config changes since last successful build; when dirty, build uses `--no-cache` for a full rebuild.
- **Official-first sorting** — `catalog.IsOfficial()` used in both initial list order (`cache.go`) and fuzzy-filter results (`filter.go`).
