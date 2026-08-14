package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type EmailMode string

const (
	EmailTempmail   EmailMode = "tempmail"
	EmailCustom     EmailMode = "custom"
	EmailDuckMail   EmailMode = "duckmail"
	EmailCloudflare EmailMode = "cloudflare"
	EmailCloudMail  EmailMode = "cloudmail"
	EmailMailNest   EmailMode = "mailnest"
	EmailMoeMail    EmailMode = "moemail"
	EmailYYDS       EmailMode = "yyds"
)

type Config struct {
	EmailMode   EmailMode
	EmailDomain string
	EmailAPI    string

	// Email providers (from grok-register-panel). Each maps to the same-named
	// lowercase config key in grok-register-panel (e.g. duckmail_api_key).
	EmailDefaultDomains          string // comma-separated, shared by cloudflare/cloudmail
	DuckMailBase                 string
	DuckMailKey                  string
	CloudflareBase               string
	CloudflareKey                string
	CloudflareAuthMode           string // none | x-api-key | x-admin-auth | query-key | bearer
	CloudflareCustomAuth         string
	CloudflareRandomizeSubdomain bool
	CloudMailURL                 string
	CloudMailAdminEmail          string
	CloudMailPassword            string
	MailNestKey                  string
	MailNestProjectCode          string
	MoeMailBase                  string
	MoeMailKey                   string
	MoeMailDomain                string
	MoeMailExpiryMS              int64
	YYDSKey                      string
	YYDSJWT                      string
	YYDSDomain                   string

	// Bridge plugins integrate locally installed registrar projects. These are
	// runtime paths, so they belong to the persistent panel configuration rather
	// than the process environment.
	BridgeRegFactoryRoot string
	BridgeGrokPanelRoot  string
	BridgeOutlookPoolDir string
	BridgePythonExe      string

	ClearanceEnabled bool
	RegisterProxy    string
	FlareSolverrURL  string
	ClearanceProxy   string
	ClearanceURLs    string

	Target      int
	PhysicalCap int

	TurnstileProvider        string
	LiteSolverURL            string
	TurnstileChromePath      string
	TurnstilePython          string
	TurnstileScript          string
	TurnstileInjectClearance bool

	ProtocolHTTP bool
	HTTPPoolSize int

	TempmailLOLRetries    int
	TempmailLOLIntervalMS int

	OAuthMinIntervalSec float64
	OAuthRetrySec       float64
	ProbeEnabled        bool

	HTTPProxy  string
	HTTPSProxy string
	NoProxy    string

	// External infrastructure. Endpoint metadata is persisted normally; access
	// tokens are appended separately so Config.Save never writes them by accident.
	ResinProxy    string
	ResinToken    string
	ResinPlatform string

	MailRouterURL    string
	MailRouterAPIKey string
	MailRouterDomain string

	// CPA Management upload
	CPAUploadEnabled      bool
	CPAManagementBase     string
	CPAManagementKey      string
	CPAUploadTimeoutSec   int
	CPAUploadRetries      int
	CPAUploadNameTemplate string
	CPAUploadVerify       bool
	CPAUploadMode         string // multipart | json

	// Transfer (upload/export jobs) defaults
	UploadConcurrency int
	UploadBatchSize   int
	ExportBatchSize   int
	ExportConcurrency int

	// Pool patrol (巡检) & quota estimate
	PatrolEnabled     bool
	PatrolIntervalMin int
	PatrolDeepProbe   bool
	PatrolConcurrency int
	QuotaPerAccount   int

	// 降智检测 (silent model downgrade). Each check is a real billed request
	// against one account, so scans are sampled and paced by the same per-exit
	// account limit that causes the flag in the first place.
	DegradeModel          string
	DegradeSample         int
	DegradeRecheckMin     int
	DegradeExitWindowMin  int
	DegradeExitAccountCap int

	// Auto refill (自动补号)
	RefillEnabled     bool
	RefillMinHealthy  int
	RefillBatch       int
	RefillCooldownMin int
	RefillDailyCap    int

	// Cleanup free-usage / quota exhausted accounts from the live CPA pool.
	// Transient 429 rate limits are never deleted by this path.
	CleanupQuotaEnabled bool
	CleanupOnPatrol     bool // run after each successful patrol
	CleanupBackup       bool // download before delete
	CleanupDryRun       bool // scan + report only

	// Cluster / federation (master–slave pool orchestration)
	// Role: standalone | master | slave
	ClusterRole         string
	ClusterNodeName     string
	ClusterPublicToken  string // optional shared secret for federation endpoints (slave↔master)
	ClusterMasterURL    string // legacy single master URL (still honored)
	ClusterMasterURLs   string // slave: multi masters, comma/newline separated
	ClusterHeartbeatSec int    // slave poll interval
	ClusterPoolTarget   int    // master desired healthy pool size
	ClusterAssignMin    int    // per-slave assign lower bound (1-10)
	ClusterAssignMax    int    // per-slave assign upper bound (1-10)
	ClusterAutoRegister bool   // slave auto start pipeline when assigned
	ClusterAutoUpload   bool   // slave upload CPA after batch
	// Public status page (human-facing), independent from ClusterPublicToken
	ClusterStatusPassword string // empty = open; set to require password on /status
	// Federation pool share (master-side ACL for slaves/peers)
	ClusterSharePoolList       bool // allow list formal CPA pool over federation token
	ClusterSharePoolPull       bool // allow download credentials over federation token
	ClusterShareInfrastructure bool // share Resin + TouchMailRouter config with authenticated peers

	// Local pool (register results)
	LocalPoolAutoImport bool // after register, copy CPA json into GROK_HOME/local-pool
	LocalPoolAutoSync   bool // upload local-pool to CPA management (master formal pool)
}

