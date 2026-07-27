package acctpool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertListFilter(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.Upsert(Account{
		Type: TypeXAI, Plugin: "xai-accounts", Label: "a@x.ai", Email: "a@x.ai",
		ExternalID: "cpa-1.json", Status: StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" {
		t.Fatal("expected id")
	}
	// dedupe by external_id
	b, err := st.Upsert(Account{
		Type: TypeXAI, Plugin: "xai-accounts", Label: "a@x.ai", Email: "a@x.ai",
		ExternalID: "cpa-1.json", Status: StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID {
		t.Fatalf("dedupe failed %s != %s", b.ID, a.ID)
	}

	_, err = st.Upsert(Account{
		Type: TypeTavily, Plugin: "tavily-pool", Label: "tvly…aaaa",
		ExternalID: "k1", Status: StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := st.List(ListFilter{Page: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 2 {
		t.Fatalf("total=%d", all.Total)
	}
	if all.ByType[TypeXAI] != 1 || all.ByType[TypeTavily] != 1 {
		t.Fatalf("by_type=%v", all.ByType)
	}
	only, err := st.List(ListFilter{Type: TypeTavily, Page: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if only.Total != 1 || only.Items[0].Type != TypeTavily {
		t.Fatalf("filter: %+v", only)
	}
}

func TestAutoMigrateLocalAndTavily(t *testing.T) {
	dir := t.TempDir()
	// fake local-pool index
	lp := filepath.Join(dir, "local-pool")
	_ = os.MkdirAll(lp, 0o700)
	idx := map[string]any{
		"version": 1,
		"items": map[string]any{
			"acc.json": map[string]any{
				"name":       "acc.json",
				"email":      "u@x.ai",
				"source_run": "20260101-000000",
				"hash":       "abc",
				"size":       12,
				"added_at":   time.Now().UTC().Format(time.RFC3339),
			},
		},
	}
	raw, _ := json.Marshal(idx)
	if err := os.WriteFile(filepath.Join(lp, "index.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// fake tavily keys
	tvPath := filepath.Join(dir, "keys.json")
	tv := map[string]any{
		"keys": []map[string]any{
			{
				"id": "deadbe", "api_key": "tvly-demo-key-aaaaaaaa", "status": "active",
				"created_at": time.Now().UTC().Format(time.RFC3339), "note": "n1",
			},
		},
	}
	traw, _ := json.Marshal(tv)
	if err := os.WriteFile(tvPath, traw, 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rep, err := st.AutoMigrate(MigrateOptions{LocalPoolDir: lp, TavilyKeysPath: tvPath})
	if err != nil {
		t.Fatal(err)
	}
	if rep.LocalPoolImported < 1 || rep.TavilyImported < 1 {
		t.Fatalf("report=%+v", rep)
	}
	// second run skipped
	rep2, err := st.AutoMigrate(MigrateOptions{LocalPoolDir: lp, TavilyKeysPath: tvPath})
	if err != nil {
		t.Fatal(err)
	}
	if !rep2.Skipped {
		t.Fatalf("expected skip, got %+v", rep2)
	}
	n, _ := st.Count()
	if n != 2 {
		t.Fatalf("count=%d", n)
	}
}
