# Claude 注册融合方案

> 上下文快照 · 2026-07-27
> 源文件：`cmd/grok/main.go`, `internal/pipeline/pipeline.go`, `internal/protocol/xai.go`, `internal/cpa/cpa.go`, `internal/oauth/oauth.go`, `internal/email/email.go`, `internal/plugin/manifest.go`
> 外部参考：`~/Downloadsear/claude-reg.py`（Claude 纯 HTTP 注册脚本）

---

## 1. 现有系统架构（touch-xai-register）

### 1.1 目录结构

```
touch-xai-register/
├── cmd/grok/main.go              # CLI 入口：start/stop/status/panel/plugin/pool
├── internal/
│   ├── pipeline/pipeline.go      # 核心流水线：S/P/C/OAuth 四类 worker
│   ├── protocol/xai.go           # xAI 注册协议：gRPC-web + Next.js server action
│   ├── cpa/cpa.go                # CPA 产物：Document 结构、WriteAtomic、Probe
│   ├── oauth/oauth.go            # xAI Device OAuth（设备码流）
│   ├── email/email.go            # 临时邮箱：tempmail.lol / mail.tm / custom
│   ├── turnstile/                # Cloudflare Turnstile 求解（browser/lite）
│   ├── clearance/                # CF 清障预热（FlareSolverr）
│   ├── daemon/daemon.go          # 后台进程管理（SIGTERM/KILL）
│   ├── plugin/                   # 插件系统：加载、清单验证、解析
│   ├── state/                    # 状态快照追踪
│   └── ...                       # 号池/acctpool/inventory/notify/transfer 等
├── plugins/
│   ├── xai-accounts/plugin.json  # xAI 注册插件（hybrid: Go + Python Turnstile）
│   ├── tavily-registrar/         # Tavily 注册插件（shell）
│   └── tavily-pool/              # Tavily 密钥池代理
├── panel/                        # Next.js Web 控制面板（Cloudflare Kumo UI）
├── web/                          # 嵌入式前端静态资源
└── docs/contracts/               # 插件 IDL 契约
```

### 1.2 xAI 注册流水线（S/P/C/OAuth 模式）

```
                    ┌──────────┐
                    │ Clearance│  清障预热（CF bypass）
                    └────┬─────┘
                         │
              ┌──────────┴──────────┐
              │                     │
         ┌────┴────┐          ┌─────┴─────┐
         │ S Worker │          │ P Worker  │
         │ Turnstile│          │ 邮箱+验证码│
         │ 求解     │          │           │
         └────┬─────┘          └─────┬─────┘
              │                      │
              └──────────┬───────────┘
                         │
                    ┌────┴────┐
                    │ C Worker│  注册（Next.js server action）+ SSO 提取
                    └────┬────┘
                         │
                    ┌────┴────┐
                    │ OAuth   │  Device OAuth → access/refresh token
                    │ Worker  │  探活 → CPA 写出
                    └─────────┘
```

**Worker 分工：**
- **S (Solve)**：获取 Turnstile token（Playwright 浏览器或 lite solver）
- **P (Prepare)**：创建临时邮箱 → 请求验证码 → 轮询收信
- **C (Confirm)**：消费 T+Q → 调注册接口 → 提取 SSO cookie
- **OAuth**：SSO → Device OAuth → 探活 → CPA JSON 写出 → 可选上传 Management API

### 1.3 插件系统

```json
// plugins/xai-accounts/plugin.json
{
  "id": "xai-accounts",
  "kind": ["registrar"],
  "runtime": "hybrid",
  "entry": { "go": "bin/xai-accounts", "js": "ui/index.js" },
  "capabilities": ["email", "turnstile", "browser", "http", "proxy", "storage", "secrets"],
  "artifactKinds": ["account.xai", "session.sso", "oauth.token", "cpa.json"]
}
```

插件清单字段：
- `kind`: registrar | pool-proxy | exporter | capability
- `runtime`: go | js | hybrid
- `hostApi`: 契约版本号
- `configSchema`: JSON Schema 配置声明
- `capabilities`: 声明的能力需求

### 1.4 CPA 产物格式

