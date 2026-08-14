# touch-squirrel Plugin IDL (v0)

Host 与插件只通过本契约通信。语言（Go / JS）是实现细节。

## 1. Manifest — `plugin.json`

```json
{
  "id": "xai-accounts",
  "name": "xAI Accounts",
  "version": "0.1.0",
  "description": "Register and OAuth xAI / Grok accounts",
  "kind": ["registrar"],
  "runtime": "go",
  "entry": {
    "go": "bin/plugin",
    "js": "dist/index.js"
  },
  "hostApi": "0.1",
  "capabilities": ["email", "turnstile", "browser", "http", "proxy", "storage", "secrets", "events"],
  "artifactKinds": ["account.xai", "session.sso", "oauth.token", "cpa.json"],
  "configSchema": {
    "type": "object",
    "additionalProperties": true
  },
  "ui": {
    "panels": ["run-console", "settings"]
  }
}
```

| field | required | notes |
|-------|----------|-------|
| `id` | yes | letters, numbers, dot, underscore, or hyphen; unique |
| `version` | yes | semver-compatible filesystem-safe value |
| `kind` | yes | `registrar` \| `pool-proxy` \| `exporter` \| `capability` |
| `runtime` | yes | `go` \| `js` \| `hybrid` \| `bridge` |
| `entry` | yes | at least one of `go` / `js` / `bridge`; relative path inside the plugin directory |
| `hostApi` | yes | must match the host contract version |
| `capabilities` | no | declared host capabilities |
| `artifactKinds` | no | produced artifact kinds |
| `configSchema` | no | JSON Schema for plugin config |

## 2. Kind

| kind | role |
|------|------|
| `registrar` | open accounts / sessions / tokens |
| `pool-proxy` | key pool, rotation, proxy façade |
| `exporter` | push artifacts to external systems |
| `capability` | reusable provider (email, captcha, …) |

## 3. Job

Host creates and schedules jobs. Plugins execute one unit of work via runner.

```json
{
  "id": "job_01H…",
  "plugin": "xai-accounts",
  "kind": "registrar",
  "target": 10,
  "config": {},
  "status": "queued|running|completed|failed|cancelled",
  "createdAt": "RFC3339",
  "startedAt": null,
  "finishedAt": null,
  "progress": { "done": 0, "failed": 0, "total": 10 },
  "labels": {}
}
```

## 4. Artifact (产物)

```json
{
  "id": "art_…",
  "plugin": "xai-accounts",
  "kind": "oauth.token",
  "status": "fresh|healthy|limited|dead",
  "labels": { "email": "a@b.c" },
  "secretRefs": ["sec_…"],
  "payload": {},
  "createdAt": "RFC3339",
  "updatedAt": "RFC3339"
}
```

- 明文密钥不进 `payload` 日志；敏感字段走 `secretRefs` / secrets store。
- Host 号池视图只认 Artifact，不认插件私有文件布局。

## 5. Capabilities (Host → Plugin)

| name | purpose |
|------|---------|
| `email` | create mailbox, poll code |
| `turnstile` | mint challenge token |
| `browser` | restricted automation session |
| `http` | outbound HTTP with proxy/jar |
| `proxy` | egress assignment |
| `storage` | plugin-private KV + run dirs |
| `secrets` | encrypted secret I/O |
| `events` | logs / progress / SSE |
| `quota` | concurrency + daily caps |

插件不得直接读全局任意 env 起不受管子进程；须走声明的 capability。

## 6. Runner

| runtime | host behavior |
|---------|----------------|
| `go` | exec `entry.go` or in-process adapter registered by id |
| `js` | load ESM `entry.js` via js-runner |
| `hybrid` | Go body for work; JS for schema/UI |

## 7. Install layout

```text
~/.touch-squirrel/plugins/<id>/<version>/
  plugin.json
  bin/… or dist/…
  ui/…
```

`enabled.json`:

```json
{
  "plugins": {
    "xai-accounts": { "version": "0.1.0", "enabled": true }
  }
}
```

## 8. Trust model (v0)

- Local path installs are explicitly trusted by the local operator.
- GitHub market sources only accept `https://github.com/{owner}/{repo}`. The host builds the codeload URL and rejects unsafe archive paths, links, oversized archives, duplicate plugin IDs, invalid manifests, and incompatible `hostApi` values.
- Repository provenance is host-owned metadata. A plugin cannot mark itself as official in `plugin.json`; only the immutable official repository receives that flag.
- Plugins remain executable third-party code. v0 does not provide signatures or a runtime sandbox, so repository trust is still required.

## 9. First-party plugins

| id | kind | runtime | notes |
|----|------|---------|-------|
| `xai-accounts` | registrar | hybrid/go | from current Grok pipeline |
| `tavily-pool` | pool-proxy | go | behavior ref: tavily-hikari |
| `tavily-registrar` | registrar | go\|js | shell first |

## 10. CLI surface (v0)

```bash
squirrel plugin list
squirrel plugin install <path|tgz|url>
squirrel plugin enable <id>
squirrel plugin disable <id>
squirrel plugin show <id>
squirrel run <id> [--target N]
squirrel panel
```

Legacy `grok` remains available during migration.
