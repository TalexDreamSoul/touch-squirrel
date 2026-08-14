package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/config"
)

const infrastructureImportVersion = 2

// infrastructureConfig is the stable bootstrap exchange contract for mail
// providers, proxies and clearance services. It deliberately excludes
// account-pool, cluster, status, and local-path settings.
//
// Import links always use the redacted form. Authenticated federation may
// include the provider credentials needed to use the imported endpoints.
type infrastructureConfig struct {
	Version int `json:"version"`

	EmailMode           string `json:"email_mode,omitempty"`
	EmailDomain         string `json:"email_domain,omitempty"`
	EmailAPI            string `json:"email_api,omitempty"`
	EmailDefaultDomains string `json:"email_default_domains,omitempty"`

	DuckMailBase       string `json:"duckmail_base,omitempty"`
	CloudflareBase     string `json:"cloudflare_base,omitempty"`
	CloudflareAuthMode string `json:"cloudflare_auth_mode,omitempty"`
	CloudMailURL       string `json:"cloudmail_url,omitempty"`
	CloudMailAdminEmail string `json:"cloudmail_admin_email,omitempty"`
	MailNestProjectCode string `json:"mailnest_project_code,omitempty"`
	MoeMailBase        string `json:"moemail_base,omitempty"`
	MoeMailDomain      string `json:"moemail_domain,omitempty"`
	YYDSDomain         string `json:"yyds_domain,omitempty"`

	RegisterProxy     string `json:"register_proxy,omitempty"`
	FlareSolverrURL   string `json:"flaresolverr_url,omitempty"`
	ClearanceProxy    string `json:"clearance_proxy,omitempty"`
	ClearanceURLs     string `json:"clearance_urls,omitempty"`
	TurnstileProvider string `json:"turnstile_provider,omitempty"`
	LiteSolverURL     string `json:"lite_solver_url,omitempty"`
	HTTPProxy         string `json:"http_proxy,omitempty"`
	HTTPSProxy        string `json:"https_proxy,omitempty"`
	NoProxy           string `json:"no_proxy,omitempty"`

	ResinProxy    string `json:"resin_proxy,omitempty"`
	ResinToken    string `json:"resin_token,omitempty"`
	ResinPlatform string `json:"resin_platform,omitempty"`

	MailRouterURL    string `json:"mail_router_url,omitempty"`
	MailRouterAPIKey string `json:"mail_router_api_key,omitempty"`
	MailRouterDomain string `json:"mail_router_domain,omitempty"`

	DuckMailKey          string `json:"duckmail_key,omitempty"`
	CloudflareKey        string `json:"cloudflare_key,omitempty"`
	CloudflareCustomAuth string `json:"cloudflare_custom_auth,omitempty"`
	CloudMailPassword    string `json:"cloudmail_password,omitempty"`
	MailNestKey          string `json:"mailnest_key,omitempty"`
	MoeMailKey           string `json:"moemail_key,omitempty"`
	YYDSKey              string `json:"yyds_key,omitempty"`
	YYDSJWT              string `json:"yyds_jwt,omitempty"`
}