```go
// internal/cpa/cpa.go
type Document struct {
    Type          string            `json:"type"`           // "xai"
    AccessToken   string            `json:"access_token"`
    RefreshToken  string            `json:"refresh_token"`
    IDToken       string            `json:"id_token,omitempty"`
    TokenType     string            `json:"token_type,omitempty"`
    ExpiresIn     int               `json:"expires_in"`
    Expired       string            `json:"expired"`
    LastRefresh   string            `json:"last_refresh"`
    Sub           string            `json:"sub,omitempty"`
    Email         string            `json:"email,omitempty"`
    BaseURL       string            `json:"base_url"`
    TokenEndpoint string            `json:"token_endpoint"`
    AuthKind      string            `json:"auth_kind"`
    Headers       map[string]string `json:"headers"`
}
```

---

## 2. Claude 注册脚本分析（claude-reg.py）

### 2.1 核心流程

```
  1. CF 绕过
     │  curl_cffi 多指纹轮询（safari17_0 → safari18_0 → chrome120…）
     │  GET /login 拿 cookie（anthropic-device-id 等）
     │
  2. Magic Link
     │  POST /api/auth/send_magic_link
     │  QuickMail 轮询收信 → 解析 magic-link URL（#nonce:base64email）
     │
  3. Verify
     │  POST /api/auth/verify_magic_link
     │  Arkose token 传空串（等价于点外侧取消人机）
     │  拿 sessionKey + org_uuid
     │
  4. Onboarding
     │  PATCH /api/account/settings（has_started/finished）
     │  PUT /api/account/email_consent
     │  PUT /api/account/accept_legal_docs
     │  PUT /api/account（age_is_verified）
     │  POST /api/account/grove_notice_viewed + PATCH grove_enabled
     │
  5. Checkout
     │  POST …/checkout_session（plan=max_20x, country=DE, sepa_debit）
     │  等 6s → POST …/address（完整账单地址，不填 VAT）
     │
  6. Stripe SEPA
     │  GET  payment_pages/{sid}/init
     │  POST payment_pages/{sid}（tax_region=DE）
     │  POST payment_methods（sepa_debit + IBAN + 账单地址）
     │  POST payment_pages/{sid}/confirm（expected_amount=含税总额）
     │
  7. Claude 收单
     │  POST …/checkout_session/{sid}/approve
     │  POST …/checkout_session/{sid}/finalize（SEPA 异步，重试 ~6 次）
     │  GET  …/checkout_session/{sid}（轮询 paymentStatus）
     │
  8. Claude OAuth
        POST /v1/oauth/{org}/authorize（PKCE + arkose_token=""）
        POST api.anthropic.com/v1/oauth/token（code → access/refresh）
        写出 claude-<email>.json（CLIProxyAPI 可导入）
```

### 2.2 关键技术点

| 技术 | 方案 | 说明 |
|------|------|------|
| CF 绕过 | `curl_cffi` (chrome120/safari17_0) | Python 生态独有的 TLS 指纹伪装；Go 无等价库 |
| 人机验证 | Arkose 跳过（空 token） | 等价于浏览器点外侧取消，服务端 accept |
| 邮箱 | QuickMail API | 自定义邮件服务，支持 open/temp 模式 |
| 支付 | Stripe Custom Checkout + SEPA | 德国 IBAN（BLZ 白名单确保路由可解析），不填 VAT |
| 收单 | approve → finalize 两步 | 对照官方 claude-sepa.js 插件还原 |
| OAuth | Claude Code OAuth (PKCE) | client_id=`9d1c250a-...`, scope 含 `user:inference` |
| Token 测试 | `claude-haiku-4-5` 聊天验证 | 确保 access_token 真实可用 |

### 2.3 支付状态机

```
paid/succeeded/complete  ──→ 终态成功
processing/pending       ──→ 处理中（SEPA 异步，视为收单成功）
failed/canceled/expired  ──→ 终态失败
subscriptionCreated=true ──→ 视为成功（有订阅即为开通）
```

---

## 3. 融合方案

### 3.1 技术决策

| 决策 | 选项 | 理由 |
|------|------|------|
| 技术路线 | **hybrid: Python 子进程** | curl_cffi TLS 指纹在 Go 无等价方案；与现有 Turnstile Python 脚本模式一致 |
| 功能范围 | **完整流程（注册+支付+OAuth）** | 对齐 Python 脚本能力，产出可用 Claude Max 账号 |
| 产物格式 | **统一 CPA 格式** | type=`"claude"`，可入库本地号池和云端 Management API |

