package plugin

import (
	"path/filepath"
	"testing"
)

func TestBridgePluginsLoad(t *testing.T) {
	root, _ := filepath.Abs("../..")
	mgr := NewManager(filepath.Join(root, "plugins"), filepath.Join(root, ".grok/plugins-enabled.json"), ResolveInTreeRoot())
	for _, id := range []string{"github-registrar", "grok-panel-registrar", "grok-http-registrar", "chatgpt-registrar", "claude-registrar", "outlook-registrar"} {
		it, err := mgr.Get(id)
		if err != nil {
			t.Fatalf("%s 加载失败: %v", id, err)
		}
		if it.Manifest.Runtime != RuntimeBridge || it.Manifest.Entry.Bridge == "" {
			t.Fatalf("%s 非 bridge 插件: runtime=%s bridge=%q", id, it.Manifest.Runtime, it.Manifest.Entry.Bridge)
		}
	}
}
