package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/home"
)

func TestConfigAPIRejectsNewlineInjection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvHome, dir)
	paths, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureBase(); err != nil {
		t.Fatal(err)
	}
	if err := saveConfigWithSecrets(paths.Config, config.Defaults()); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Paths: paths})
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"duckmail_base":"https://duck.example.com\nRESIN_TOKEN=injected"}`))
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestConfigAPIEmailProviderAndBridgeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(home.EnvHome, dir)
	paths, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureBase(); err != nil {
		t.Fatal(err)
	}
	if err := saveConfigWithSecrets(paths.Config, config.Defaults()); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Paths: paths})

	body := map[string]any{
		"email_mode":                   "cloudmail",
		"duckmail_base":                "https://duck.example.com/",
		"duckmail_key":                 "duck-secret",
		"cloudflare_base":              "https://worker.example.com/",
		"cloudflare_key":               "cloudflare-secret",
		"cloudmail_url":                "https://cloudmail.example.com/",
		"cloudmail_password":           "cloudmail-secret",
		"moemail_base":                 "https://moemail.example.com/",
		"moemail_key":                  "moemail-secret",
		"bridge_reg_factory_root":      "/opt/reg-factory",
		"bridge_grok_panel_root":       "/opt/grok-panel",
		"bridge_outlook_pool_dir":      "/data/outlook-pool",
		"bridge_python":                "/opt/python",
		"turnstile_chrome_path":        "/opt/chrome",
		"turnstile_python":             "/opt/turnstile-python",
		"turnstile_script":             "/opt/turnstile_mint.py",
		"turnstile_inject_clearance":   true,
		"oauth_min_interval_sec":       12.5,
		"oauth_retry_sec":              75,
		"tempmail_lol_retries":         44,
		"tempmail_lol_min_interval_ms": 1800,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(raw))
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", res.Code, res.Body.String())
	}

	got, err := config.Load(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if got.EmailMode != config.EmailCloudMail || got.DuckMailKey != "duck-secret" || got.CloudflareKey != "cloudflare-secret" || got.CloudMailPassword != "cloudmail-secret" || got.MoeMailKey != "moemail-secret" {
		t.Fatalf("provider secrets not persisted: %+v", got)
	}
	if got.BridgeRegFactoryRoot != "/opt/reg-factory" || got.BridgeGrokPanelRoot != "/opt/grok-panel" || got.BridgeOutlookPoolDir != "/data/outlook-pool" || got.BridgePythonExe != "/opt/python" {
		t.Fatalf("bridge configuration mismatch: %+v", got)
	}
	if got.TurnstileChromePath != "/opt/chrome" || got.TurnstilePython != "/opt/turnstile-python" || got.TurnstileScript != "/opt/turnstile_mint.py" || !got.TurnstileInjectClearance {
		t.Fatalf("turnstile configuration mismatch: %+v", got)
	}
	if got.OAuthMinIntervalSec != 12.5 || got.OAuthRetrySec != 75 || got.TempmailLOLRetries != 44 || got.TempmailLOLIntervalMS != 1800 {
		t.Fatalf("runtime settings mismatch: %+v", got)
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/config", nil)
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", res.Code, res.Body.String())
	}
	payload := res.Body.String()
	for _, secret := range []string{"duck-secret", "cloudflare-secret", "cloudmail-secret", "moemail-secret"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("secret leaked from config response: %s", secret)
		}
	}
	if !strings.Contains(payload, `"duckmail_key_set":true`) || !strings.Contains(payload, `"cloudmail_password_set":true`) {
		t.Fatalf("secret status not exposed: %s", payload)
	}
}
