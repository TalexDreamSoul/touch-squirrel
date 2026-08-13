package hunter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
)

func TestNetworkEnumerationSharesLimitAcrossCIDRs(t *testing.T) {
	hosts, err := enumerateNetworkHosts([]string{"10.0.0.0/24", "192.168.1.0/24"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 || hosts[0].String() != "10.0.0.0" || hosts[1].String() != "192.168.1.0" {
		t.Fatalf("hosts=%v", hosts)
	}
}

func TestDiscoverNetworkFindsCLIProxyAPIOnLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/healthz":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "CLI Proxy API Server", "status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())

	svc := NewService(filepath.Join(t.TempDir(), "hunter.json"))
	cfg := DefaultConfig()
	cfg.DiscoveryCIDRs = []string{"127.0.0.1/32"}
	cfg.DiscoveryPorts = []int{port}
	cfg.MaxDiscoveryHosts = 4
	cfg.DiscoveryConcurrency = 2
	cfg.DiscoveryTimeoutMS = 200
	if err := svc.Store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	report, err := svc.DiscoverNetwork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Imported != 1 || report.OpenPorts != 1 {
		t.Fatalf("report=%+v", report)
	}
	snap, err := svc.Store.Snapshot(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Findings) != 1 || snap.Findings[0].Product != "cliproxyapi" || !snap.Findings[0].InScope {
		t.Fatalf("findings=%+v", snap.Findings)
	}
}

func TestRepeatedNetworkDiscoverySkipsRecentCredentialAudit(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/health":
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "Sub2API", "status": "ok"})
		case "/setup/status":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": map[string]interface{}{"needs_setup": false, "step": "completed"}})
		case "/api/v1/auth/login":
			attempts++
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": map[string]string{"access_token": "discard"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	svc := NewService(filepath.Join(t.TempDir(), "hunter.json"))
	cfg := DefaultConfig()
	cfg.AutoDiscoverNetwork = false
	cfg.DiscoveryCIDRs = []string{"127.0.0.1/32"}
	cfg.DiscoveryPorts = []int{port}
	cfg.DiscoveryConcurrency = 1
	cfg.MaxDiscoveryHosts = 1
	if err := svc.Store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	first, err := svc.DiscoverNetwork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DiscoverNetwork(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		snap, _ := svc.Store.Snapshot(true)
		t.Fatalf("credential attempts=%d want 1 report=%+v findings=%+v", attempts, first, snap.Findings)
	}
}

func TestSub2APIDefaultCredentialAuditIsBounded(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			http.NotFound(w, r)
			return
		}
		attempts++
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] == "admin@sub2api.local" && body["password"] == "admin123" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 0, "data": map[string]string{"access_token": "do-not-store"}})
			return
		}
		http.Error(w, "invalid", http.StatusUnauthorized)
	}))
	defer srv.Close()
	scope, err := ParseScope([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	p := NewProber(nil)
	evidence, err := p.AuditDefaultCredentials(context.Background(), srv.URL, "sub2api", scope, true)
	if err != nil {
		t.Fatal(err)
	}
	if attempts > 2 {
		t.Fatalf("too many attempts: %d", attempts)
	}
	if len(evidence) != 1 || evidence[0].Kind != "default_credential" {
		t.Fatalf("evidence=%+v", evidence)
	}
	if evidence[0].Redacted != "admin@sub2api.local:****" {
		t.Fatalf("redacted=%q", evidence[0].Redacted)
	}
	if !scope.Allows("127.0.0.1", netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("test scope mismatch")
	}
}
