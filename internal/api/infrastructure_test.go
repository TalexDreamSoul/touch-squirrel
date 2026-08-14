package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/home"
)

func TestInfrastructureConfigSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	cfg := config.Defaults()
	cfg.ResinProxy = "http://resin.internal:2260"
	cfg.ResinToken = "resin-secret"
	cfg.ResinPlatform = "Grok"
	cfg.MailRouterURL = "https://mail.example.com"
	cfg.MailRouterAPIKey = "mail-secret"
	cfg.MailRouterDomain = "inbound.example.com"
	if err := saveInfrastructureConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResinProxy != cfg.ResinProxy || got.ResinToken != cfg.ResinToken || got.ResinPlatform != cfg.ResinPlatform {
		t.Fatalf("resin config mismatch: %+v", got)
	}
	if got.MailRouterURL != cfg.MailRouterURL || got.MailRouterAPIKey != cfg.MailRouterAPIKey || got.MailRouterDomain != cfg.MailRouterDomain {
		t.Fatalf("mail router config mismatch: %+v", got)
	}
}

func TestSaveConfigWithSecretsPreservesAllSecretTypes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	cfg := config.Defaults()
	cfg.CPAManagementKey = "cpa-secret"
	cfg.ClusterPublicToken = "cluster-secret"
	cfg.ClusterStatusPassword = "status-secret"
	cfg.ResinToken = "resin-secret"
	cfg.MailRouterAPIKey = "mail-secret"
	cfg.DuckMailKey = "duck-secret"
	cfg.CloudflareKey = "cloudflare-secret"
	cfg.CloudflareCustomAuth = "custom-auth"
	cfg.CloudMailPassword = "cloudmail-secret"
	cfg.MailNestKey = "mailnest-secret"
	cfg.MoeMailKey = "moemail-secret"
	cfg.YYDSKey = "yyds-secret"
	cfg.YYDSJWT = "yyds-jwt"
	if err := saveConfigWithSecrets(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.CPAManagementKey != cfg.CPAManagementKey ||
		got.ClusterPublicToken != cfg.ClusterPublicToken ||
		got.ClusterStatusPassword != cfg.ClusterStatusPassword ||
		got.ResinToken != cfg.ResinToken ||
		got.MailRouterAPIKey != cfg.MailRouterAPIKey ||
		got.DuckMailKey != cfg.DuckMailKey ||
		got.CloudflareKey != cfg.CloudflareKey ||
		got.CloudflareCustomAuth != cfg.CloudflareCustomAuth ||
		got.CloudMailPassword != cfg.CloudMailPassword ||
		got.MailNestKey != cfg.MailNestKey ||
		got.MoeMailKey != cfg.MoeMailKey ||
		got.YYDSKey != cfg.YYDSKey ||
		got.YYDSJWT != cfg.YYDSJWT {
		t.Fatalf("secret preservation mismatch: %+v", got)
	}
}

func TestConfiguredMasterAccessUsesExistingEndpointToken(t *testing.T) {
	cfg := config.Defaults()
	cfg.ClusterPublicToken = "global-token"
	cfg.ClusterMasterURLs = `[{"url":"https://master.example.com","token":"endpoint-token"}]`
	master, token := configuredMasterAccess(cfg, "", "")
	if master != "https://master.example.com" || token != "endpoint-token" {
		t.Fatalf("master=%q token=%q", master, token)
	}
}

