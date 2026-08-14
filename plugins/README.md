# 官方插件

此目录是 `https://github.com/TalexDreamSoul/touch-squirrel` 的官方插件源。

最终用户通过 `npx @talex-touch/squirrel` 启动纯 Host。Host 不从当前工作目录加载这些源码插件；打开「插件 → 插件市场」，同步官方仓库后，只安装实际需要的功能。

## 安装流程

```text
squirrel / squirrel web
  → 本地 Web Host
  → 同步 GitHub 官方源
  → 读取 plugins/*/plugin.json
  → 用户选择插件
  → 安装到 ~/.touch-squirrel/plugins/<id>/<version>/
  → 记录官方来源并启用
```

## 插件目录

| ID | Runtime | 状态 | 说明 |
|---|---|---|---|
| `xai-accounts` | Hybrid | 可用 | xAI 注册、OAuth 与 CPA 产物 |
| `chatgpt-registrar` | Bridge | 可用 | reg-factory ChatGPT 注册流程 |
| `claude-registrar` | Bridge | 可用 | reg-factory Claude 注册流程 |
| `github-registrar` | Bridge | 可用 | GitHub + Outlook 注册流程 |
| `grok-http-registrar` | Bridge | 可用 | reg-factory Grok HTTP 流程 |
| `grok-panel-registrar` | Bridge | 可用 | grok-register-panel Bridge |
| `outlook-registrar` | Bridge | 可用 | Outlook 注册流程 |
| `tavily-pool` | Go | 可用 | Tavily Key Pool 与 HTTP/MCP Proxy |
| `tavily-registrar` | Go | Shell | Tavily 注册器壳层 |

Bridge 插件只包含 Host Adapter，仍需在设置中配置对应 Python Runtime、上游项目目录和外部依赖。安装插件不会自动安装或信任第三方浏览器、代理和账号服务。

## 目录契约

每个插件目录至少包含：

```text
plugins/<id>/
├── plugin.json
└── <entry file declared by plugin.json>
```

`plugin.json` 必须符合 [Plugin IDL](../docs/contracts/plugin-idl.md)。官方身份由 Host 根据仓库来源写入，插件不能在 Manifest 中自行声明 `official`。