### 3.2 插件结构

```
plugins/claude-registrar/
├── plugin.json                    # 清单声明
├── bin/claude-registrar           # Go 编排层（编译产物）
├── scripts/
│   ├── claude-reg.py              # Python 协议层（JSON行 stdin/stdout）
│   └── requirements.txt           # curl_cffi
└── ui/index.js                    # 面板注册（可选）
```

### 3.3 plugin.json 设计

```json
{
  "id": "claude-registrar",
  "name": "Claude Registrar",
  "version": "0.1.0",
  "description": "Claude 账号注册（Magic Link + Stripe SEPA + Claude Code OAuth）",
  "kind": ["registrar"],
  "runtime": "hybrid",
  "entry": {
    "go": "bin/claude-registrar",
    "js": "ui/index.js"
  },
  "hostApi": "0.1",
  "capabilities": [
    "email",
    "http",
    "proxy",
    "storage",
    "secrets",
    "events"
  ],
  "artifactKinds": [
    "account.claude",
    "session.claude",
    "oauth.token",
    "cpa.json"
  ],
  "configSchema": {
    "type": "object",
    "properties": {
      "target": { "type": "integer", "minimum": 1, "maximum": 100, "default": 1 },
      "plan": { "type": "string", "enum": ["max_5x", "max_20x", "pro"], "default": "max_20x" },
      "country": { "type": "string", "default": "DE" },
      "quickmailBase": { "type": "string" },
      "quickmailKey": { "type": "string" },
      "quickmailDomain": { "type": "string" },
      "quickmailMode": { "type": "string", "enum": ["auto", "open", "temp"] },
      "noPay": { "type": "boolean", "default": false },
      "impersonate": { "type": "string", "default": "safari17_0" }
    },
    "additionalProperties": true
  },
  "source": "in-tree"
}
```

### 3.4 Go ↔ Python 子进程契约

Go 通过 stdin 发送 JSON 行命令，Python 通过 stdout 输出 JSON 行事件。

**请求格式（Go → Python）：**
```json
{"action":"register","params":{"email":"xxx@quickmail.xxx","iban":"DE...","plan":"max_20x","country":"DE","quickmail":{"base":"...","key":"...","domain":"...","mode":"auto"}}}
```

**事件格式（Python → Go）：**
```json
{"event":"log","level":"info","msg":"[L2] send_magic_link xxx@..."}
{"event":"log","level":"warn","msg":"[!] arkose skip: ..."}
{"event":"progress","phase":"register","detail":"verify magic link"}
{"event":"progress","phase":"checkout","detail":"stripe confirm"}
{"event":"done","ok":true,"account":{"email":"...","org_uuid":"...","session_key":"...","plan":"max_20x","payment_status":"processing","oauth":{"access_token":"...","refresh_token":"...","expired":"...","email":"..."}}}
{"event":"error","msg":"verify_magic_link -> 403: ..."}
```

### 3.5 Go 编排层设计

```go
// internal/pipeline/claude.go（新增）
type ClaudeWorker struct {
    pythonPath string   // scripts/claude-reg.py
    outputDir  string   // run 输出目录
    store      *state.Store
}

func (w *ClaudeWorker) Register(ctx context.Context, params ClaudeParams) (*cpa.Document, error) {
    cmd := exec.CommandContext(ctx, pythonPath)
    cmd.Stdin = stdinPipe   // 写 JSON 行
    cmd.Stdout = stdoutPipe // 读 JSON 行
    // 逐行解析事件 → 更新 store 状态 → 最终收集 Document
}
```

### 3.6 CPA 产物格式（Claude 扩展）

```json
{
  "type": "claude",
  "access_token": "ses_...",
  "refresh_token": "...",
  "email": "xxx@quickmail.xxx",
  "expired": "2026-08-03T12:00:00+08:00",
  "last_refresh": "2026-07-27T12:00:00+08:00",
  "base_url": "https://api.anthropic.com/v1",
  "auth_kind": "oauth",
  "headers": {
    "anthropic-beta": "oauth-2025-04-20",
    "anthropic-version": "2023-06-01"
  },
  "ext": {
    "session_key": "...",
    "org_uuid": "...",
    "plan": "max_20x",
    "payment_status": "processing",
    "iban": "DE...",
    "country": "DE"
  }
}
```

