package localpool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteCredentialAndRejectTraversal(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "run-1")
	cpaDir := filepath.Join(runDir, "CPA")
	if err := os.MkdirAll(cpaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cpaDir, "account.json"), []byte(`{"email":"user@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	service := New(filepath.Join(root, "local-pool"))
	added, entries, err := service.ImportRun(runDir)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || len(entries) != 1 {
		t.Fatalf("unexpected import: added=%d entries=%v", added, entries)
	}
	if _, err := service.PathFor("../account.json"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	path, err := service.PathFor("account.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Delete("account.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("credential file still exists: %v", err)
	}
	if got := service.List(); len(got) != 0 {
		t.Fatalf("index entry still exists: %+v", got)
	}
	if _, _, err := service.ImportRun(runDir); err != nil {
		t.Fatal(err)
	}
	missingPath, err := service.PathFor("account.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(missingPath); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete("account.json"); err != nil {
		t.Fatalf("stale index cleanup failed: %v", err)
	}
	if got := service.List(); len(got) != 0 {
		t.Fatalf("stale index entry still exists: %+v", got)
	}
}
