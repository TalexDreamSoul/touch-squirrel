package plugin

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testMarketManifest = `{
  "id": "demo",
  "name": "Demo",
  "version": "1.0.0",
  "kind": ["registrar"],
  "runtime": "bridge",
  "entry": {"bridge": "runner.py"},
  "hostApi": "0.1"
}`

func TestDefaultOfficialRepositoryURL(t *testing.T) {
	const want = "https://github.com/TalexDreamSoul/touch-squirrel"
	if DefaultOfficialRepositoryURL != want {
		t.Fatalf("DefaultOfficialRepositoryURL=%q, want %q", DefaultOfficialRepositoryURL, want)
	}
}

func TestMarketAllowsSlowRepositoryDownloads(t *testing.T) {
	market := NewMarket(t.TempDir(), nil)
	if got, want := market.client.Timeout, 15*time.Minute; got != want {
		t.Fatalf("client timeout=%s, want %s", got, want)
	}
}

func TestMarketRepositoriesKeepOfficialSource(t *testing.T) {
	t.Setenv(officialRepositoryEnv, DefaultOfficialRepositoryURL)
	root := t.TempDir()
	market := NewMarket(root, NewManager(filepath.Join(root, "plugins"), filepath.Join(root, "enabled.json"), ""))

	repository, err := market.AddRepository("Mirror", "https://github.com/example/plugins.git")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := market.AddRepository("Duplicate", "https://github.com/EXAMPLE/PLUGINS"); err == nil {
		t.Fatal("expected case-insensitive duplicate repository to be rejected")
	}
	repositories, err := market.Repositories()
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 2 || !repositories[0].Official || repositories[0].ID != OfficialRepositoryID {
		t.Fatalf("repositories=%+v", repositories)
	}
	if err := market.RemoveRepository(OfficialRepositoryID); err != ErrOfficialRepository {
		t.Fatalf("official remove error=%v", err)
	}
	if err := market.RemoveRepository(repository.ID); err != nil {
		t.Fatal(err)
	}
	repositories, err = market.Repositories()
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || !repositories[0].Official {
		t.Fatalf("repositories after remove=%+v", repositories)
	}
}

func TestMarketInstallTracksOfficialProvenance(t *testing.T) {
	t.Setenv(officialRepositoryEnv, DefaultOfficialRepositoryURL)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	manager := NewManager(filepath.Join(home, "plugins"), filepath.Join(home, "enabled.json"), "")
	market := NewMarket(filepath.Join(home, "market-cache"), manager)
	snapshot := ".snapshot-test"
	pluginRoot := filepath.Join(home, "market-cache", "repositories", OfficialRepositoryID, snapshot, "archive", "plugins", "demo")
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "plugin.json"), []byte(testMarketManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "runner.py"), []byte("print('ok')\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	state := repositoryState{
		RepositoryURL: DefaultOfficialRepositoryURL,
		Snapshot:      snapshot,
		PluginCount:   1,
	}
	if err := writeJSONAtomic(filepath.Join(home, "market-cache", "repositories", OfficialRepositoryID, "state.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}

	plugins, err := market.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || !plugins[0].Official || plugins[0].Installed {
		t.Fatalf("plugins=%+v", plugins)
	}
	installed, err := market.Install(OfficialRepositoryID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !installed.Official || installed.RepositoryID != OfficialRepositoryID || !installed.Enabled {
		t.Fatalf("installed=%+v", installed)
	}

	listed, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].Official || listed[0].RepositoryURL != DefaultOfficialRepositoryURL {
		t.Fatalf("listed=%+v", listed)
	}
}

func TestOfficialCacheIsBoundToRepositoryURL(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "repositories", OfficialRepositoryID, "state.json")
	state := repositoryState{
		RepositoryURL: DefaultOfficialRepositoryURL,
		Snapshot:      ".snapshot-old",
		SyncedAt:      time.Now().UTC(),
		PluginCount:   3,
		LastError:     "old repository failed",
	}
	if err := writeJSONAtomic(statePath, state, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(officialRepositoryEnv, "https://github.com/TalexDreamSoul/new-official-plugins")
	market := NewMarket(root, nil)
	repositories, err := market.Repositories()
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].PluginCount != 0 || repositories[0].SyncedAt != nil || repositories[0].LastError != "" {
		t.Fatalf("stale official cache was reused: %+v", repositories)
	}
	plugins, err := market.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 0 {
		t.Fatalf("stale official plugins were exposed: %+v", plugins)
	}
}

func TestMarketInstalledStateRequiresSameRepository(t *testing.T) {
	t.Setenv(officialRepositoryEnv, DefaultOfficialRepositoryURL)
	root := t.TempDir()
	home := filepath.Join(root, "home")
	manager := NewManager(filepath.Join(home, "plugins"), filepath.Join(home, "enabled.json"), "")
	marketRoot := filepath.Join(home, "market-cache")
	market := NewMarket(marketRoot, manager)
	snapshot := ".snapshot-official"
	cachedRoot := filepath.Join(marketRoot, "repositories", OfficialRepositoryID, snapshot, "archive", "demo")
	if err := os.MkdirAll(cachedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cachedRoot, "plugin.json"), []byte(testMarketManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(marketRoot, "repositories", OfficialRepositoryID, "state.json"), repositoryState{
		RepositoryURL: DefaultOfficialRepositoryURL,
		Snapshot:      snapshot,
		PluginCount:   1,
	}, 0o600); err != nil {
		t.Fatal(err)
	}

	localRoot := filepath.Join(root, "local-demo")
	if err := os.MkdirAll(localRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localRoot, "plugin.json"), []byte(testMarketManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallPath(localRoot); err != nil {
		t.Fatal(err)
	}
	plugins, err := market.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].Installed || plugins[0].InstalledVersion != "" {
		t.Fatalf("local plugin was treated as official installation: %+v", plugins)
	}
}

func TestExtractRepositoryArchiveRejectsPathTraversal(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "bad.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../escape/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	destination := t.TempDir()
	if err := extractRepositoryArchive(archivePath, destination); err == nil {
		t.Fatal("expected unsafe archive to be rejected")
	}
	if _, err := os.Stat(filepath.Join(destination, "..", "escape", "plugin.json")); !os.IsNotExist(err) {
		t.Fatalf("path traversal created a file: %v", err)
	}
}

func TestNormalizeGitHubRepository(t *testing.T) {
	normalized, owner, repo, err := normalizeGitHubRepository("https://github.com/TalexDreamSoul/plugins.git")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "https://github.com/TalexDreamSoul/plugins" || owner != "TalexDreamSoul" || repo != "plugins" {
		t.Fatalf("got %q %q %q", normalized, owner, repo)
	}
	for _, raw := range []string{
		"http://github.com/example/plugins",
		"https://example.com/example/plugins",
		"https://github.com/example/plugins/tree/main",
		"https://github.com/example/plugins?token=secret",
	} {
		if _, _, _, err := normalizeGitHubRepository(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}