func infrastructureFromConfig(cfg config.Config, includeSecrets bool) infrastructureConfig {
	out := infrastructureConfig{
		Version:             infrastructureImportVersion,
		EmailMode:           string(cfg.EmailMode),
		EmailDomain:         strings.TrimSpace(cfg.EmailDomain),
		EmailAPI:            strings.TrimSpace(cfg.EmailAPI),
		EmailDefaultDomains: strings.TrimSpace(cfg.EmailDefaultDomains),
		DuckMailBase:        strings.TrimSpace(cfg.DuckMailBase),
		CloudflareBase:      strings.TrimSpace(cfg.CloudflareBase),
		CloudflareAuthMode:  strings.TrimSpace(cfg.CloudflareAuthMode),
		CloudMailURL:        strings.TrimSpace(cfg.CloudMailURL),
		CloudMailAdminEmail: strings.TrimSpace(cfg.CloudMailAdminEmail),
		MailNestProjectCode: strings.TrimSpace(cfg.MailNestProjectCode),
		MoeMailBase:         strings.TrimSpace(cfg.MoeMailBase),
		MoeMailDomain:       strings.TrimSpace(cfg.MoeMailDomain),
		YYDSDomain:          strings.TrimSpace(cfg.YYDSDomain),
		RegisterProxy:       strings.TrimSpace(cfg.RegisterProxy),
		FlareSolverrURL:     strings.TrimSpace(cfg.FlareSolverrURL),
		ClearanceProxy:      strings.TrimSpace(cfg.ClearanceProxy),
		ClearanceURLs:       strings.TrimSpace(cfg.ClearanceURLs),
		TurnstileProvider:   strings.TrimSpace(cfg.TurnstileProvider),
		LiteSolverURL:       strings.TrimSpace(cfg.LiteSolverURL),
		HTTPProxy:           strings.TrimSpace(cfg.HTTPProxy),
		HTTPSProxy:          strings.TrimSpace(cfg.HTTPSProxy),
		NoProxy:             strings.TrimSpace(cfg.NoProxy),
		ResinProxy:          strings.TrimSpace(cfg.ResinProxy),
		ResinPlatform:       strings.TrimSpace(cfg.ResinPlatform),
		MailRouterURL:       strings.TrimSpace(cfg.MailRouterURL),
		MailRouterDomain:    strings.TrimSpace(cfg.MailRouterDomain),
	}
	if includeSecrets {
		out.ResinToken = cfg.ResinToken
		out.MailRouterAPIKey = cfg.MailRouterAPIKey
		out.DuckMailKey = cfg.DuckMailKey
		out.CloudflareKey = cfg.CloudflareKey
		out.CloudflareCustomAuth = cfg.CloudflareCustomAuth
		out.CloudMailPassword = cfg.CloudMailPassword
		out.MailNestKey = cfg.MailNestKey
		out.MoeMailKey = cfg.MoeMailKey
		out.YYDSKey = cfg.YYDSKey
		out.YYDSJWT = cfg.YYDSJWT
	}
	return out
}

func (in infrastructureConfig) validate() error {
	if in.Version != 0 && in.Version != 1 && in.Version != infrastructureImportVersion {
		return fmt.Errorf("unsupported infrastructure import version %d", in.Version)
	}
	for _, field := range []struct {
		name  string
		value string
		check func(string, string) error
	}{
		{"email_api", in.EmailAPI, validateHTTPURL},
		{"duckmail_base", in.DuckMailBase, validateHTTPURL},
		{"cloudflare_base", in.CloudflareBase, validateHTTPURL},
		{"cloudmail_url", in.CloudMailURL, validateHTTPURL},
		{"moemail_base", in.MoeMailBase, validateHTTPURL},
		{"register_proxy", in.RegisterProxy, validateProxyURL},
		{"flaresolverr_url", in.FlareSolverrURL, validateHTTPURL},
		{"clearance_proxy", in.ClearanceProxy, validateProxyURL},
		{"lite_solver_url", in.LiteSolverURL, validateHTTPURL},
		{"http_proxy", in.HTTPProxy, validateProxyURL},
		{"https_proxy", in.HTTPSProxy, validateProxyURL},
		{"resin_proxy", in.ResinProxy, validateProxyURL},
		{"mail_router_url", in.MailRouterURL, validateHTTPURL},
	} {
		if err := field.check(field.name, field.value); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"resin_token", in.ResinToken}, {"mail_router_api_key", in.MailRouterAPIKey},
		{"duckmail_key", in.DuckMailKey}, {"cloudflare_key", in.CloudflareKey},
		{"cloudflare_custom_auth", in.CloudflareCustomAuth}, {"cloudmail_password", in.CloudMailPassword},
		{"mailnest_key", in.MailNestKey}, {"moemail_key", in.MoeMailKey},
		{"yyds_key", in.YYDSKey}, {"yyds_jwt", in.YYDSJWT},
	} {
		if err := validateSecretInput(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func validateProxyURL(name, raw string) error {
	return validateURL(name, raw, map[string]bool{
		"http": true, "https": true, "socks5": true, "socks5h": true,
	})
}

func validateHTTPURL(name, raw string) error {
	return validateURL(name, raw, map[string]bool{"http": true, "https": true})
}

func validateURL(name, raw string, allowed map[string]bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", name)
	}
	if u.User != nil {
		return fmt.Errorf("%s must not embed credentials", name)
	}
	if !allowed[strings.ToLower(u.Scheme)] {
		return fmt.Errorf("%s has unsupported scheme %q", name, u.Scheme)
	}
	return nil
}

func validateSecretInput(name, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must not contain a newline", name)
	}
	return nil
}