### 3.7 与现有系统集成对照

| 现有模块 | 集成方式 | 备注 |
|----------|----------|------|
| `internal/pipeline` | 新增 `ClaudeWorker`，独立于 S/P/C 流水线 | Claude 流程完全不同 |
| `internal/state` | 复用 `state.Store` / `state.Snapshot` | 进度上报、阶段标记 |
| `internal/cpa` | 复用 `Document` + `WriteAtomic` + `Probe` | type=`"claude"`，Probe 走 Anthropic API |
| `internal/daemon` | 复用 `StartBackground` / `Stop` | `run claude-registrar` 后台模式 |
| `internal/plugin` | `plugin.json` 自动发现 | 面板通过 `/api/plugins` 拉取 |
| `internal/email` | **不用** | QuickMail 调用留在 Python 侧 |
| `internal/oauth` | **不用** | Claude OAuth 走 PKCE 流程 |
| `internal/turnstile` | **不用** | Claude 用 Arkose（可跳过） |
| `panel` | plugin.json 声明 → 自动注册 | 面板扫描 `registrar` 类型插件 |

### 3.8 流程差异对比

```
                    xAI                          Claude
                    ───                          ──────
  Captcha     Turnstile (CF)               Arkose（可跳过）+ CF 绕过
  邮箱        临时邮箱验证码 (grpc-web)      Magic Link 邮件
  注册        Next.js server action        verify_magic_link + onboarding
  支付        —                             Stripe SEPA → approve → finalize
  OAuth       Device OAuth                 Claude Code OAuth (PKCE)
  产物        CPA JSON (Cliproxy)           CPA JSON (Anthropic API)
```

---

## 4. Python 脚本改造要点

现有 `claude-reg.py` 需要拆分：

| 保留（进 scripts/claude-reg.py） | 移除（交给 Go） |
|----------------------------------|-----------------|
| `ClaudeHTTP` 类（全部 HTTP 方法） | `argparse` CLI 层 |
| `QuickMail` 类 | `run_once()` 函数 |
| `random_de_iban()` / `person()` | 文件写入（`RESULT_JSON`） |
| `wait_magic_link()` / `parse_magic_link()` | 批量循环（`main()` for 循环） |
| `pay_sepa()` / `finalize_subscription()` | 环境变量读取（Go 通过 stdin 传配置） |
| `oauth_authorize()` / `oauth_exchange()` | OAuth 文件写出（Go 通过 CPA 模块写） |
| `generate_cliproxy_auth()` | |

---

## 5. 实施步骤

### Phase 1：拆 Python 协议层
1. 从 `claude-reg.py` 提取 `ClaudeHTTP` + `QuickMail` 类
2. 实现 stdin/stdout JSON 行协议
3. 环境变量配置改为 stdin params 注入
4. 产物不写文件，通过 stdout `done` 事件返回

### Phase 2：写 Go 编排层
1. `internal/pipeline/claude.go`：Python 子进程管理 + 事件解析
2. 状态上报到 `state.Store`
3. 产物格式化 → `cpa.Document`{Type:"claude"} → `WriteAtomic`

### Phase 3：插件集成
1. `plugins/claude-registrar/plugin.json`
2. Makefile 构建目标
3. 面板自动发现验证

### Phase 4：测试与对齐
1. 单账号端到端测试
2. CPA 格式验证
3. 号池入库验证
4. 文档完善

---

## 6. 风险与注意事项

- **CF 绕过依赖 curl_cffi**：Python 子进程是唯一务实的方案；Go 侧 uTLS 等库成熟度不足
- **QuickMail 依赖外部服务**：需要稳定的 QuickMail 实例
- **Stripe SEPA 异步**：支付确认后可能是 `processing` 态而非 `paid`，需以 `subscriptionCreated` 为准
- **IBAN 有效性**：使用 DE_BLZ 白名单确保 Stripe 可解析 BIC，但账户号随机仍可能被拒
- **并发控制**：Claude 侧可能有频率限制，批量注册需控制并发数
