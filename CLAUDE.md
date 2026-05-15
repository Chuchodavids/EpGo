# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build epgo

# Build with version info stripped (production)
go build -ldflags="-s -w" -o epgo

# Cross-compile (used by CI)
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o epgo_linux_amd64

# Tidy dependencies
go mod tidy

# Run (interactive config)
./epgo -configure myconfig.yaml

# Run (fetch EPG data and generate XMLTV)
./epgo -config myconfig.yaml
```

There are no tests in this project.

## Architecture

EPGo fetches EPG data from the [Schedules Direct](https://schedulesdirect.org) API and generates XMLTV files for use with media servers (Plex, Jellyfin, etc.).

### Two execution modes

**`-configure`** → `Configure()` in `configure.go`
Interactive TUI for managing credentials, lineups, and channel selection. Writes the YAML config file and cache. Uses `promptui` for menus.

**`-config`** → `sd.Update()` in `data.go`
Headless pipeline: open cache → login → fetch status → fetch lineup/schedule/program/metadata from SD → write cache → generate XMLTV file.

### The `SD` struct (`struct_sd.go`, `sd.go`)

Central API client. Uses a function-field pattern — `sd.Login`, `sd.Status`, `sd.Lineups`, etc. are closures assigned in `sd.Init()`. All HTTP calls funnel through `sd.Connect()`. The SD API base URL is `https://json.schedulesdirect.org/20141201/`.

Token caching: on login, the token and its expiry timestamp are stored in `Cache.Token` / `Cache.TokenExpires` and persisted to the cache JSON file. On subsequent runs, the cached token is reused if it expires more than 5 minutes from now.

### The `cache` struct (`struct_cache.go`, `cache.go`)

Single JSON file (default: `config_cache.json`) that persists between runs to avoid re-downloading unchanged data. Contains:

- `Channel` — `map[stationID]EPGoCache` with station metadata (name, callsign, logo, LCN)
- `Schedule` — `map[stationID][]EPGoCache` with airtimes
- `Program` — `map[programID]EPGoCache` with program details
- `Metadata` — `map[programID]EPGoCache` with artwork URLs
- `Token` / `TokenExpires` — cached SD auth token

`Cache.Init()` reinitializes only the Channel/Schedule/Program/Metadata maps without touching the token fields.

### Config file (`struct_config.go`, `configure.go`)

YAML file managed by `config.Open()` / `config.Save()`. When opened, the code detects missing option blocks by scanning the raw YAML bytes and injects defaults — this is the config migration mechanism (no version field, purely string-matching based).

The `channel` struct is dual-purpose: serialized to YAML for the config file, and also encoded directly to XML for XMLTV output (via Go struct tags). The `Lcn` field is XML-only (`yaml:"-"`), populated from the SD lineup map's `atscMajor.atscMinor` for OTA or the `channel` string for cable/satellite.

### XMLTV generation (`xmltv.go`)

Streams XML directly to file using `xml.NewEncoder`. Channels are written first (from `Cache.Channel`), then programmes (from `Cache.Schedule` joined with `Cache.Program` and `Cache.Metadata`). Channel IDs follow the format `epgo.<stationID>.schedulesdirect.org`.

The `dd_progid` episode number is formatted as `EP01938443.3118` — the first 10 characters of the program ID followed by a dot and the remaining digits.

### Image server (`server.go`)

Optional HTTP file server started with `-serve dir:port` or automatically after XMLTV generation when `Config.Server.Enable = true`. Serves downloaded artwork from the configured image path.

### Key globals

- `Config` — the parsed YAML config (`config` struct)
- `Cache` — the JSON cache (`cache` struct, includes `sync.RWMutex`)
- `Token` — the current SD auth token string (mirrors `Cache.Token` during a run)
- `logger` — `*slog.Logger` using text handler to stdout

### Branch workflow

Development happens on the `development` branch. Changes are merged to `master` for releases. The `development-release.yaml` CI workflow publishes a rolling pre-release on every push to `development`; `release.yaml` publishes versioned releases on `v*` tags.
