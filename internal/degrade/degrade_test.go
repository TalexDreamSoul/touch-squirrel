package degrade

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
)

type fakeClient struct {
	list []cpa.AuthMeta
	docs map[string]cpa.Document
}

func (f *fakeClient) List() ([]cpa.AuthMeta, error) { return f.list, nil }

func (f *fakeClient) Download(name string) ([]byte, error) {
	d, ok := f.docs[name]
	if !ok {
		return nil, fmt.Errorf("not found: %s", name)
	}
	return json.Marshal(d)
}

func newTestService(t *testing.T, client ManagementAPI, probe ProbeFunc) *Service {
	t.Helper()
	cfg := config.Defaults()
	cfg.CPAManagementKey = "k"
	cfg.DegradeSample = 10
	cfg.DegradeRecheckMin = 0
	cfg.DegradeExitWindowMin = 10
	cfg.DegradeExitAccountCap = 5
	s := New(filepath.Join(t.TempDir(), "degrade.json"),
		func() config.Config { return cfg },
		func(config.Config) ManagementAPI { return client })
	s.probeFn = probe
	return s
}

func threeAccounts() *fakeClient {
	c := &fakeClient{docs: map[string]cpa.Document{}}
	for _, n := range []string{"a", "b", "c"} {
		c.list = append(c.list, cpa.AuthMeta{Name: n, Email: n + "@x"})
		c.docs[n] = cpa.Document{AccessToken: "t-" + n, Email: n + "@x"}
	}
	return c
}

// A scan must separate the three outcomes that matter operationally: a
// thinking account, a silently downgraded one, and one whose quota ran out
// (which proves nothing either way and must not be called degraded).
func TestScanClassifiesReasoningQuotaAndDowngrade(t *testing.T) {
	client := threeAccounts()
	s := newTestService(t, client, func(doc cpa.Document, proxy string, opt cpa.ProbeOptions) (cpa.ProbeResult, error) {
		switch doc.AccessToken {
		case "t-a":
			return cpa.ProbeResult{Status: 200, ReasoningTokens: 42, HasReasoning: true, OutputText: "1016"}, nil
		case "t-b":
			return cpa.ProbeResult{Status: 200, OutputText: "1016"}, nil
		default:
			return cpa.ProbeResult{Status: 429}, fmt.Errorf("probe http=429 rate/exhausted body=free-usage-exhausted")
		}
	})

	rec, err := s.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if rec.Checked != 3 || rec.Normal != 1 || rec.Degraded != 1 || rec.Exhausted != 1 {
		t.Fatalf("unexpected tally: %+v", rec)
	}

	byName := map[string]Record{}
	for _, r := range s.Records("", "") {
		byName[r.Name] = r
	}
	if byName["b"].Verdict != VerdictDegraded || byName["b"].FirstDegradedAt == nil {
		t.Fatalf("b should be degraded with a first-seen timestamp: %+v", byName["b"])
	}
	if byName["c"].Verdict != VerdictExhausted {
		t.Fatalf("quota-exhausted account must not be called degraded: %+v", byName["c"])
	}
	if got := s.Overview(); got.Degraded != 1 || got.Normal != 1 || got.PoolTotal != 3 {
		t.Fatalf("overview: %+v", got)
	}

	exits := s.Exits()
	if len(exits) != 1 || exits[0].Accounts != 3 {
		t.Fatalf("exit window should hold all three probes: %+v", exits)
	}
}

// FirstDegradedAt records when an account went bad, so it must survive later
// scans instead of being overwritten on every re-check.
func TestFirstDegradedAtIsStable(t *testing.T) {
	client := threeAccounts()
	s := newTestService(t, client, func(cpa.Document, string, cpa.ProbeOptions) (cpa.ProbeResult, error) {
		return cpa.ProbeResult{Status: 200}, nil
	})
	if _, err := s.Scan(context.Background(), ScanOptions{}); err != nil {
		t.Fatalf("scan: %v", err)
	}
	first := map[string]time.Time{}
	for _, r := range s.Records("", "") {
		if r.FirstDegradedAt == nil {
			t.Fatalf("%s: expected a first-degraded timestamp", r.Name)
		}
		first[r.Name] = *r.FirstDegradedAt
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := s.Scan(context.Background(), ScanOptions{Recheck: true}); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	for _, r := range s.Records("", "") {
		if !r.FirstDegradedAt.Equal(first[r.Name]) {
			t.Fatalf("%s: first-degraded timestamp moved: %v -> %v", r.Name, first[r.Name], r.FirstDegradedAt)
		}
		if !r.CheckedAt.After(first[r.Name]) {
			t.Fatalf("%s: re-check should advance checked_at", r.Name)
		}
	}
}

// The exit guard is what stops a scan from manufacturing the very flag it is
// looking for, so the boundary has to be exact: at cap, no slot.
func TestExitSlotBlocksAtAccountCap(t *testing.T) {
	s := newTestService(t, threeAccounts(), nil)
	now := time.Now()
	s.events = []exitEvent{
		{Exit: "direct", Account: "a", At: now.Add(-time.Minute)},
		{Exit: "direct", Account: "a", At: now.Add(-30 * time.Second)}, // same account, not a new slot
		{Exit: "direct", Account: "b", At: now.Add(-20 * time.Second)},
		{Exit: "other", Account: "z", At: now.Add(-10 * time.Second)},
	}
	if free, _ := s.exitSlot("direct", 10*time.Minute, 3); !free {
		t.Fatal("two distinct accounts under a cap of 3 should leave a slot")
	}
	free, next := s.exitSlot("direct", 10*time.Minute, 2)
	if free {
		t.Fatal("at cap the exit must block")
	}
	if want := now.Add(-time.Minute).Add(10 * time.Minute); next.Sub(want).Abs() > time.Second {
		t.Fatalf("should wait for the oldest in-window account to age out, got %v want %v", next, want)
	}
	if free, _ := s.exitSlot("direct", 15*time.Second, 2); !free {
		t.Fatal("a shorter window should drop the older accounts and free the exit")
	}
}

// Isolated accounts are the ones already known bad; re-probing them burns
// quota for nothing and puts another account on the exit counter.
func TestPickTargetsSkipsIsolatedAndRecentlyChecked(t *testing.T) {
	client := threeAccounts()
	s := newTestService(t, client, nil)
	now := time.Now()
	s.records = map[string]Record{
		"a": {Name: "a", Verdict: VerdictDegraded, Isolated: true, CheckedAt: now.Add(-time.Hour)},
		"b": {Name: "b", Verdict: VerdictNormal, CheckedAt: now.Add(-time.Minute)},
	}
	cfg := config.Defaults()
	cfg.DegradeRecheckMin = 120

	got := s.pickTargets(client.list, 10, false, cfg)
	if len(got) != 1 || got[0].Name != "c" {
		t.Fatalf("only the never-checked account should be picked, got %+v", got)
	}

	got = s.pickTargets(client.list, 10, true, cfg)
	if len(got) != 2 {
		t.Fatalf("recheck should re-include b but never the isolated a, got %+v", got)
	}
	if got[0].Name != "c" {
		t.Fatalf("never-checked accounts must come first, got %+v", got)
	}
	for _, m := range got {
		if m.Name == "a" {
			t.Fatal("isolated account must stay out of the sample")
		}
	}
}
