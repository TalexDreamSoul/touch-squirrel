# `@talex-touch/squirrel`

Lightweight npm launcher for the local Touch Squirrel web manager and plugin Host.

Node.js 20 or newer is required. Supported release targets are macOS arm64/amd64,
Linux arm64/amd64, and Windows amd64.

## Quick Start

Run the environment diagnostics first:

```bash
npx --yes @talex-touch/squirrel@latest doctor
```

Start the local web manager:

```bash
npx --yes @talex-touch/squirrel@latest
```

Open <http://127.0.0.1:8787>. Running without a subcommand is equivalent to `web`:

```bash
npx --yes @talex-touch/squirrel@latest web
```

To reproduce this release, replace `latest` with `0.2.4`.

## Install Plugins

A fresh Host contains no feature plugins. It starts with only the web manager,
plugin runtime, tasks, artifacts, and storage.

1. Open **Plugins** in the web manager.
2. Open **Plugin Market** and sync the official `TalexDreamSoul/touch-squirrel` source.
3. Install and enable only the registrar, pool, or Bridge plugins you need.

Installed plugins are stored under `~/.touch-squirrel/plugins/`. They can be updated
independently from this npm launcher and the Host binary. Plugins are executable code;
only add repositories you trust.

## Global Install

The package exposes the `squirrel` binary:

```bash
npm install --global @talex-touch/squirrel
squirrel doctor
squirrel
```

Update a global installation with:

```bash
npm update --global @talex-touch/squirrel
```

## How It Works

The launcher downloads the matching Go Host from the versioned
[`TalexDreamSoul/touch-squirrel`](https://github.com/TalexDreamSoul/touch-squirrel/releases)
GitHub Release, verifies it against `checksums.txt`, and caches it. Later runs reuse the
verified binary instead of downloading it again.

## Environment

| Variable | Purpose | Default |
|---|---|---|
| `SQUIRREL_BINARY` | Use a local Host binary instead of downloading | Unset |
| `SQUIRREL_CACHE_DIR` | Override the launcher binary cache | npm user cache |
| `SQUIRREL_HOME` | Override Host data, plugins, and artifacts | `~/.touch-squirrel` |
| `SQUIRREL_OFFICIAL_PLUGIN_REPO` | Override the official plugin repository | Official GitHub repo |
| `SQUIRREL_DOWNLOAD_TIMEOUT_MS` | Override the GitHub Release timeout in milliseconds | `900000` |
| `PANEL_ADDR` | Override the web manager bind address | `127.0.0.1:8787` |
| `PANEL_TOKEN` | Enable web manager token authentication | Unset |

macOS and Linux example:

```bash
PANEL_ADDR=127.0.0.1:9797 SQUIRREL_HOME="$HOME/.touch-squirrel-dev" \
  npx --yes @talex-touch/squirrel@latest
```

Windows PowerShell example:

```powershell
$env:PANEL_ADDR = "127.0.0.1:9797"
$env:SQUIRREL_HOME = "$HOME/.touch-squirrel-dev"
npx --yes @talex-touch/squirrel@latest
```
