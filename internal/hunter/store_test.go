package hunter

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDraftApprovalStateMachine(t *testing.T) {
	st := NewStore(filepath.Join(t.TempDir(), "hunter.json"))
	f, err := st.UpsertFinding(Finding{URL: "https://example.com", Product: "openai-compatible", Status: FindingConfirmed})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.CreateDraft(Draft{FindingID: f.ID, ChannelID: "smtp-1", To: "security@example.com", Subject: "Security notice", Body: "Redacted evidence sk-proj-abcdefghijklmnopqrstuvxyz0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != DraftPending {
		t.Fatalf("status=%s", d.Status)
	}
	if strings.Contains(d.Body, "abcdefghijkl") {
		t.Fatalf("draft stored raw secret: %q", d.Body)
	}
	if _, err := st.MarkSent(d.ID); err == nil {
		t.Fatal("unapproved draft must not be sent")
	}
	d, err = st.ApproveDraft(d.ID, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != DraftApproved || d.ApprovedBy != "operator" {
		t.Fatalf("draft=%+v", d)
	}
	d, err = st.BeginSend(d.ID)
	if err != nil || d.Status != DraftSending {
		t.Fatalf("draft=%+v err=%v", d, err)
	}
	d, err = st.MarkSent(d.ID)
	if err != nil || d.Status != DraftSent || d.SentAt == "" {
		t.Fatalf("draft=%+v err=%v", d, err)
	}
}

func TestVersionTwoMigrationPreservesExplicitFalse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hunter.json")
	raw := `{"version":2,"config":{"isolated_network":false,"auto_discover_network":false,"credential_audit_enabled":false,"max_results":50,"rate_per_minute":6}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := NewStore(path).Config(false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsolatedNetwork || cfg.AutoDiscoverNetwork || cfg.CredentialAuditEnabled {
		t.Fatalf("explicit false values were overwritten: %+v", cfg)
	}
}

func TestSnapshotMigratesLegacyMetadataRedaction(t *testing.T) {
	st := NewStore(filepath.Join(t.TempDir(), "hunter.json"))
	secret := "sk-proj-abcdefghijklmnopqrstuvxyz0123456789"
	if err := st.save(Snapshot{Version: 1, Config: DefaultConfig(), Findings: []Finding{{ID: "f1", URL: "https://example.com", Metadata: map[string]string{"server": secret}}}}); err != nil {
		t.Fatal(err)
	}
	snap, err := st.Snapshot(true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snap.Findings[0].Metadata["server"], "abcdefghijkl") {
		t.Fatalf("snapshot leaked metadata: %+v", snap.Findings[0].Metadata)
	}
	raw, err := st.load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw.Findings[0].Metadata["server"], "abcdefghijkl") {
		t.Fatalf("store was not migrated: %+v", raw.Findings[0].Metadata)
	}
}

func TestConcurrentBeginSendHasSingleWinner(t *testing.T) {
	st := NewStore(filepath.Join(t.TempDir(), "hunter.json"))
	f, err := st.UpsertFinding(Finding{URL: "https://example.com", Status: FindingConfirmed})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.CreateDraft(Draft{FindingID: f.ID, ChannelID: "smtp-1", To: "security@example.com", Subject: "notice", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveDraft(d.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.BeginSend(d.ID); err == nil {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("send winners=%d", winners.Load())
	}
}

func TestRediscoveryPreservesHumanStatus(t *testing.T) {
	st := NewStore(filepath.Join(t.TempDir(), "hunter.json"))
	f, err := st.UpsertFinding(Finding{URL: "https://example.com", Status: FindingNew})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetFindingStatus(f.ID, FindingDismissed); err != nil {
		t.Fatal(err)
	}
	f, err = st.UpsertFinding(Finding{URL: "https://example.com", Status: FindingNew})
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != FindingDismissed {
		t.Fatalf("status reset to %s", f.Status)
	}
}

func TestProviderSecretsAreMasked(t *testing.T) {
	st := NewStore(filepath.Join(t.TempDir(), "hunter.json"))
	err := st.SaveConfig(Config{FOFAEmail: "a@example.com", FOFAKey: "fofa-secret", ShodanKey: "shodan-secret"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := st.Config(true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FOFAKey != MaskedSecret || cfg.ShodanKey != MaskedSecret {
		t.Fatalf("not masked: %+v", cfg)
	}
	if err := st.SaveConfig(Config{FOFAEmail: "a@example.com", FOFAKey: "", ShodanKey: "", MaxResults: 50, RatePerMinute: 6}); err != nil {
		t.Fatal(err)
	}
	cfg, err = st.Config(false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FOFAKey != "" || cfg.ShodanKey != "" {
		t.Fatalf("keys were not cleared: %+v", cfg)
	}
}