func Defaults() Config {
	return Config{
		EmailMode:                    EmailTempmail,
		EmailAPI:                     "http://127.0.0.1:8080",
		ClearanceEnabled:             true,
		RegisterProxy:                "http://127.0.0.1:40080",
		FlareSolverrURL:              "http://127.0.0.1:8191",
		ClearanceProxy:               "http://privoxy:8118",
		ClearanceURLs:                "https://accounts.x.ai,https://x.ai,https://status.x.ai,https://console.x.ai,https://auth.x.ai",
		Target:                       10,
		PhysicalCap:                  0,
		TurnstileProvider:            "browser",
		LiteSolverURL:                "http://127.0.0.1:5072",
		ProtocolHTTP:                 true,
		HTTPPoolSize:                 8,
		TempmailLOLRetries:           30,
		TempmailLOLIntervalMS:        1500,
		CloudflareRandomizeSubdomain: true,
		MailNestProjectCode:          "x-ai001",
		MoeMailExpiryMS:              3600000,
		OAuthMinIntervalSec:          10,
		OAuthRetrySec:                60,
		ProbeEnabled:                 true,
		HTTPProxy:                    "http://127.0.0.1:40080",
		HTTPSProxy:                   "http://127.0.0.1:40080",
		NoProxy:                      "127.0.0.1,localhost",
		ResinPlatform:                "Default",
		CPAUploadEnabled:             false,
		CPAManagementBase:            "http://localhost:8317/v0/management",
		CPAUploadTimeoutSec:          30,
		CPAUploadRetries:             2,
		CPAUploadNameTemplate:        "{email}.json",
		CPAUploadVerify:              true,
		CPAUploadMode:                "multipart",
		UploadConcurrency:            3,
		UploadBatchSize:              20,
		ExportBatchSize:              500,
		ExportConcurrency:            15,
		PatrolEnabled:                false,
		PatrolIntervalMin:            30,
		PatrolDeepProbe:              false,
		PatrolConcurrency:            10,
		QuotaPerAccount:              60,
		DegradeModel:                 "grok-4.6",
		DegradeSample:                5,
		DegradeRecheckMin:            120,
		DegradeExitWindowMin:         10,
		DegradeExitAccountCap:        5,
		RefillEnabled:                false,
		RefillMinHealthy:             5,
		RefillBatch:                  10,
		RefillCooldownMin:            60,
		RefillDailyCap:               50,
		CleanupQuotaEnabled:          false,
		CleanupOnPatrol:              true,
		CleanupBackup:                true,
		CleanupDryRun:                false,
		ClusterRole:                  "standalone",
		ClusterHeartbeatSec:          15,
		ClusterPoolTarget:            50,
		ClusterAssignMin:             1,
		ClusterAssignMax:             10,
		ClusterAutoRegister:          true,
		ClusterAutoUpload:            true,
		ClusterSharePoolList:         false,
		ClusterSharePoolPull:         false,
		LocalPoolAutoImport:          true,
		LocalPoolAutoSync:            false,
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	env := parseEnvFile(string(data))
	applyMap(&cfg, env)
	return cfg, nil
}

func Save(path string, cfg Config) error {
	var b strings.Builder
	b.WriteString("# grok-reg config\n")
	b.WriteString(fmt.Sprintf("EMAIL_MODE=%s\n", cfg.EmailMode))
	if cfg.EmailDomain != "" {
		b.WriteString(fmt.Sprintf("EMAIL_DOMAIN=%s\n", cfg.EmailDomain))
	}
	if cfg.EmailAPI != "" {
		b.WriteString(fmt.Sprintf("EMAIL_API=%s\n", cfg.EmailAPI))
	}
	b.WriteString(fmt.Sprintf("DEFAULT_DOMAINS=%s\n", cfg.EmailDefaultDomains))
	b.WriteString(fmt.Sprintf("DUCKMAIL_API_BASE=%s\n", cfg.DuckMailBase))
	// DUCKMAIL_API_KEY: written by saveConfigWithSecrets.
	b.WriteString(fmt.Sprintf("CLOUDFLARE_API_BASE=%s\n", cfg.CloudflareBase))
	// CLOUDFLARE_API_KEY / CLOUDFLARE_CUSTOM_AUTH: written by saveConfigWithSecrets.
	b.WriteString(fmt.Sprintf("CLOUDFLARE_AUTH_MODE=%s\n", cfg.CloudflareAuthMode))
	b.WriteString(fmt.Sprintf("CLOUDFLARE_RANDOMIZE_SUBDOMAIN=%s\n", bool01(cfg.CloudflareRandomizeSubdomain)))
	b.WriteString(fmt.Sprintf("CLOUDMAIL_URL=%s\n", cfg.CloudMailURL))
	b.WriteString(fmt.Sprintf("CLOUDMAIL_ADMIN_EMAIL=%s\n", cfg.CloudMailAdminEmail))
	// CLOUDMAIL_PASSWORD: written by saveConfigWithSecrets.
	b.WriteString(fmt.Sprintf("MAILNEST_PROJECT_CODE=%s\n", cfg.MailNestProjectCode))
	// MAILNEST_API_KEY: written by saveConfigWithSecrets.
	b.WriteString(fmt.Sprintf("MOEMAIL_API_BASE=%s\n", cfg.MoeMailBase))
	b.WriteString(fmt.Sprintf("MOEMAIL_DOMAIN=%s\n", cfg.MoeMailDomain))
	b.WriteString(fmt.Sprintf("MOEMAIL_EXPIRY_MS=%d\n", cfg.MoeMailExpiryMS))
	// MOEMAIL_API_KEY: written by saveConfigWithSecrets.
	b.WriteString(fmt.Sprintf("YYDS_DEFAULT_DOMAIN=%s\n", cfg.YYDSDomain))
	// YYDS_API_KEY / YYDS_JWT: written by saveConfigWithSecrets.
	b.WriteString(fmt.Sprintf("CLEARANCE_ENABLED=%s\n", bool01(cfg.ClearanceEnabled)))
	b.WriteString(fmt.Sprintf("REGISTER_PROXY=%s\n", cfg.RegisterProxy))
	b.WriteString(fmt.Sprintf("FLARESOLVERR_URL=%s\n", cfg.FlareSolverrURL))
	b.WriteString(fmt.Sprintf("CLEARANCE_PROXY=%s\n", cfg.ClearanceProxy))
	b.WriteString(fmt.Sprintf("CLEARANCE_URLS=%s\n", cfg.ClearanceURLs))
	b.WriteString(fmt.Sprintf("TURNSTILE_PROVIDER=%s\n", cfg.TurnstileProvider))
	b.WriteString(fmt.Sprintf("LITE_SOLVER_URL=%s\n", cfg.LiteSolverURL))
	b.WriteString(fmt.Sprintf("TURNSTILE_CHROME_PATH=%s\n", cfg.TurnstileChromePath))
	b.WriteString(fmt.Sprintf("TURNSTILE_PYTHON=%s\n", cfg.TurnstilePython))
	b.WriteString(fmt.Sprintf("TURNSTILE_SCRIPT=%s\n", cfg.TurnstileScript))
	b.WriteString(fmt.Sprintf("TURNSTILE_INJECT_CLEARANCE=%s\n", bool01(cfg.TurnstileInjectClearance)))
	b.WriteString(fmt.Sprintf("PROTOCOL_HTTP=%s\n", bool01(cfg.ProtocolHTTP)))
	b.WriteString(fmt.Sprintf("HTTP_POOL_SIZE=%d\n", cfg.HTTPPoolSize))
	b.WriteString(fmt.Sprintf("OAUTH_MIN_INTERVAL_SEC=%g\n", cfg.OAuthMinIntervalSec))
	b.WriteString(fmt.Sprintf("OAUTH_RETRY_SEC=%g\n", cfg.OAuthRetrySec))
	b.WriteString(fmt.Sprintf("TEMPMAIL_LOL_RETRIES=%d\n", cfg.TempmailLOLRetries))
	b.WriteString(fmt.Sprintf("TEMPMAIL_LOL_MIN_INTERVAL_MS=%d\n", cfg.TempmailLOLIntervalMS))
	b.WriteString(fmt.Sprintf("HTTPS_PROXY=%s\n", cfg.HTTPSProxy))
	b.WriteString(fmt.Sprintf("HTTP_PROXY=%s\n", cfg.HTTPProxy))
	b.WriteString(fmt.Sprintf("NO_PROXY=%s\n", cfg.NoProxy))
	b.WriteString(fmt.Sprintf("RESIN_PROXY=%s\n", cfg.ResinProxy))
	b.WriteString(fmt.Sprintf("RESIN_PLATFORM=%s\n", cfg.ResinPlatform))
	// RESIN_TOKEN: written via appendEnvKey when explicitly set from panel.
	b.WriteString(fmt.Sprintf("MAIL_ROUTER_URL=%s\n", cfg.MailRouterURL))
	b.WriteString(fmt.Sprintf("MAIL_ROUTER_DOMAIN=%s\n", cfg.MailRouterDomain))
	// MAIL_ROUTER_API_KEY: written via appendEnvKey when explicitly set from panel.
	b.WriteString(fmt.Sprintf("BRIDGE_REG_FACTORY_ROOT=%s\n", cfg.BridgeRegFactoryRoot))
	b.WriteString(fmt.Sprintf("BRIDGE_GROK_PANEL_ROOT=%s\n", cfg.BridgeGrokPanelRoot))
	b.WriteString(fmt.Sprintf("BRIDGE_OUTLOOK_POOL_DIR=%s\n", cfg.BridgeOutlookPoolDir))
	b.WriteString(fmt.Sprintf("BRIDGE_PYTHON=%s\n", cfg.BridgePythonExe))
	b.WriteString(fmt.Sprintf("PROBE_ENABLED=%s\n", bool01(cfg.ProbeEnabled)))
	b.WriteString(fmt.Sprintf("PHYSICAL_CAP=%d\n", cfg.PhysicalCap))
	b.WriteString(fmt.Sprintf("CPA_UPLOAD_ENABLED=%s\n", bool01(cfg.CPAUploadEnabled)))
	b.WriteString(fmt.Sprintf("CPA_MANAGEMENT_BASE=%s\n", cfg.CPAManagementBase))
	// CPA_MANAGEMENT_KEY: never auto-written (set manually in config.env)
	b.WriteString(fmt.Sprintf("CPA_UPLOAD_TIMEOUT_SEC=%d\n", cfg.CPAUploadTimeoutSec))
	b.WriteString(fmt.Sprintf("CPA_UPLOAD_RETRIES=%d\n", cfg.CPAUploadRetries))
	b.WriteString(fmt.Sprintf("CPA_UPLOAD_NAME_TEMPLATE=%s\n", cfg.CPAUploadNameTemplate))
	b.WriteString(fmt.Sprintf("CPA_UPLOAD_VERIFY=%s\n", bool01(cfg.CPAUploadVerify)))
	b.WriteString(fmt.Sprintf("CPA_UPLOAD_MODE=%s\n", cfg.CPAUploadMode))
	b.WriteString(fmt.Sprintf("UPLOAD_CONCURRENCY=%d\n", cfg.UploadConcurrency))
	b.WriteString(fmt.Sprintf("UPLOAD_BATCH_SIZE=%d\n", cfg.UploadBatchSize))
	b.WriteString(fmt.Sprintf("EXPORT_BATCH_SIZE=%d\n", cfg.ExportBatchSize))
	b.WriteString(fmt.Sprintf("EXPORT_CONCURRENCY=%d\n", cfg.ExportConcurrency))
	b.WriteString(fmt.Sprintf("PATROL_ENABLED=%s\n", bool01(cfg.PatrolEnabled)))
	b.WriteString(fmt.Sprintf("PATROL_INTERVAL_MIN=%d\n", cfg.PatrolIntervalMin))
	b.WriteString(fmt.Sprintf("PATROL_DEEP_PROBE=%s\n", bool01(cfg.PatrolDeepProbe)))
	b.WriteString(fmt.Sprintf("PATROL_CONCURRENCY=%d\n", cfg.PatrolConcurrency))
	b.WriteString(fmt.Sprintf("QUOTA_PER_ACCOUNT=%d\n", cfg.QuotaPerAccount))
	b.WriteString(fmt.Sprintf("DEGRADE_MODEL=%s\n", cfg.DegradeModel))
	b.WriteString(fmt.Sprintf("DEGRADE_SAMPLE=%d\n", cfg.DegradeSample))
	b.WriteString(fmt.Sprintf("DEGRADE_RECHECK_MIN=%d\n", cfg.DegradeRecheckMin))
	b.WriteString(fmt.Sprintf("DEGRADE_EXIT_WINDOW_MIN=%d\n", cfg.DegradeExitWindowMin))
	b.WriteString(fmt.Sprintf("DEGRADE_EXIT_ACCOUNT_CAP=%d\n", cfg.DegradeExitAccountCap))
	b.WriteString(fmt.Sprintf("REFILL_ENABLED=%s\n", bool01(cfg.RefillEnabled)))
	b.WriteString(fmt.Sprintf("REFILL_MIN_HEALTHY=%d\n", cfg.RefillMinHealthy))
	b.WriteString(fmt.Sprintf("REFILL_BATCH=%d\n", cfg.RefillBatch))
	b.WriteString(fmt.Sprintf("REFILL_COOLDOWN_MIN=%d\n", cfg.RefillCooldownMin))
	b.WriteString(fmt.Sprintf("REFILL_DAILY_CAP=%d\n", cfg.RefillDailyCap))
	b.WriteString(fmt.Sprintf("CLEANUP_QUOTA_ENABLED=%s\n", bool01(cfg.CleanupQuotaEnabled)))
	b.WriteString(fmt.Sprintf("CLEANUP_ON_PATROL=%s\n", bool01(cfg.CleanupOnPatrol)))
	b.WriteString(fmt.Sprintf("CLEANUP_BACKUP=%s\n", bool01(cfg.CleanupBackup)))
	b.WriteString(fmt.Sprintf("CLEANUP_DRY_RUN=%s\n", bool01(cfg.CleanupDryRun)))
	b.WriteString(fmt.Sprintf("CLUSTER_ROLE=%s\n", cfg.ClusterRole))
	b.WriteString(fmt.Sprintf("CLUSTER_NODE_NAME=%s\n", cfg.ClusterNodeName))
	// CLUSTER_PUBLIC_TOKEN: written via appendEnvKey when set from panel
	b.WriteString(fmt.Sprintf("CLUSTER_MASTER_URL=%s\n", cfg.ClusterMasterURL))
	// Persist full endpoints (URL + optional per-master token) as JSON.
	b.WriteString(fmt.Sprintf("CLUSTER_MASTER_URLS=%s\n", FormatMasterEndpoints(cfg.ClusterMasterEndpoints())))
	b.WriteString(fmt.Sprintf("CLUSTER_HEARTBEAT_SEC=%d\n", cfg.ClusterHeartbeatSec))
	b.WriteString(fmt.Sprintf("CLUSTER_POOL_TARGET=%d\n", cfg.ClusterPoolTarget))
	b.WriteString(fmt.Sprintf("CLUSTER_ASSIGN_MIN=%d\n", cfg.ClusterAssignMin))
	b.WriteString(fmt.Sprintf("CLUSTER_ASSIGN_MAX=%d\n", cfg.ClusterAssignMax))
	b.WriteString(fmt.Sprintf("CLUSTER_AUTO_REGISTER=%s\n", bool01(cfg.ClusterAutoRegister)))
	b.WriteString(fmt.Sprintf("CLUSTER_AUTO_UPLOAD=%s\n", bool01(cfg.ClusterAutoUpload)))
	b.WriteString(fmt.Sprintf("CLUSTER_SHARE_POOL_LIST=%s\n", bool01(cfg.ClusterSharePoolList)))
	b.WriteString(fmt.Sprintf("CLUSTER_SHARE_POOL_PULL=%s\n", bool01(cfg.ClusterSharePoolPull)))
	b.WriteString(fmt.Sprintf("CLUSTER_SHARE_INFRASTRUCTURE=%s\n", bool01(cfg.ClusterShareInfrastructure)))
	b.WriteString(fmt.Sprintf("LOCAL_POOL_AUTO_IMPORT=%s\n", bool01(cfg.LocalPoolAutoImport)))
	b.WriteString(fmt.Sprintf("LOCAL_POOL_AUTO_SYNC=%s\n", bool01(cfg.LocalPoolAutoSync)))
	// CLUSTER_STATUS_PASSWORD via appendEnvKey when set from panel
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func InteractiveSetup(path string) (Config, error) {
	cfg := Defaults()
	fmt.Println()
	fmt.Println("选择邮箱模式:")
	fmt.Println("  [1] 免费临时邮箱           (默认 · 零配置 · 直接回车)")
	fmt.Println("  [2] 自建域名邮箱           (需 Cloudflare Email Routing + 本地 webhook)")
	fmt.Print("输入 1 或 2 [1]: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "2" {
		cfg.EmailMode = EmailCustom
		fmt.Print("  你的域名 (如 example.com): ")
		dom, _ := reader.ReadString('\n')
		cfg.EmailDomain = strings.TrimSpace(dom)
		fmt.Print("  webhook 地址 [http://127.0.0.1:8080]: ")
		api, _ := reader.ReadString('\n')
		api = strings.TrimSpace(api)
		if api == "" {
			api = "http://127.0.0.1:8080"
		}
		cfg.EmailAPI = api
	} else {
		cfg.EmailMode = EmailTempmail
	}
	if err := Save(path, cfg); err != nil {
		return cfg, err
	}
	fmt.Printf("[*] 已写入 %s\n", path)
	return cfg, nil
}

func ClampTarget(n int) (int, error) {
	if n < 1 {
		return 0, fmt.Errorf("target must be >= 1, got %d", n)
	}
	if n > 10000 {
		return 0, fmt.Errorf("target max is 10000, got %d", n)
	}
	return n, nil
}

func parseEnvFile(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		v = strings.Trim(v, `"'`)
		out[k] = v
	}
	return out
}

func applyMap(cfg *Config, env map[string]string) {
	if v, ok := env["EMAIL_MODE"]; ok {
		cfg.EmailMode = EmailMode(strings.ToLower(v))
	}
	if v, ok := env["EMAIL_DOMAIN"]; ok {
		cfg.EmailDomain = v
	}
	if v, ok := env["EMAIL_API"]; ok {
		cfg.EmailAPI = v
	}
	if v, ok := env["DEFAULT_DOMAINS"]; ok {
		cfg.EmailDefaultDomains = v
	}
	if v, ok := env["DUCKMAIL_API_BASE"]; ok {
		cfg.DuckMailBase = v
	}
	if v, ok := env["DUCKMAIL_API_KEY"]; ok {
		cfg.DuckMailKey = v
	}
	if v, ok := env["CLOUDFLARE_API_BASE"]; ok {
		cfg.CloudflareBase = v
	}
	if v, ok := env["CLOUDFLARE_API_KEY"]; ok {
		cfg.CloudflareKey = v
	}
	if v, ok := env["CLOUDFLARE_AUTH_MODE"]; ok {
		cfg.CloudflareAuthMode = v
	}
	if v, ok := env["CLOUDFLARE_CUSTOM_AUTH"]; ok {
		cfg.CloudflareCustomAuth = v
	}
	if v, ok := env["CLOUDFLARE_RANDOMIZE_SUBDOMAIN"]; ok {
		cfg.CloudflareRandomizeSubdomain = truthy(v)
	}
	if v, ok := env["CLOUDMAIL_URL"]; ok {
		cfg.CloudMailURL = v
	}
	if v, ok := env["CLOUDMAIL_ADMIN_EMAIL"]; ok {
		cfg.CloudMailAdminEmail = v
	}
	if v, ok := env["CLOUDMAIL_PASSWORD"]; ok {
		cfg.CloudMailPassword = v
	}
	if v, ok := env["MAILNEST_API_KEY"]; ok {
		cfg.MailNestKey = v
	}
	if v, ok := env["MAILNEST_PROJECT_CODE"]; ok {
		cfg.MailNestProjectCode = v
	}
	if v, ok := env["MOEMAIL_API_BASE"]; ok {
		cfg.MoeMailBase = v
	}
	if v, ok := env["MOEMAIL_API_KEY"]; ok {
		cfg.MoeMailKey = v
	}
	if v, ok := env["MOEMAIL_DOMAIN"]; ok {
		cfg.MoeMailDomain = v
	}
	if v, ok := env["MOEMAIL_EXPIRY_MS"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.MoeMailExpiryMS = n
		}
	}
	if v, ok := env["YYDS_API_KEY"]; ok {
		cfg.YYDSKey = v
	}
	if v, ok := env["YYDS_JWT"]; ok {
		cfg.YYDSJWT = v
	}
	if v, ok := env["YYDS_DEFAULT_DOMAIN"]; ok {
		cfg.YYDSDomain = v
	}
	if v, ok := env["BRIDGE_REG_FACTORY_ROOT"]; ok {
		cfg.BridgeRegFactoryRoot = v
	}
	if v, ok := env["BRIDGE_GROK_PANEL_ROOT"]; ok {
		cfg.BridgeGrokPanelRoot = v
	}
	if v, ok := env["BRIDGE_OUTLOOK_POOL_DIR"]; ok {
		cfg.BridgeOutlookPoolDir = v
	}
	if v, ok := env["BRIDGE_PYTHON"]; ok {
		cfg.BridgePythonExe = v
	}
	if v, ok := env["CLEARANCE_ENABLED"]; ok {
		cfg.ClearanceEnabled = truthy(v)
	}
	if v, ok := env["REGISTER_PROXY"]; ok {
		cfg.RegisterProxy = v
	}
	if v, ok := env["FLARESOLVERR_URL"]; ok {
		cfg.FlareSolverrURL = v
	}
	if v, ok := env["CLEARANCE_PROXY"]; ok {
		cfg.ClearanceProxy = v
	}
	if v, ok := env["CLEARANCE_URLS"]; ok {
		cfg.ClearanceURLs = v
	}
	if v, ok := env["TURNSTILE_PROVIDER"]; ok {
		cfg.TurnstileProvider = v
	}
	if v, ok := env["LITE_SOLVER_URL"]; ok {
		cfg.LiteSolverURL = v
	}
	if v, ok := env["TURNSTILE_CHROME_PATH"]; ok {
		cfg.TurnstileChromePath = v
	}
	if v, ok := env["TURNSTILE_PYTHON"]; ok {
		cfg.TurnstilePython = v
	}
	if v, ok := env["TURNSTILE_SCRIPT"]; ok {
		cfg.TurnstileScript = v
	}
	if v, ok := env["TURNSTILE_INJECT_CLEARANCE"]; ok {
		cfg.TurnstileInjectClearance = truthy(v)
	}
	if v, ok := env["PROTOCOL_HTTP"]; ok {
		cfg.ProtocolHTTP = truthy(v)
	}
	if v, ok := env["OAUTH_MIN_INTERVAL_SEC"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.OAuthMinIntervalSec = n
		}
	}
	if v, ok := env["OAUTH_RETRY_SEC"]; ok {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.OAuthRetrySec = n
		}
	}
	if v, ok := env["TEMPMAIL_LOL_RETRIES"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TempmailLOLRetries = n
		}
	}
	if v, ok := env["TEMPMAIL_LOL_MIN_INTERVAL_MS"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TempmailLOLIntervalMS = n
		}
	}
	if v, ok := env["HTTPS_PROXY"]; ok {
		cfg.HTTPSProxy = v
	}
	if v, ok := env["HTTP_PROXY"]; ok {
		cfg.HTTPProxy = v
	}
	if v, ok := env["NO_PROXY"]; ok {
		cfg.NoProxy = v
	}
	if v, ok := env["RESIN_PROXY"]; ok {
		cfg.ResinProxy = v
	}
	if v, ok := env["RESIN_TOKEN"]; ok {
		cfg.ResinToken = v
	}
	if v, ok := env["RESIN_PLATFORM"]; ok {
		cfg.ResinPlatform = v
	}
	if v, ok := env["MAIL_ROUTER_URL"]; ok {
		cfg.MailRouterURL = v
	}
	if v, ok := env["MAIL_ROUTER_API_KEY"]; ok {
		cfg.MailRouterAPIKey = v
	}
	if v, ok := env["MAIL_ROUTER_DOMAIN"]; ok {
		cfg.MailRouterDomain = v
	}
	if v, ok := env["PROBE_ENABLED"]; ok {
		cfg.ProbeEnabled = truthy(v)
	}
	if v, ok := env["PHYSICAL_CAP"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PhysicalCap = n
		}
	}
	if v, ok := env["CPA_UPLOAD_ENABLED"]; ok {
		cfg.CPAUploadEnabled = truthy(v)
	}
	if v, ok := env["CPA_MANAGEMENT_BASE"]; ok {
		cfg.CPAManagementBase = v
	}
	if v, ok := env["CPA_MANAGEMENT_KEY"]; ok {
		cfg.CPAManagementKey = v
	}
	if v, ok := env["CPA_UPLOAD_TIMEOUT_SEC"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.CPAUploadTimeoutSec = n
		}
	}
	if v, ok := env["CPA_UPLOAD_RETRIES"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.CPAUploadRetries = n
		}
	}
	if v, ok := env["CPA_UPLOAD_NAME_TEMPLATE"]; ok {
		cfg.CPAUploadNameTemplate = v
	}
	if v, ok := env["CPA_UPLOAD_VERIFY"]; ok {
		cfg.CPAUploadVerify = truthy(v)
	}
	if v, ok := env["CPA_UPLOAD_MODE"]; ok {
		cfg.CPAUploadMode = v
	}
	if v, ok := env["UPLOAD_CONCURRENCY"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.UploadConcurrency = n
		}
	}
	if v, ok := env["UPLOAD_BATCH_SIZE"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.UploadBatchSize = n
		}
	}
	if v, ok := env["EXPORT_BATCH_SIZE"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ExportBatchSize = n
		}
	}
	if v, ok := env["EXPORT_CONCURRENCY"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ExportConcurrency = n
		}
	}
	if v, ok := env["PATROL_ENABLED"]; ok {
		cfg.PatrolEnabled = truthy(v)
	}
	if v, ok := env["PATROL_INTERVAL_MIN"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PatrolIntervalMin = n
		}
	}
	if v, ok := env["PATROL_DEEP_PROBE"]; ok {
		cfg.PatrolDeepProbe = truthy(v)
	}
	if v, ok := env["PATROL_CONCURRENCY"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PatrolConcurrency = n
		}
	}
	if v, ok := env["QUOTA_PER_ACCOUNT"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.QuotaPerAccount = n
		}
	}
	if v, ok := env["DEGRADE_MODEL"]; ok && strings.TrimSpace(v) != "" {
		cfg.DegradeModel = strings.TrimSpace(v)
	}
	if v, ok := env["DEGRADE_SAMPLE"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DegradeSample = n
		}
	}
	if v, ok := env["DEGRADE_RECHECK_MIN"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DegradeRecheckMin = n
		}
	}
	if v, ok := env["DEGRADE_EXIT_WINDOW_MIN"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DegradeExitWindowMin = n
		}
	}
	if v, ok := env["DEGRADE_EXIT_ACCOUNT_CAP"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DegradeExitAccountCap = n
		}
	}
	if v, ok := env["REFILL_ENABLED"]; ok {
		cfg.RefillEnabled = truthy(v)
	}
	if v, ok := env["REFILL_MIN_HEALTHY"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RefillMinHealthy = n
		}
	}
	if v, ok := env["REFILL_BATCH"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RefillBatch = n
		}
	}
	if v, ok := env["REFILL_COOLDOWN_MIN"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RefillCooldownMin = n
		}
	}
	if v, ok := env["REFILL_DAILY_CAP"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RefillDailyCap = n
		}
	}
	if v, ok := env["CLEANUP_QUOTA_ENABLED"]; ok {
		cfg.CleanupQuotaEnabled = truthy(v)
	}
	if v, ok := env["CLEANUP_ON_PATROL"]; ok {
		cfg.CleanupOnPatrol = truthy(v)
	}
	if v, ok := env["CLEANUP_BACKUP"]; ok {
		cfg.CleanupBackup = truthy(v)
	}
	if v, ok := env["CLEANUP_DRY_RUN"]; ok {
		cfg.CleanupDryRun = truthy(v)
	}
	if v, ok := env["CLUSTER_ROLE"]; ok {
		cfg.ClusterRole = strings.ToLower(strings.TrimSpace(v))
	}
	if v, ok := env["CLUSTER_NODE_NAME"]; ok {
		cfg.ClusterNodeName = v
	}
	if v, ok := env["CLUSTER_PUBLIC_TOKEN"]; ok {
		cfg.ClusterPublicToken = v
	}
	if v, ok := env["CLUSTER_MASTER_URL"]; ok {
		cfg.ClusterMasterURL = strings.TrimRight(strings.TrimSpace(v), "/")
	}
	if v, ok := env["CLUSTER_MASTER_URLS"]; ok {
		cfg.ClusterMasterURLs = v
	}
	if v, ok := env["CLUSTER_STATUS_PASSWORD"]; ok {
		cfg.ClusterStatusPassword = v
	}
	if v, ok := env["CLUSTER_HEARTBEAT_SEC"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ClusterHeartbeatSec = n
		}
	}
	if v, ok := env["CLUSTER_POOL_TARGET"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ClusterPoolTarget = n
		}
	}
	if v, ok := env["CLUSTER_ASSIGN_MIN"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ClusterAssignMin = n
		}
	}
	if v, ok := env["CLUSTER_ASSIGN_MAX"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ClusterAssignMax = n
		}
	}
	if v, ok := env["CLUSTER_AUTO_REGISTER"]; ok {
		cfg.ClusterAutoRegister = truthy(v)
	}
	if v, ok := env["CLUSTER_AUTO_UPLOAD"]; ok {
		cfg.ClusterAutoUpload = truthy(v)
	}
	if v, ok := env["CLUSTER_SHARE_POOL_LIST"]; ok {
		cfg.ClusterSharePoolList = truthy(v)
	}
	if v, ok := env["CLUSTER_SHARE_POOL_PULL"]; ok {
		cfg.ClusterSharePoolPull = truthy(v)
	}
	if v, ok := env["CLUSTER_SHARE_INFRASTRUCTURE"]; ok {
		cfg.ClusterShareInfrastructure = truthy(v)
	}
	if v, ok := env["LOCAL_POOL_AUTO_IMPORT"]; ok {
		cfg.LocalPoolAutoImport = truthy(v)
	}
	if v, ok := env["LOCAL_POOL_AUTO_SYNC"]; ok {
		cfg.LocalPoolAutoSync = truthy(v)
	}
}

