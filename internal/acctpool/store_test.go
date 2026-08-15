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

func TestStoreTimeFilterAndBatchPrimitives(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	old, err := st.Upsert(Account{
		Type: TypeXAI, Plugin: "xai-accounts", Label: "old", ExternalID: "old.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	recent, err := st.Upsert(Account{
		Type: TypeTavily, Plugin: "tavily-pool", Label: "recent", ExternalID: "key-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	neverUsed, err := st.Upsert(Account{
		Type: TypeTavily, Plugin: "tavily-pool", Label: "never", ExternalID: "key-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(
		`UPDATE accounts SET created_at=?, updated_at=?, last_used_at=? WHERE id=?`,
		"2025-01-01T00:00:00Z", "2025-02-01T00:00:00Z", "2025-03-01T00:00:00Z", old.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(
		`UPDATE accounts SET created_at=?, updated_at=?, last_used_at=? WHERE id=?`,
		"2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z", "2026-03-01T00:00:00Z", recent.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(
		`UPDATE accounts SET created_at=?, updated_at=?, last_used_at=? WHERE id=?`,
		"2024-01-01T00:00:00Z", "2024-02-01T00:00:00Z", "", neverUsed.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Upsert(Account{
		Type: TypeTavily, Plugin: "tavily-pool", Label: "recent", ExternalID: "key-1",
	}); err != nil {
		t.Fatal(err)
	}
	stable, err := st.GetMany([]string{recent.ID})
	if err != nil || stable[recent.ID].UpdatedAt != "2026-02-01T00:00:00Z" {
		t.Fatalf("unchanged upsert moved updated_at: account=%+v err=%v", stable[recent.ID], err)
	}
	if _, err := st.Upsert(Account{
		Type: TypeTavily, Plugin: "tavily-pool", Label: "recent", ExternalID: "key-1",
		CreatedAt: "2023-04-05T06:07:08+08:00",
	}); err != nil {
		t.Fatal(err)
	}
	corrected, err := st.GetMany([]string{recent.ID})
	if err != nil || corrected[recent.ID].CreatedAt != "2023-04-04T22:07:08Z" {
		t.Fatalf("source created_at was not backfilled: account=%+v err=%v", corrected[recent.ID], err)
	}
	if corrected[recent.ID].UpdatedAt != "2026-02-01T00:00:00Z" {
		t.Fatalf("created_at correction moved updated_at: %+v", corrected[recent.ID])
	}

	filtered, err := st.List(ListFilter{
		TimeField: "updated_at", From: "2026-01-01T00:00:00Z", To: "2027-01-01T00:00:00Z", Page: 1, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || filtered.Items[0].ID != recent.ID {
		t.Fatalf("unexpected filtered result: %+v", filtered)
	}
	lastUsed, err := st.List(ListFilter{
		TimeField: "last_used_at", To: "2027-01-01T00:00:00Z", Page: 1, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastUsed.Total != 2 {
		t.Fatalf("never-used account leaked into last-used range: %+v", lastUsed)
	}
	if err := st.SetStatus(recent.ID, StatusDisabled); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Upsert(Account{
		Type: TypeTavily, Plugin: "tavily-pool", Label: "recent", ExternalID: "key-1", Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	tavilyState, err := st.GetMany([]string{recent.ID})
	if err != nil || tavilyState[recent.ID].Status != StatusActive {
		t.Fatalf("tavily source status did not reconcile: account=%+v err=%v", tavilyState[recent.ID], err)
	}
	if _, err := st.List(ListFilter{TimeField: "DROP TABLE accounts", Page: 1, Limit: 10}); err == nil {
		t.Fatal("expected invalid time field error")
	}

	if err := st.SetStatus(old.ID, StatusDisabled); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Upsert(Account{
		Type: TypeXAI, Plugin: "xai-accounts", Label: "old", ExternalID: "old.json", Status: StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	accounts, err := st.GetMany([]string{old.ID, recent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if accounts[old.ID].Status != StatusDisabled {
		t.Fatalf("manual disable was overwritten: %+v", accounts[old.ID])
	}
	if err := st.Delete(recent.ID); err != nil {
		t.Fatal(err)
	}
	accounts, err = st.GetMany([]string{recent.ID})
	if err != nil || len(accounts) != 0 {
		t.Fatalf("delete failed: accounts=%v err=%v", accounts, err)
	}
}

func TestAutoMigrateLocalAndTavily(t *testing.T) {
	dir := t.TempDir()
	// fake local-pool index
	lp := filepath.Join(dir, "local-pool")
	_ = os.MkdirAll(lp, 0o700)
	localCreated := time.Date(2024, time.June, 7, 8, 9, 10, 0, time.UTC)
	idx := map[string]any{
		"version": 1,
		"items": map[string]any{
			"acc.json": map[string]any{
				"name":       "acc.json",
				"email":      "u@x.ai",
				"source_run": "20260101-000000",
				"hash":       "abc",
				"size":       12,
				"added_at":   localCreated.Format(time.RFC3339),
			},
		},
	}
	raw, _ := json.Marshal(idx)
	if err := os.WriteFile(filepath.Join(lp, "index.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// fake tavily keys
	tvPath := filepath.Join(dir, "keys.json")
	tavilyCreated := "2023-05-06T07:08:09Z"
	tv := map[string]any{
		"keys": []map[string]any{
			{
				"id": "deadbe", "api_key": "tvly-demo-key-aaaaaaaa", "status": "active",
				"created_at": tavilyCreated, "note": "n1",
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
	localAccount, err := st.GetByExternal(TypeXAI, "acc.json")
	if err != nil || localAccount.CreatedAt != localCreated.Format(time.RFC3339) {
		t.Fatalf("local created_at not preserved: account=%+v err=%v", localAccount, err)
	}
	tavilyAccount, err := st.GetByExternal(TypeTavily, "deadbe")
	if err != nil || tavilyAccount.CreatedAt != tavilyCreated {
		t.Fatalf("tavily created_at not preserved: account=%+v err=%v", tavilyAccount, err)
	}
}