func (in infrastructureConfig) apply(cfg *config.Config) {
	applyString := func(value string, set func(string)) {
		if value = strings.TrimSpace(value); value != "" {
			set(value)
		}
	}
	applyString(in.EmailMode, func(v string) { cfg.EmailMode = config.EmailMode(strings.ToLower(v)) })
	applyString(in.EmailDomain, func(v string) { cfg.EmailDomain = v })
	applyString(in.EmailAPI, func(v string) { cfg.EmailAPI = v })
	applyString(in.EmailDefaultDomains, func(v string) { cfg.EmailDefaultDomains = v })
	applyString(in.DuckMailBase, func(v string) { cfg.DuckMailBase = strings.TrimRight(v, "/") })
	applyString(in.CloudflareBase, func(v string) { cfg.CloudflareBase = strings.TrimRight(v, "/") })
	applyString(in.CloudflareAuthMode, func(v string) { cfg.CloudflareAuthMode = strings.ToLower(v) })
	applyString(in.CloudMailURL, func(v string) { cfg.CloudMailURL = strings.TrimRight(v, "/") })
	applyString(in.CloudMailAdminEmail, func(v string) { cfg.CloudMailAdminEmail = v })
	applyString(in.MailNestProjectCode, func(v string) { cfg.MailNestProjectCode = v })
	applyString(in.MoeMailBase, func(v string) { cfg.MoeMailBase = strings.TrimRight(v, "/") })
	applyString(in.MoeMailDomain, func(v string) { cfg.MoeMailDomain = v })
	applyString(in.YYDSDomain, func(v string) { cfg.YYDSDomain = v })
	applyString(in.RegisterProxy, func(v string) { cfg.RegisterProxy = v })
	applyString(in.FlareSolverrURL, func(v string) { cfg.FlareSolverrURL = v })
	applyString(in.ClearanceProxy, func(v string) { cfg.ClearanceProxy = v })
	applyString(in.ClearanceURLs, func(v string) { cfg.ClearanceURLs = v })
	applyString(in.TurnstileProvider, func(v string) { cfg.TurnstileProvider = v })
	applyString(in.LiteSolverURL, func(v string) { cfg.LiteSolverURL = strings.TrimRight(v, "/") })
	applyString(in.HTTPProxy, func(v string) { cfg.HTTPProxy = v })
	applyString(in.HTTPSProxy, func(v string) { cfg.HTTPSProxy = v })
	applyString(in.NoProxy, func(v string) { cfg.NoProxy = v })
	applyString(in.ResinProxy, func(v string) { cfg.ResinProxy = v })
	applyString(in.ResinToken, func(v string) { cfg.ResinToken = v })
	applyString(in.ResinPlatform, func(v string) { cfg.ResinPlatform = v })
	applyString(in.MailRouterURL, func(v string) { cfg.MailRouterURL = strings.TrimRight(v, "/") })
	applyString(in.MailRouterAPIKey, func(v string) { cfg.MailRouterAPIKey = v })
	applyString(in.MailRouterDomain, func(v string) { cfg.MailRouterDomain = v })
	applyString(in.DuckMailKey, func(v string) { cfg.DuckMailKey = v })
	applyString(in.CloudflareKey, func(v string) { cfg.CloudflareKey = v })
	applyString(in.CloudflareCustomAuth, func(v string) { cfg.CloudflareCustomAuth = v })
	applyString(in.CloudMailPassword, func(v string) { cfg.CloudMailPassword = v })
	applyString(in.MailNestKey, func(v string) { cfg.MailNestKey = v })
	applyString(in.MoeMailKey, func(v string) { cfg.MoeMailKey = v })
	applyString(in.YYDSKey, func(v string) { cfg.YYDSKey = v })
	applyString(in.YYDSJWT, func(v string) { cfg.YYDSJWT = v })
}