func TestFederationInfrastructureRequiresExplicitShare(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvHome, dir)
	paths, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureBase(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.ClusterRole = "master"
	cfg.ClusterPublicToken = "cluster-secret"
	cfg.ResinProxy = "http://resin.internal:2260"
	cfg.ResinToken = "resin-secret"
	if err := saveInfrastructureConfig(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Paths: paths})

	req := httptest.NewRequest(http.MethodGet, "/api/federation/infrastructure", nil)
	req.Header.Set("X-Cluster-Token", "cluster-secret")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	cfg.ClusterShareInfrastructure = true
	if err := saveInfrastructureConfig(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	res = httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if got := res.Body.String(); !containsString(got, "resin-secret") {
		t.Fatalf("shared secret absent from response: %s", got)
	}
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestInfrastructureExportV2RedactsSecretsAndPreservesProfile(t *testing.T) {
	cfg := completeInfrastructureConfig()
	paths := writeInfrastructureTestConfig(t, cfg)
	s := New(Options{Paths: paths, Token: "panel-token"})

	req := httptest.NewRequest(http.MethodGet, "/api/infrastructure/export", nil)
	req.Header.Set("X-Panel-Token", "panel-token")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	profile := decodeInfrastructureProfile(t, res.Body.Bytes())
	if got := profile["version"]; got != float64(2) {
		t.Fatalf("version=%v, want v2", got)
	}
	assertProfileFields(t, profile, map[string]string{
		"email_mode":             "cloudmail",
		"email_domain":           "mail.example.com",
		"email_api":              "https://email.example.com/api",
		"email_default_domains":  "one.example.com,two.example.com",
		"duckmail_base":          "https://duckmail.example.com",
		"cloudflare_base":        "https://cloudflare.example.com",
		"cloudflare_auth_mode":   "bearer",
		"cloudmail_url":          "https://cloudmail.example.com",
		"cloudmail_admin_email":  "admin@example.com",
		"mailnest_project_code":  "project-42",
		"moemail_base":           "https://moemail.example.com",
		"moemail_domain":         "moemail.example.com",
		"yyds_domain":            "yyds.example.com",
		"register_proxy":         "http://register-proxy.example.com:8080",
		"flaresolverr_url":       "https://flare.example.com",
		"clearance_proxy":        "socks5://clearance-proxy.example.com:1080",
		"clearance_urls":         "https://accounts.example.com,https://auth.example.com",
		"turnstile_provider":     "resin",
		"lite_solver_url":        "https://lite.example.com",
		"http_proxy":             "http://http-proxy.example.com:8080",
		"https_proxy":            "http://https-proxy.example.com:8080",
		"no_proxy":               "localhost,127.0.0.1",
		"resin_proxy":            "https://resin.example.com",
		"resin_platform":         "TouchMail",
		"mail_router_url":        "https://touchmail.example.com",
		"mail_router_domain":     "router.example.com",
	})
	assertSecretsAbsent(t, profile, res.Body.String(), completeInfrastructureSecrets())
}

func TestFederationInfrastructureV2SharesServiceSecretsOnly(t *testing.T) {
	cfg := completeInfrastructureConfig()
	cfg.ClusterRole = "master"
	cfg.ClusterShareInfrastructure = true
	paths := writeInfrastructureTestConfig(t, cfg)
	s := New(Options{Paths: paths})

	req := httptest.NewRequest(http.MethodGet, "/api/federation/infrastructure", nil)
	req.Header.Set("X-Cluster-Token", cfg.ClusterPublicToken)
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}

	profile := decodeInfrastructureProfile(t, res.Body.Bytes())
	assertProfileFields(t, profile, map[string]string{
		"resin_token":             "resin-secret",
		"mail_router_api_key":     "touchmail-secret",
		"duckmail_key":            "duckmail-secret",
		"cloudflare_key":          "cloudflare-secret",
		"cloudflare_custom_auth":  "cloudflare-custom-secret",
		"cloudmail_password":      "cloudmail-secret",
		"mailnest_key":            "mailnest-secret",
		"moemail_key":             "moemail-secret",
		"yyds_key":                "yyds-secret",
		"yyds_jwt":                "yyds-jwt-secret",
	})
	assertSecretsAbsent(t, profile, res.Body.String(), map[string]string{
		"cpa_management_key":      cfg.CPAManagementKey,
		"cluster_public_token":     cfg.ClusterPublicToken,
		"cluster_status_password":  cfg.ClusterStatusPassword,
	})
}

