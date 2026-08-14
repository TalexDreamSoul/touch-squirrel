package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInTreeRootCanBeDisabled(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	t.Setenv(disableInTreePluginsEnv, "1")
	if got := ResolveInTreeRoot(); got != "" {
		t.Fatalf("ResolveInTreeRoot()=%q, want empty", got)
	}
}
