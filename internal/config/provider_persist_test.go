package config

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadEmailProvidersAndBridgeSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.env")
	cfg := Defaults()
	cfg.EmailMode = EmailCloudMail
	cfg.EmailDefaultDomains = "mail.example.com,alt.example.com"
	cfg.DuckMailBase = "https://duck.example.com"
	cfg.DuckMailKey = "duck-secret"
	cfg.CloudflareBase = "https://worker.example.com"
	cfg.CloudflareKey = "cloudflare-secret"
	cfg.CloudflareAuthMode = "x-api-key"
	cfg.CloudflareCustomAuth = "custom-secret"
	cfg.CloudMailURL = "https://cloudmail.example.com"
	cfg.CloudMailAdminEmail = "admin@example.com"
	cfg.CloudMailPassword = "cloudmail-secret"
	cfg.MailNestKey = "mailnest-secret"
	cfg.MoeMailBase = "https://moemail.example.com"
	cfg.MoeMailKey = "moemail-secret"
	cfg.MoeMailDomain = "moemail.example.com"
	cfg.YYDSKey = "yyds-key"
	cfg.YYDSJWT = "yyds-jwt"
	cfg.YYDSDomain = "yyds.example.com"
	cfg.BridgeRegFactoryRoot = "/opt/reg-factory"
	cfg.BridgeGrokPanelRoot = "/opt/grok-panel"
	cfg.BridgeOutlookPoolDir = "/data/outlook"
	cfg.BridgePythonExe = "/opt/python"
	cfg.OAuthMinIntervalSec = 12.5
	cfg.OAuthRetrySec = 75

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.EmailMode != cfg.EmailMode || got.DuckMailBase != cfg.DuckMailBase || got.CloudflareBase != cfg.CloudflareBase || got.CloudMailURL != cfg.CloudMailURL || got.MailNestProjectCode != cfg.MailNestProjectCode || got.MoeMailBase != cfg.MoeMailBase || got.YYDSDomain != cfg.YYDSDomain {
		t.Fatalf("provider config did not round-trip: %+v", got)
	}
	if got.BridgeRegFactoryRoot != cfg.BridgeRegFactoryRoot || got.BridgeGrokPanelRoot != cfg.BridgeGrokPanelRoot || got.BridgeOutlookPoolDir != cfg.BridgeOutlookPoolDir || got.BridgePythonExe != cfg.BridgePythonExe {
		t.Fatalf("bridge config did not round-trip: %+v", got)
	}
	if got.OAuthMinIntervalSec != cfg.OAuthMinIntervalSec || got.OAuthRetrySec != cfg.OAuthRetrySec {
		t.Fatalf("oauth config did not round-trip: %+v", got)
	}
}