func truthy(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func bool01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// ApplyProxyEnv sets process proxy env for outbound HTTP (tempmail etc).
func ApplyProxyEnv(cfg Config) {
	if cfg.HTTPProxy != "" {
		_ = os.Setenv("HTTP_PROXY", cfg.HTTPProxy)
		_ = os.Setenv("http_proxy", cfg.HTTPProxy)
	}
	if cfg.HTTPSProxy != "" {
		_ = os.Setenv("HTTPS_PROXY", cfg.HTTPSProxy)
		_ = os.Setenv("https_proxy", cfg.HTTPSProxy)
	}
	if cfg.NoProxy != "" {
		_ = os.Setenv("NO_PROXY", cfg.NoProxy)
		_ = os.Setenv("no_proxy", cfg.NoProxy)
	}
}

// MasterEndpoint is one federation master a slave may connect to.
// Token overrides the global ClusterPublicToken when non-empty.
type MasterEndpoint struct {
	URL   string `json:"url"`
	Token string `json:"token,omitempty"`
}

// ClusterMasterEndpoints returns de-duplicated master endpoints for a slave.
// CLUSTER_MASTER_URLS accepts:
//   - plain URLs separated by comma/newline/space
//   - JSON array: [{"url":"https://m","token":"secret"}]
//   - lines "url|token"
//
// Falls back to CLUSTER_MASTER_URL; empty token falls back to ClusterPublicToken at call site.
func (cfg Config) ClusterMasterEndpoints() []MasterEndpoint {
	raw := strings.TrimSpace(cfg.ClusterMasterURLs)
	if raw == "" {
		raw = strings.TrimSpace(cfg.ClusterMasterURL)
	}
	if raw == "" {
		return nil
	}
	// JSON array form
	if strings.HasPrefix(raw, "[") {
		var arr []MasterEndpoint
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			return dedupeMasterEndpoints(arr)
		}
	}
	// UI may send real newlines or the two-char sequence \n
	raw = strings.ReplaceAll(raw, `\n`, "\n")
	raw = strings.ReplaceAll(raw, `\r`, "\r")
	// Prefer line-oriented parse so "url|token" works
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	var out []MasterEndpoint
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var u, tok string
		if i := strings.IndexAny(line, "|\t"); i >= 0 {
			u = strings.TrimSpace(line[:i])
			tok = strings.TrimSpace(line[i+1:])
		} else {
			parts := strings.Fields(line)
			if len(parts) == 0 {
				continue
			}
			u = parts[0]
			if len(parts) > 1 {
				tok = parts[1]
			}
		}
		u = strings.TrimRight(u, "/")
		if u == "" {
			continue
		}
		out = append(out, MasterEndpoint{URL: u, Token: tok})
	}
	return dedupeMasterEndpoints(out)
}

// ClusterMasters returns de-duplicated master base URLs (legacy).
func (cfg Config) ClusterMasters() []string {
	eps := cfg.ClusterMasterEndpoints()
	out := make([]string, 0, len(eps))
	for _, e := range eps {
		out = append(out, e.URL)
	}
	return out
}

// FormatMasterEndpoints serializes endpoints as JSON for CLUSTER_MASTER_URLS.
func FormatMasterEndpoints(eps []MasterEndpoint) string {
	eps = dedupeMasterEndpoints(eps)
	if len(eps) == 0 {
		return ""
	}
	b, err := json.Marshal(eps)
	if err != nil {
		var urls []string
		for _, e := range eps {
			urls = append(urls, e.URL)
		}
		return strings.Join(urls, ",")
	}
	return string(b)
}

func dedupeMasterEndpoints(in []MasterEndpoint) []MasterEndpoint {
	seen := map[string]struct{}{}
	var out []MasterEndpoint
	for _, e := range in {
		u := strings.TrimRight(strings.TrimSpace(e.URL), "/")
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, MasterEndpoint{URL: u, Token: strings.TrimSpace(e.Token)})
	}
	return out
}
