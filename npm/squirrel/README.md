# `@talex-touch/squirrel`

Lightweight launcher for the local Touch Squirrel web manager.

## Run

No installation is required. With no subcommand, Squirrel starts the web manager:

```bash
npx @talex-touch/squirrel
```

Explicit commands remain available:

```bash
npx @talex-touch/squirrel web
npx @talex-touch/squirrel doctor
```

The package exposes the `squirrel` binary. After a global install, use the short commands:

```bash
npm install --global @talex-touch/squirrel
squirrel
squirrel web
squirrel doctor
```

The launcher downloads the matching Go Host binary from the
[`TalexDreamSoul/touch-squirrel`](https://github.com/TalexDreamSoul/touch-squirrel)
GitHub Release, verifies it against `checksums.txt`, caches it, and then starts the Host.
It does not bundle feature plugins. Open the local web manager and install only the
plugins you need from the official GitHub source.

## Environment

| Variable | Purpose |
|---|---|
| `SQUIRREL_BINARY` | Use a local Host binary instead of downloading a release |
| `SQUIRREL_CACHE_DIR` | Override the launcher cache directory |
| `SQUIRREL_HOME` | Override Host data storage |
| `SQUIRREL_OFFICIAL_PLUGIN_REPO` | Override the official plugin repository |
| `PANEL_ADDR` | Override the web manager bind address |
| `PANEL_TOKEN` | Enable web manager authentication |

Node.js 20 or newer is required.
