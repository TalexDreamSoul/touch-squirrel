package hunter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return r[strings.ToLower(host)], nil
}

func TestValidateTargetRequiresPublicInScopeAddress(t *testing.T) {
	scope, err := ParseScope([]string{"allowed.example"})
	if err != nil {
		t.Fatal(err)
	}
	p := NewProber(staticResolver{
		"allowed.example": {netip.MustParseAddr("8.8.8.8")},
		"private.example": {netip.MustParseAddr("127.0.0.1")},
	})
	if _, err := p.validateTarget(context.Background(), "https://allowed.example", scope, false); err != nil {
		t.Fatalf("allowed target rejected: %v", err)
	}
	if _, err := p.validateTarget(context.Background(), "https://private.example", scope, false); err == nil {
		t.Fatal("private target accepted")
	}
	if _, err := p.validateTarget(context.Background(), "https://outside.example", scope, false); err == nil {
		t.Fatal("outside target accepted")
	}
	if _, err := p.validateTarget(context.Background(), "https://user:pass@allowed.example", scope, false); err == nil {
		t.Fatal("userinfo target accepted")
	}
	privateScope, err := ParseScope([]string{"private.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.validateTarget(context.Background(), "https://private.example", privateScope, true); err != nil {
		t.Fatalf("isolated mode rejected private target: %v", err)
	}
}

func TestCLIProxyHeaderFingerprint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-CPA-Version", "7.0.0")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	scope, err := ParseScope([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewProber(nil).Probe(context.Background(), srv.URL, "unknown", scope, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Product != "cliproxyapi" {
		t.Fatalf("product=%q", result.Product)
	}
}

func TestProbePathsAreFixedByProduct(t *testing.T) {
	got := ProbePaths("openai-compatible")
	if len(got) != 2 || got[0] != "/" || got[1] != "/v1/models" {
		t.Fatalf("paths=%v", got)
	}
	if got := ProbePaths("sub2api"); len(got) < 3 || got[0] != "/health" {
		t.Fatalf("sub2api paths=%v", got)
	}
	if got := ProbePaths("cliproxyapi"); len(got) < 4 || got[0] != "/healthz" {
		t.Fatalf("cliproxyapi paths=%v", got)
	}
	if got := ProbePaths("unknown"); len(got) < 4 {
		t.Fatalf("generic paths=%v", got)
	}
}
