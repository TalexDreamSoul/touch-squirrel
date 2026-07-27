package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndListInTree(t *testing.T) {
	root := t.TempDir()
	plug := filepath.Join(root, "demo")
	if err := os.MkdirAll(plug, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{
  "id": "demo",
  "name": "Demo",
  "version": "0.1.0",
  "kind": ["registrar"],
  "runtime": "go",
  "entry": {"go": "bin/demo"},
  "hostApi": "0.1"
}`
	if err := os.WriteFile(filepath.Join(plug, "plugin.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	m := NewManager(filepath.Join(home, "plugins"), filepath.Join(home, "enabled.json"), root)
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Manifest.ID != "demo" {
		t.Fatalf("list=%+v", list)
	}
	if !list[0].Enabled {
		t.Fatal("in-tree should default enabled")
	}
	if err := m.Disable("demo"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestInstallPath(t *testing.T) {
	srcRoot := t.TempDir()
	src := filepath.Join(srcRoot, "pack")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{
  "id": "local-pack",
  "name": "Local",
  "version": "1.2.3",
  "kind": ["pool-proxy"],
  "runtime": "js",
  "entry": {"js": "dist/index.js"},
  "hostApi": "0.1"
}`
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "dist-index-placeholder"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	m := NewManager(filepath.Join(home, "plugins"), filepath.Join(home, "enabled.json"), "")
	it, err := m.InstallPath(src)
	if err != nil {
		t.Fatal(err)
	}
	if it.Manifest.ID != "local-pack" || !it.Enabled {
		t.Fatalf("%+v", it)
	}
	if _, err := os.Stat(filepath.Join(home, "plugins", "local-pack", "1.2.3", "plugin.json")); err != nil {
		t.Fatal(err)
	}
}
