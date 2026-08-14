# touch-squirrel Architecture (v0)

产品名：**touch-squirrel**（松鼠 / 囤囤鼠）
定位：可插拔注册与号池运行时。本体不绑 Grok / Tavily。

## Brand

| item | value |
|------|-------|
| product | touch-squirrel |
| CLI | `squirrel` (`grok` alias kept) |
| home | `~/.touch-squirrel` (`GROK_HOME` / existing `~/.grok` still work) |
| metaphor | 囤账号、密钥、池、产物 |

## Host vs plugins

- **Host**: plugin manager, jobs, capabilities, artifacts, panel shell, CLI
- **Plugins**: business flows packaged with `plugin.json`
- **Runtimes**: `go` | `js` | `hybrid` (official heavy plugins may keep Go body)

## First-party plugins

| id | kind | status |
|----|------|--------|
| `xai-accounts` | registrar | hybrid; `run` → existing pipeline |
| `tavily-pool` | pool-proxy | manifest only; behavior ref [tavily-hikari](https://github.com/IvanLi-CN/tavily-hikari) |
| `tavily-registrar` | registrar | shell |

## Layout in this repo

```text
cmd/grok/                     # binary entry (built as squirrel + grok link)
internal/plugin/              # manifest + manager
internal/home/                # SQUIRREL_HOME + legacy GROK_HOME
plugins/<id>/plugin.json      # in-tree first-party packs
docs/contracts/plugin-idl.md  # IDL
```

## Trust (v0)

Local arbitrary packages allowed (`plugin install <dir>`). No signature required yet.

## Next

1. Wire artifact store writes from xai-accounts pipeline
2. tavily-pool MVP (key pool + HTTP façade, hikari-aligned subset)
3. Panel plugin-aware routes
4. tgz/url install

## Surfaces (current)

### CLI
- `squirrel plugin list|show|install|enable|disable`
- `squirrel run xai-accounts|tavily-pool`
- `squirrel pool keys …` / `squirrel pool serve`
- `squirrel artifacts list`
- `squirrel panel`

### tavily-pool HTTP (pool serve)
- `GET /health` — includes `surfaces: ["http","mcp"]`
- `GET|POST /api/keys`
- `POST /api/tavily/{search,extract,crawl,map,research}`
- `POST|GET|DELETE /mcp` — pool-key injected MCP proxy to `https://mcp.tavily.com/mcp`

### Panel API (host)
- `GET /api/plugins`
- `POST /api/plugins/{id}/enable|disable`
- `GET /api/artifacts`
- `GET|POST /api/tavily/keys`
- `POST /api/tavily/keys/{id}/status`

### Panel UI tabs
插件 · 注册流水线 · 产物仓库 · Tavily 池 · 凭证上传 · 批量导出 · CPA 号池 · 设置
