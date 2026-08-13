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

const infrastructureImportVersion = 1

// infrastructureConfig is the stable exchange contract for Resin and
// TouchMailRouter. It is intentionally separate from the broader panel config
// so import links never have to carry unrelated settings.
type infrastructureConfig struct {
	Version int `json:"version"`

	ResinProxy    string `json:"resin_proxy,omitempty"`
	ResinToken    string `json:"resin_token,omitempty"`
	ResinPlatform string `json:"resin_platform,omitempty"`

	MailRouterURL    string `json:"mail_router_url,omitempty"`
	MailRouterAPIKey string `json:"mail_router_api_key,omitempty"`
	MailRouterDomain string `json:"mail_router_domain,omitempty"`
}

func infrastructureFromConfig(cfg config.Config, includeSecrets bool) infrastructureConfig {
	out := infrastructureConfig{
		Version:          infrastructureImportVersion,
		ResinProxy:       strings.TrimSpace(cfg.ResinProxy),
		ResinPlatform:    strings.TrimSpace(cfg.ResinPlatform),
		MailRouterURL:    strings.TrimSpace(cfg.MailRouterURL),
		MailRouterDomain: strings.TrimSpace(cfg.MailRouterDomain),
	}
	if includeSecrets {
		out.ResinToken = cfg.ResinToken
		out.MailRouterAPIKey = cfg.MailRouterAPIKey
	}
	return out
}

func (in infrastructureConfig) validate() error {
	if in.Version != 0 && in.Version != infrastructureImportVersion {
		return fmt.Errorf("unsupported infrastructure import version %d", in.Version)
	}
	if err := validateProxyURL("resin_proxy", in.ResinProxy); err != nil {
		return err
	}
	if err := validateHTTPURL("mail_router_url", in.MailRouterURL); err != nil {
		return err
	}
	if err := validateSecretInput("resin_token", in.ResinToken); err != nil {
		return err
	}
	if err := validateSecretInput("mail_router_api_key", in.MailRouterAPIKey); err != nil {
		return err
	}
	if strings.ContainsAny(in.ResinPlatform, "\r\n") {
		return fmt.Errorf("resin_platform must not contain a newline")
	}
	if strings.ContainsAny(in.MailRouterDomain, "\r\n") {
		return fmt.Errorf("mail_router_domain must not contain a newline")
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
	if v := strings.TrimSpace(in.ResinProxy); v != "" {
		cfg.ResinProxy = v
	}
	if v := strings.TrimSpace(in.ResinToken); v != "" {
		cfg.ResinToken = v
	}
	if v := strings.TrimSpace(in.ResinPlatform); v != "" {
		cfg.ResinPlatform = v
	}
	if v := strings.TrimSpace(in.MailRouterURL); v != "" {
		cfg.MailRouterURL = v
	}
	if v := strings.TrimSpace(in.MailRouterAPIKey); v != "" {
		cfg.MailRouterAPIKey = v
	}
	if v := strings.TrimSpace(in.MailRouterDomain); v != "" {
		cfg.MailRouterDomain = v
	}
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
