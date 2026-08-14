# reg-factory 融入 touch-squirrel 架构方案

> 日期：2026-07-27 · 版本：v0.1 · 状态：方案评审

## 1. 目标

将 [tiantianGPU/reg-factory](https://github.com/tiantianGPU/reg-factory) 的平台注册能力融入 touch-squirrel 插件体系，使 Host 统一调度多平台注册流水线，产物统一进入 artifact store → acctpool → CPA/上传。

首批平台：**GitHub 注册**（`register_github.py`）。

## 2. 现状对照

### touch-squirrel 侧

| 能力 | 当前状态 |
|------|---------|
| 插件系统 | manifest 发现 + install/enable/disable 已落地；`run` 调度只有 `xai-accounts` 硬编码 |
| 注册流水线 | 仅 xAI (Grok) Go 原生流水线；`/api/start {plugin}` 硬拒非 xai |
| Job 框架 | `internal/jobs/jobs.go` 已实现（队列/SSE/日志/剪枝），但仅用于 transfer (upload/export) |
| Artifact Store | `internal/artifact/artifact.go` 功能完备（JSON 文件 store），xAI 流水线 **未接入** |
| Account Pool | `internal/acctpool/store.go` SQLite 统一号池，已预留 `Plugin`/`Type` 字段，xAI/Tavily 兼容 |

### reg-factory 侧

| 能力 | 当前状态 |
|------|---------|
| GitHub 注册 | `register_github.py`：BitBrowser + Playwright → GitHub signup → Arkose FunCaptcha → Outlook 取 launch code → cookie 保存 |
| 邮件池 | `_outlook_pool/*.json`（email/password/cookies），`common/mailbox.py` 浏览器登录 Outlook 取信 |
| 打码 | YesCaptcha/CapSolver/EZ-Captcha FunCaptcha 解 Arkose，`common/agent_captcha.py` 视觉投票求解器 |
| 指纹浏览器 | BitBrowser/AdsPower 本地 API (`bitbrowser.py` / `adspower.py`) |
| Session 导出 | `common/cookies.py` `save_platform_cookies` 落 cookie JSON |
| 后端语言 | Python 3.10+，依赖：playwright, requests, curl_cffi |

## 3. 总体策略

> **桥接，不搬仓。** reg-factory 保持独立 Python 运行时；Host 通过子进程调度。

```
┌─────────────────────────────────────────────────────┐
│                   Host (Go)                          │
│  ┌─────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │ Plugin Mgr  │  │  Job Manager │  │  Artifact   │  │
│  │ (manifest)  │  │  (SSE/logs)  │  │  Store      │  │
│  └──────┬──────┘  └──────┬───────┘  └─────┬──────┘  │
│         │                │                │          │
│  ┌──────┴────────────────┴────────────────┴──────┐  │
│  │           Plugin Runner (Go adapter)          │  │
│  │  • 启动 Python 子进程                          │  │
│  │  • stdout JSON 行 → progress/logs→Job.AddLog │  │
│  │  • 产物文件 → Artifact Store                  │  │
│  └────────────────────┬──────────────────────────┘  │
│                       │ os/exec                     │
└───────────────────────┼─────────────────────────────┘
                        ▼
┌─────────────────────────────────────────────────────┐
│            reg-factory venv (Python)                 │
│  ┌───────────────────────┐  ┌─────────────────────┐ │
│  │ runner.py (bridge)    │  │ register_github.py  │ │
│  │  • 解析 job config    │─▶│ + common/*          │ │
│  │  • 取邮箱 pool        │  │ + config.py         │ │
│  │  • 写进度 stdout       │  └─────────────────────┘ │
│  │  • 落产物文件         │                          │
│  └───────────────────────┘                          │
└─────────────────────────────────────────────────────┘
```

### 原则

1. **Go 管命，Python 管活。** Job 生命周期、取消、超时、重试 100% 在 Host 侧；Python 只做浏览器自动化。
2. **stdio 是合同。** Python 子进程 stdout 输出 NDJSON 行：`{"type":"progress","done":1,"total":5}` / `{"type":"artifact","path":"..."}` / `{"type":"error","msg":"..."}`。
3. **不重写 reg-factory。** 用 import 方式调用其现有函数，只加一个轻量 `runner.py` 做 Host 适配。
4. **env vars 复用。** 打码 Key、BitBrowser API 地址、Outlook pool 路径走 .env → Host 透传给子进程环境。

## 4. 架构设计

### 4.1 插件 Manifest

```json
{
  "id": "github-registrar",
  "name": "GitHub 账号注册",
  "version": "0.1.0",
  "description": "自动化注册 GitHub 账号（BitBrowser + Playwright）",
  "kind": ["registrar"],
  "runtime": "bridge",
  "entry": {
    "bridge": "runner.py"
  },
  "hostApi": "0.1",
  "capabilities": [
    "email", "browser", "http", "proxy", "storage", "secrets", "events"
  ],
  "artifactKinds": [
    "account.github",
    "session.cookie"
  ],
  "configSchema": {
    "type": "object",
    "properties": {
      "target": { "type": "integer", "minimum": 1, "maximum": 100, "default": 5 },
      "auto": { "type": "boolean", "default": true },
      "yescaptchaKey": { "type": "string", "description": "留空走环境变量" },
      "capsolverKey": { "type": "string", "description": "留空走环境变量" },
      "keepWindows": { "type": "boolean", "default": false, "description": "保留 BitBrowser 窗口供人工排查" }
    }
  },
  "ui": {
    "panels": ["run-console", "settings"]
  },
  "source": "in-tree"
}
```

### 4.2 Runner 桥接协议

Python 子进程 stdin 收到一行 JSON 作为 job config：

```json
{
  "jobId": "job_git_01H…",
  "target": 5,
  "config": {
    "auto": true,
    "keepWindows": false
  },
  "env": {
    "OUTLOOK_POOL_DIR": "/path/to/_outlook_pool",
    "YESCAPTCHA_API_KEY": "…",
    "CAPSOLVER_API_KEY": "…",
    "BITBROWSER_API": "http://127.0.0.1:54345",
    "CLASH_PROXY": "http://127.0.0.1:7897"
  },
  "outputDir": "/path/to/outputs/job_git_01H…"
}
```

stdout NDJSON 行（每行一个事件）：

```json
{"type":"progress","done":1,"total":5,"email":"a@outlook.com","username":"coolfox1234"}
{"type":"log","msg":"[2] fill single-page form"}
{"type":"captcha","status":"solving","platform":"yescaptcha","taskId":"…"}
{"type":"artifact","kind":"session.cookie","file":"outputs/…/coolfox1234.json","email":"a@outlook.com","username":"coolfox1234"}
{"type":"done","ok":3,"fail":2,"total":5}
{"type":"error","attempt":2,"msg":"GOTO failed after retries","email":"b@outlook.com"}
```

**退出码约定：**
- `0` = 全部完成（可能有部分失败，看 `done` 事件的 fail 数）
- `1` = 全部失败
- `2` = 基础设施不可用（无 BitBrowser/Outlook pool 空等）
- `130` = 被 SIGTERM（Host 取消）

### 4.3 Go 侧 Host 改动

#### 4.3.1 Plugin Runner 注册

在 `cmd/grok/main.go` 的 `cmdRun` switch 中新增 branch：

```go
// 在 /Users/talexdreamsoul/Workspace/Projects/touch-xai-register/cmd/grok/main.go:754 附近
case "github-registrar":
    return runBridgePlugin("github-registrar", rest)
```

`runBridgePlugin` 是新的通用调度器：
1. 解析 manifest → 取 `entry.bridge`（Python 脚本路径）
2. 找 venv Python（`GROK_PYTHON` 或项目 `venv/bin/python`）
3. 构造 job → `jobs.Manager.Add(j)`
4. `os/exec` 启动 Python，捕获 stdout/stderr
5. 每行 JSON → `job.AddLog` / `job.Broadcast`
6. 产物文件 → `artifact.Store.Put`
7. PID 注册 to daemon
8. job 完成时触发可选 CPA 上传

#### 4.3.2 `/api/start` 解禁

在 `internal/api/server.go:482-489` 移除硬编码的 xai-accounts 禁令，改为：

```go
// 检查插件是否 enabled + kind=registrar
mgr := s.pluginManager()
it, err := mgr.Get(pluginID)
if err != nil || !it.Enabled || !it.Manifest.HasKind(plugin.KindRegistrar) {
    writeJSON(w, 400, map[string]any{"error": "插件未启用或不是 registrar"})
    return
}
// 分发到对应 runner
switch it.Manifest.Runtime {
case "bridge":
    return s.runBridgePlugin(it, target)
case "go", "hybrid":
    // 现有流水线
default:
    writeJSON(w, 400, map[string]any{"error": "插件 runtime 未支持"})
}
```

#### 4.3.3 新增文件

| 文件 | 职责 |
|------|------|
| `internal/bridge/runner.go` | 桥接 runner：spawn Python、协议解析、job 管理 |
| `internal/bridge/protocol.go` | NDJSON 事件类型定义与解析 |
| `plugins/github-registrar/plugin.json` | Manifest |
| `plugins/github-registrar/runner.py` | Python 桥接入口 |
| `plugins/github-registrar/ui/index.js` | 面板 schema/UI 验证 |

### 4.4 Python 侧 runner.py

```python
#!/usr/bin/env python3
"""github-registrar bridge runner for touch-squirrel host."""

import json
import os
import sys
import asyncio
from pathlib import Path

# 动态加 reg-factory common/ 到 sys.path
REG_FACTORY_ROOT = os.environ.get("REG_FACTORY_ROOT",
    str(Path(__file__).resolve().parents[1] / "reg-factory"))
sys.path.insert(0, REG_FACTORY_ROOT)
sys.path.insert(0, os.path.join(REG_FACTORY_ROOT, "common"))

from register_github import register_one, load_pool_accounts

def emit(typ: str, **kwargs):
    print(json.dumps({"type": typ, **kwargs}), flush=True)

def main():
    config = json.loads(sys.stdin.readline().strip())
    target = config.get("target", 5)
    auto = config.get("config", {}).get("auto", True)
    keep = config.get("config", {}).get("keepWindows", False)
    output_dir = Path(config.get("outputDir", "."))
    output_dir.mkdir(parents=True, exist_ok=True)

    # 加载邮箱池
    accounts = load_pool_accounts()
    if not accounts:
        emit("error", msg="邮箱池为空")
        sys.exit(2)

    ok = 0
    fail = 0
    async def run():
        nonlocal ok, fail
        async with async_playwright() as p:
            for i in range(min(target, len(accounts))):
                email, password, cookies = accounts[i]
                emit("log", msg=f"#{i+1}/{target} email={email}")
                try:
                    result = await register_one(
                        email, password, cookies, p,
                        auto=auto, keep=keep,
                    )
                    if result startswith "CAPTCHA":
                        emit("log", msg=f"  captcha not solved: {result}")
                        fail += 1
                    elif result:
                        # 写产物文件
                        artifact_path = output_dir / f"{result.get('username','account')}.json"
                        artifact_path.write_text(json.dumps(result))
                        emit("artifact", kind="session.cookie",
                             file=str(artifact_path),
                             email=email,
                             username=result.get("username", ""))
                        ok += 1
                    else:
                        fail += 1
                except Exception as e:
                    emit("error", attempt=i+1, msg=str(e)[:200], email=email)
                    fail += 1
                emit("progress", done=i+1, total=min(target, len(accounts)))

    asyncio.run(run())
    emit("done", ok=ok, fail=fail, total=min(target, len(accounts)))
    sys.exit(0 if ok > 0 else 1)

if __name__ == "__main__":
    main()
```

### 4.5 面板 UI

注册页 `panel/src/app/register/page.tsx` 已有的动态 registrar 选择器直接生效：

- `GET /api/plugins` 自动返回 `github-registrar`（kind=registrar）
- 用户在下拉框选「GitHub 账号注册（github-registrar）」
- 配置表单由 `configSchema` 动态生成（target / keepWindows 开关）
- 点「开始」→ POST `/api/start {plugin:"github-registrar", target:5}` → SSE 进度

**新增 settings 配置：**
- 打码 Key（YesCaptcha / CapSolver）
- BitBrowser API 地址
- Outlook pool 路径
- reg-factory venv Python 路径

### 4.6 产物入库路径

```
注册成功
  → cookies/github/<username>.json (user_session, _gh_sess)
  → Artifact Store: {plugin:"github-registrar", kind:"session.cookie", labels:{email,username}}
  → acctpool: {type:"github", plugin:"github-registrar", status:"验证中"}
  → 可选 CPA 上传 (暂不接，GitHub 没有标准 OAuth/API key)
```

## 5. 分阶段计划

### Phase A: 基础设施（本次）

| 任务 | 文件 | 工作量 |
|------|------|--------|
| plugin.json manifest | `plugins/github-registrar/plugin.json` | 小 |
| runner.py 桥接 | `plugins/github-registrar/runner.py` | 中 |
| Go bridge runner | `internal/bridge/runner.go` + `protocol.go` | 中 |
| cmdRun 解禁 | `cmd/grok/main.go` 展开 switch | 小 |
| /api/start 解禁 | `internal/api/server.go` 去硬编码 | 小 |
| panel 配置页 | `panel/src/app/settings/page.tsx` 加打码/路径 | 小 |
| artifact 入库 | runner 产物 → `Artifact.PutJSON` | 小 |
| venv 准备脚本 | `scripts/setup-regfactory-venv.sh` | 小 |

### Phase B: 验证

| 任务 | 说明 |
|------|------|
| 冒烟 | `squirrel run github-registrar -t 1` 跑通一个 |
| Panel 端到端 | Web UI 启动 → 实时日志 → artifact 列表 |
| 错误恢复 | 验证异常/网络断/没有 Outlook 号时正确报错 |

### Phase C: 多平台扩展（后续）

| 平台 | 基 | 挑战 |
|------|------|------|
| Grok (HTTP) | `register_grok_http.py` | 已有 Go 托管 OAuth，需要对齐 acctpool |
| ChatGPT | `register_chatgpt.py` + `oauth_codex.py` | 需要接 Codex/SUB2API 授权 |
| Claude | `register.py` | hCaptcha + sessionKey |
| Outlook 自注册 | `outlook_reg_loop.py` | 可作为独立 email capability |

### Phase D: 多平台池化

- Outlook 邮箱池变成 Host 级 capability（注册任何平台都可以消费）
- 多平台号池统一展示在 `/pool` 页
- 联邦模式下主节点可分享任意平台号池

## 6. 风险与决策

| 风险 | 决策 | 理由 |
|------|------|------|
| Python 子进程管理 | Go `os/exec` + context cancel → SIGTERM | 简单，不引入额外 IPC 中间件 |
| BitBrowser 单例 | 串行注册（一次一个窗口），不并发 | GitHub 风控严格，并发=高风险 |
| Arkose FunCaptcha 成功率 | 打码平台兜底 + 视觉投票求解器 SKIP_VARIANT 重试（已实现） | register_github.py 内置重试/跳过逻辑 |
| Outlook 取信稳定性 | 浏览器登录 Outlook（`get_code_outlook_pw`），带重试 | 现有成熟路径 |
| reg-factory 路径耦合 | `REG_FACTORY_ROOT` env 指向 reg-factory 目录，不搬代码 | 保持双方独立演进 |
| 多 Python 版本 | 使用项目内 venv（`.venv/`），不依赖系统 Python | 可复现 |

## 7. 环境依赖矩阵（GitHub 平台）

### Host 侧（Go，不动）
- Go 1.21+ 编译 squirrel
- 清障栈（WARP+Privoxy+FlareSolverr，可选）

### Bridge 侧（Python，新增）

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install playwright requests
playwright install chromium
```

### 外部服务（用户提供）
| 服务 | 必需？ | 说明 |
|------|--------|------|
| BitBrowser/AdsPower | ✅ | 指纹隔离，注册完释放窗口 |
| Clash Verge | ✅ | GitHub 需干净 IP |
| YesCaptcha/CapSolver | ✅ | 过 Arkose FunCaptcha |
| Outlook 邮箱池 | ✅ | `_outlook_pool/*.json` |

## 8. 文件变更总览

```
touch-xai-register/
├── internal/
│   └── bridge/                    ← 新增
│       ├── runner.go              👈 Go 桥接引擎
│       └── protocol.go            👈 NDJSON 协议
├── plugins/
│   └── github-registrar/         ← 新增
│       ├── plugin.json            👈 manifest
│       ├── runner.py              👈 Python 桥接入口
│       └── ui/
│           └── index.js           👈 面板配置 schema
├── scripts/
│   └── setup-regfactory-venv.sh  ← 新增（一键准备环境）
├── cmd/grok/main.go              ← 改：cmdRun 加 github-registrar
├── internal/api/server.go        ← 改：/api/start 解禁多插件
├── internal/api/plugins.go       ← 改：runner 路由
└── panel/src/app/settings/page.tsx ← 改：加打码/路径配置
```

## 9. 未来扩展：多平台注册架构总览

```mermaid
graph TB
    U[Panel UI<br/>注册页 | 号池页] --> A[API Server<br/>POST /api/start<br/>GET /api/plugins]

    A --> PM[Plugin Manager<br/>manifest 发现]
    A --> JM[Job Manager<br/>SSE 进度推送]

    JM --> RU

    subgraph Runners
        RU[Bridge Runner<br/>os/exec Python]
        LGCY[Legacy Pipeline<br/>Go in-process]
    end

    RU --> |stdin JSON| GH[github-registrar<br/>runner.py]
    RU -.-> |future| GR[grok-http-registrar]
    RU -.-> |future| CH[chatgpt-registrar]
    LGCY --> XAI[xai-accounts<br/>Go pipeline]

    GH --> |import| RF[reg-factory<br/>register_github.py]
    GR --> |import| RF2[reg-factory<br/>register_grok_http.py]
    CH --> |import| RF3[reg-factory<br/>register_chatgpt.py]

    GH --> |cookies| AS[Artifact Store]
    XAI --> |cpa.json| AS
    AS --> AP[Account Pool<br/>SQLite]
    AS --> |可选| CPA[CPA Upload]

    style GH fill:#10A37F,color:#fff
    style GR fill:#666,color:#fff
    style CH fill:#666,color:#fff
    style AS fill:#5A4FCF,color:#fff
    style AP fill:#5A4FCF,color:#fff
```
