package api

import (
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
		got.MailRouterAPIKey != cfg.MailRouterAPIKey {
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