func TestInfrastructureV2ApplyWritesMailAndProxyConfiguration(t *testing.T) {
	cfg := config.Config{}
	in := infrastructureConfig{
		Version:               2,
		EmailMode:             "CLOUDMAIL",
		EmailDomain:           "mail.example.com",
		EmailAPI:              "https://email.example.com/api",
		EmailDefaultDomains:   "one.example.com,two.example.com",
		DuckMailBase:          "https://duckmail.example.com/",
		DuckMailKey:           "duckmail-secret",
		CloudflareBase:        "https://cloudflare.example.com/",
		CloudflareKey:         "cloudflare-secret",
		CloudflareAuthMode:    "BEARER",
		CloudflareCustomAuth:  "cloudflare-custom-secret",
		CloudMailURL:          "https://cloudmail.example.com/",
		CloudMailAdminEmail:   "admin@example.com",
		CloudMailPassword:     "cloudmail-secret",
		MailNestProjectCode:   "project-42",
		MailNestKey:           "mailnest-secret",
		MoeMailBase:           "https://moemail.example.com/",
		MoeMailDomain:         "moemail.example.com",
		MoeMailKey:            "moemail-secret",
		YYDSDomain:            "yyds.example.com",
		YYDSKey:               "yyds-secret",
		YYDSJWT:               "yyds-jwt-secret",
		RegisterProxy:         "http://register-proxy.example.com:8080",
		FlareSolverrURL:       "https://flare.example.com",
		ClearanceProxy:        "socks5://clearance-proxy.example.com:1080",
		ClearanceURLs:         "https://accounts.example.com,https://auth.example.com",
		TurnstileProvider:     "resin",
		LiteSolverURL:         "https://lite.example.com/",
		HTTPProxy:             "http://http-proxy.example.com:8080",
		HTTPSProxy:            "http://https-proxy.example.com:8080",
		NoProxy:               "localhost,127.0.0.1",
		ResinProxy:            "https://resin.example.com",
		ResinToken:            "resin-secret",
		ResinPlatform:         "TouchMail",
		MailRouterURL:         "https://touchmail.example.com/",
		MailRouterAPIKey:      "touchmail-secret",
		MailRouterDomain:      "router.example.com",
	}
	if err := in.validate(); err != nil {
		t.Fatalf("v2 profile rejected: %v", err)
	}
	in.apply(&cfg)

	if cfg.EmailMode != config.EmailCloudMail || cfg.EmailDomain != "mail.example.com" || cfg.EmailAPI != "https://email.example.com/api" || cfg.EmailDefaultDomains != "one.example.com,two.example.com" {
		t.Fatalf("email configuration not applied: %+v", cfg)
	}
	if cfg.DuckMailBase != "https://duckmail.example.com" || cfg.DuckMailKey != "duckmail-secret" || cfg.CloudflareBase != "https://cloudflare.example.com" || cfg.CloudflareKey != "cloudflare-secret" || cfg.CloudflareAuthMode != "bearer" || cfg.CloudflareCustomAuth != "cloudflare-custom-secret" {
		t.Fatalf("mail provider configuration not applied: %+v", cfg)
	}
	if cfg.CloudMailURL != "https://cloudmail.example.com" || cfg.CloudMailAdminEmail != "admin@example.com" || cfg.CloudMailPassword != "cloudmail-secret" || cfg.MailNestProjectCode != "project-42" || cfg.MailNestKey != "mailnest-secret" || cfg.MoeMailBase != "https://moemail.example.com" || cfg.MoeMailDomain != "moemail.example.com" || cfg.MoeMailKey != "moemail-secret" || cfg.YYDSDomain != "yyds.example.com" || cfg.YYDSKey != "yyds-secret" || cfg.YYDSJWT != "yyds-jwt-secret" {
		t.Fatalf("provider credentials not applied: %+v", cfg)
	}
	if cfg.RegisterProxy != "http://register-proxy.example.com:8080" || cfg.FlareSolverrURL != "https://flare.example.com" || cfg.ClearanceProxy != "socks5://clearance-proxy.example.com:1080" || cfg.ClearanceURLs != "https://accounts.example.com,https://auth.example.com" || cfg.TurnstileProvider != "resin" || cfg.LiteSolverURL != "https://lite.example.com" || cfg.HTTPProxy != "http://http-proxy.example.com:8080" || cfg.HTTPSProxy != "http://https-proxy.example.com:8080" || cfg.NoProxy != "localhost,127.0.0.1" {
		t.Fatalf("proxy and clearance configuration not applied: %+v", cfg)
	}
	if cfg.ResinProxy != "https://resin.example.com" || cfg.ResinToken != "resin-secret" || cfg.ResinPlatform != "TouchMail" || cfg.MailRouterURL != "https://touchmail.example.com" || cfg.MailRouterAPIKey != "touchmail-secret" || cfg.MailRouterDomain != "router.example.com" {
		t.Fatalf("TouchMail configuration not applied: %+v", cfg)
	}
}

