package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRollsBackWhenProvenanceSaveFails(t *testing.T) {
	t.Setenv(officialRepositoryEnv, DefaultOfficialRepositoryURL)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	manager := NewManager(filepath.Join(home, "plugins"), filepath.Join(home, "enabled.json"), "")
	officialRoot := filepath.Join(root, "official")
	if err := os.MkdirAll(officialRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "id": "transaction-demo",
  "name": "Transaction Demo",
  "version": "1.0.0",
  "kind": ["registrar"],
  "runtime": "bridge",
  "entry": {"bridge": "runner.py"},
  "hostApi": "0.1"
}`
	if err := os.WriteFile(filepath.Join(officialRoot, "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(officialRoot, "runner.py"), []byte("official\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallRepositoryPath(officialRoot, InstallSource{
		RepositoryID:   OfficialRepositoryID,
		RepositoryName: "Official",
		RepositoryURL:  DefaultOfficialRepositoryURL,
	}); err != nil {
		t.Fatal(err)
	}

	localRoot := filepath.Join(root, "local")
	if err := copyDir(officialRoot, localRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "runner.py"), []byte("local replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.writeJSON = func(path string, value any, mode os.FileMode) error {
		if strings.HasSuffix(path, "plugin-sources.json") {
			return errors.New("injected provenance write failure")
		}
		return writeJSONAtomic(path, value, mode)
	}
	if _, err := manager.InstallPath(localRoot); err == nil {
		t.Fatal("expected install to fail")
	}
	manager.writeJSON = writeJSONAtomic

	installedRunner := filepath.Join(home, "plugins", "transaction-demo", "1.0.0", "runner.py")
	data, err := os.ReadFile(installedRunner)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "official\n" {
		t.Fatalf("plugin directory was not rolled back: %q", data)
	}
	listed, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].Official {
		t.Fatalf("official provenance was not preserved: %+v", listed)
	}
}

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
  "version": "1.2.3+build.7",
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
	forgedSource := `{"repository_id":"official","repository_url":"https://github.com/example/fake","official":true}`
	if err := os.WriteFile(filepath.Join(src, installSourceFile), []byte(forgedSource), 0o600); err != nil {
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
	if it.Official || it.RepositoryID != "" {
		t.Fatalf("local install trusted forged provenance: %+v", it)
	}
	if err := os.WriteFile(filepath.Join(home, "plugins", "local-pack", "1.2.3+build.7", installSourceFile), []byte(forgedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Official || listed[0].RepositoryID != "" {
		t.Fatalf("scanned plugin trusted in-directory provenance: %+v", listed)
	}
	if _, err := os.Stat(filepath.Join(home, "plugins", "local-pack", "1.2.3+build.7", "plugin.json")); err != nil {
		t.Fatal(err)
	}
}