func (s *Server) handleInfrastructureExport(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"infrastructure": infrastructureFromConfig(cfg, false),
	})
}

func (s *Server) handleFederationInfrastructure(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if code, msg := s.clusterAuthorize(cfg, federationToken(r)); code != 0 {
		writeJSON(w, code, map[string]any{"ok": false, "error": msg})
		return
	}
	if !cfg.ClusterShareInfrastructure {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"ok":    false,
			"error": "主节点未开启基础设施配置分享（CLUSTER_SHARE_INFRASTRUCTURE）",
		})
		return
	}
	if strings.TrimSpace(cfg.ClusterPublicToken) == "" {
		writeJSON(w, http.StatusConflict, map[string]any{
			"ok":    false,
			"error": "基础设施配置分享要求设置 CLUSTER_PUBLIC_TOKEN",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"source":         "federation",
		"infrastructure": infrastructureFromConfig(cfg, true),
	})
}

func (s *Server) handleInfrastructureImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source       string                `json:"source"`
		Import       *infrastructureConfig `json:"import"`
		MasterURL    string                `json:"master_url"`
		ClusterToken string                `json:"cluster_token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}

	var imported infrastructureConfig
	switch strings.ToLower(strings.TrimSpace(body.Source)) {
	case "", "link", "manual":
		if body.Import == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "missing import"})
			return
		}
		imported = *body.Import
	case "federation", "master":
		cfg, err := config.Load(s.opt.Paths.Config)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		master, token := configuredMasterAccess(cfg, body.MasterURL, body.ClusterToken)
		if err := validateHTTPURL("master_url", master); err != nil || master == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "master_url must be an absolute URL"})
			return
		}
		resp, err := getFederationInfrastructure(master, token)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		imported = resp
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "source must be link|federation"})
		return
	}
	if err := imported.validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	imported.apply(&cfg)
	if err := saveInfrastructureConfig(s.opt.Paths.Config, cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "imported": infrastructureFromConfig(cfg, false)})
}

func configuredMasterAccess(cfg config.Config, masterRaw, tokenRaw string) (string, string) {
	master := strings.TrimRight(strings.TrimSpace(masterRaw), "/")
	token := strings.TrimSpace(tokenRaw)
	endpoints := cfg.ClusterMasterEndpoints()
	if master == "" && len(endpoints) > 0 {
		master = endpoints[0].URL
	}
	if token == "" {
		for _, endpoint := range endpoints {
			if endpoint.URL == master && strings.TrimSpace(endpoint.Token) != "" {
				token = strings.TrimSpace(endpoint.Token)
				break
			}
		}
	}
	if token == "" {
		token = strings.TrimSpace(cfg.ClusterPublicToken)
	}
	return master, token
}

func getFederationInfrastructure(master, token string) (infrastructureConfig, error) {
	req, err := http.NewRequest(http.MethodGet, master+"/api/federation/infrastructure", nil)
	if err != nil {
		return infrastructureConfig{}, err
	}
	if token != "" {
		req.Header.Set("X-Cluster-Token", token)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return infrastructureConfig{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return infrastructureConfig{}, err
	}
	var payload struct {
		OK             bool                 `json:"ok"`
		Error          string               `json:"error"`
		Infrastructure infrastructureConfig `json:"infrastructure"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&payload); err != nil {
		return infrastructureConfig{}, fmt.Errorf("master response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || !payload.OK {
		if payload.Error == "" {
			payload.Error = fmt.Sprintf("master status %d", res.StatusCode)
		}
		return infrastructureConfig{}, fmt.Errorf("master: %s", payload.Error)
	}
	return payload.Infrastructure, nil
}

func saveInfrastructureConfig(path string, cfg config.Config) error {
	return saveConfigWithSecrets(path, cfg)
}
