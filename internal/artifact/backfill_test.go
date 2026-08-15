package artifact

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grok-free-register/grok-reg/internal/localpool"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
)

func TestBackfillRejectsOversizedCredential(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, "local-pool")
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "oversized.json"
	path := filepath.Join(localDir, name)
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxBackfillPayloadBytes+1); err != nil {
		t.Fatal(err)
	}
	index := localpool.Index{Version: 1, Items: map[string]*localpool.Entry{
		name: {Name: name},
	}}
	raw, _ := json.Marshal(index)
	if err := os.WriteFile(filepath.Join(localDir, "index.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := BackfillLegacyCredentials(NewStore(filepath.Join(root, "artifacts")), localDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.Imported != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestBackfillLegacyCredentialsIsIdempotent(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, "local-pool")
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		t.Fatal(err)
	}
	credential := []byte(`{"email":"legacy@example.com","token":"xai-secret"}`)
	if err := os.WriteFile(filepath.Join(localDir, "xai-legacy.json"), credential, 0o600); err != nil {
		t.Fatal(err)
	}
	index := localpool.Index{
		Version: 1,
		Items: map[string]*localpool.Entry{
			"xai-legacy.json": {
				Name: "xai-legacy.json", Email: "legacy@example.com", SourceRun: "run-legacy",
				AddedAt: time.Date(2026, time.August, 14, 1, 2, 3, 0, time.UTC),
			},
		},
	}
	indexRaw, _ := json.Marshal(index)
	if err := os.WriteFile(filepath.Join(localDir, "index.json"), indexRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	tavilyPath := filepath.Join(root, "plugins-data", "tavily-pool", "keys.json")
	if err := os.MkdirAll(filepath.Dir(tavilyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	tavilyRaw, _ := json.Marshal(tavilypool.Snapshot{Keys: []tavilypool.Key{{
		ID: "key-legacy", APIKey: "tvly-secret", Status: tavilypool.StatusActive,
		CreatedAt: "2026-08-14T02:03:04Z", Note: "legacy",
	}}})
	if err := os.WriteFile(tavilyPath, tavilyRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(root, "artifacts"))
	report, err := BackfillLegacyCredentials(store, localDir, tavilyPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Imported != 2 || report.Failed != 0 {
		t.Fatalf("report=%+v", report)
	}
	items, err := store.List("", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%+v", items)
	}
	var sawXAI, sawTavily bool
	for _, item := range items {
		switch item.Plugin {
		case "xai-accounts":
			sawXAI = item.Labels["email"] == "legacy@example.com" && strings.Contains(string(item.Payload), "xai-secret")
		case "tavily-pool":
			sawTavily = item.Labels["key_id"] == "key-legacy" && strings.Contains(string(item.Payload), "tvly-secret")
		}
	}
	if !sawXAI || !sawTavily {
		t.Fatalf("missing payloads: xai=%v tavily=%v", sawXAI, sawTavily)
	}

	report, err = BackfillLegacyCredentials(store, localDir, tavilyPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Imported != 0 || report.Skipped != 2 || report.Failed != 0 {
		t.Fatalf("second report=%+v", report)
	}
	items, err = store.List("", "", 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("second items=%d err=%v", len(items), err)
	}

	updatedCredential := []byte(`{"email":"legacy@example.com","token":"xai-secret-updated"}`)
	if err := os.WriteFile(filepath.Join(localDir, "xai-legacy.json"), updatedCredential, 0o600); err != nil {
		t.Fatal(err)
	}
	report, err = BackfillLegacyCredentials(store, localDir, tavilyPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || report.Imported != 0 || report.Skipped != 1 || report.Failed != 0 {
		t.Fatalf("update report=%+v", report)
	}
	items, err = store.List("", "", 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("updated items=%d err=%v", len(items), err)
	}
	for _, item := range items {
		if item.Plugin == "xai-accounts" && !strings.Contains(string(item.Payload), "xai-secret-updated") {
			t.Fatalf("stale xai payload=%s", item.Payload)
		}
	}
}