func completeInfrastructureConfig() config.Config {
	cfg := config.Defaults()
	cfg.EmailMode = config.EmailCloudMail
	cfg.EmailDomain = "mail.example.com"
	cfg.EmailAPI = "https://email.example.com/api"
	cfg.EmailDefaultDomains = "one.example.com,two.example.com"
	cfg.DuckMailBase = "https://duckmail.example.com"
	cfg.DuckMailKey = "duckmail-secret"
	cfg.CloudflareBase = "https://cloudflare.example.com"
	cfg.CloudflareKey = "cloudflare-secret"
	cfg.CloudflareAuthMode = "bearer"
	cfg.CloudflareCustomAuth = "cloudflare-custom-secret"
	cfg.CloudMailURL = "https://cloudmail.example.com"
	cfg.CloudMailAdminEmail = "admin@example.com"
	cfg.CloudMailPassword = "cloudmail-secret"
	cfg.MailNestProjectCode = "project-42"
	cfg.MailNestKey = "mailnest-secret"
	cfg.MoeMailBase = "https://moemail.example.com"
	cfg.MoeMailDomain = "moemail.example.com"
	cfg.MoeMailKey = "moemail-secret"
	cfg.YYDSDomain = "yyds.example.com"
	cfg.YYDSKey = "yyds-secret"
	cfg.YYDSJWT = "yyds-jwt-secret"
	cfg.RegisterProxy = "http://register-proxy.example.com:8080"
	cfg.FlareSolverrURL = "https://flare.example.com"
	cfg.ClearanceProxy = "socks5://clearance-proxy.example.com:1080"
	cfg.ClearanceURLs = "https://accounts.example.com,https://auth.example.com"
	cfg.TurnstileProvider = "resin"
	cfg.LiteSolverURL = "https://lite.example.com"
	cfg.HTTPProxy = "http://http-proxy.example.com:8080"
	cfg.HTTPSProxy = "http://https-proxy.example.com:8080"
	cfg.NoProxy = "localhost,127.0.0.1"
	cfg.ResinProxy = "https://resin.example.com"
	cfg.ResinToken = "resin-secret"
	cfg.ResinPlatform = "TouchMail"
	cfg.MailRouterURL = "https://touchmail.example.com"
	cfg.MailRouterAPIKey = "touchmail-secret"
	cfg.MailRouterDomain = "router.example.com"
	cfg.CPAManagementKey = "cpa-secret"
	cfg.ClusterPublicToken = "cluster-secret"
	cfg.ClusterStatusPassword = "status-secret"
	return cfg
}

func completeInfrastructureSecrets() map[string]string {
	return map[string]string{
		"resin_token":             "resin-secret",
		"mail_router_api_key":     "touchmail-secret",
		"duckmail_key":            "duckmail-secret",
		"cloudflare_key":          "cloudflare-secret",
		"cloudflare_custom_auth":  "cloudflare-custom-secret",
		"cloudmail_password":      "cloudmail-secret",
		"mailnest_key":            "mailnest-secret",
		"moemail_key":             "moemail-secret",
		"yyds_key":                "yyds-secret",
		"yyds_jwt":                "yyds-jwt-secret",
		"cpa_management_key":      "cpa-secret",
		"cluster_public_token":    "cluster-secret",
		"cluster_status_password": "status-secret",
	}
}

func writeInfrastructureTestConfig(t *testing.T, cfg config.Config) home.Paths {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.EnvHome, dir)
	paths, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureBase(); err != nil {
		t.Fatal(err)
	}
	if err := saveInfrastructureConfig(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

func decodeInfrastructureProfile(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload struct {
		OK             bool           `json:"ok"`
		Infrastructure map[string]any `json:"infrastructure"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.OK {
		t.Fatalf("response not ok: %s", body)
	}
	return payload.Infrastructure
}

func assertProfileFields(t *testing.T, profile map[string]any, expected map[string]string) {
	t.Helper()
	for field, want := range expected {
		if got, ok := profile[field].(string); !ok || got != want {
			t.Fatalf("%s=%#v, want %q", field, profile[field], want)
		}
	}
}

func assertSecretsAbsent(t *testing.T, profile map[string]any, response string, secrets map[string]string) {
	t.Helper()
	for field, secret := range secrets {
		if _, found := profile[field]; found {
			t.Fatalf("secret field %q exposed", field)
		}
		if containsString(response, secret) {
			t.Fatalf("secret %q exposed", field)
		}
	}
}
