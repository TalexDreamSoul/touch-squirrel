package plugin

import (
	"os"
	"path/filepath"
)

// ResolveInTreeRoot finds the repo plugins/ directory when developing or running
// from a checkout. Empty string means "no in-tree pack".
//
// Order:
//  1. SQUIRREL_PLUGINS env
//  2. ./plugins (cwd)
//  3. walk parents of cwd for plugins/<id>/plugin.json
//  4. relative to executable (bin/../plugins, bin/../../plugins)
func ResolveInTreeRoot() string {
	if v := os.Getenv("SQUIRREL_PLUGINS"); v != "" {
		if st, err := os.Stat(v); err == nil && st.IsDir() {
			return v
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if cand := filepath.Join(cwd, "plugins"); isPluginTree(cand) {
			return cand
		}
		dir := cwd
		for range 8 {
			cand := filepath.Join(dir, "plugins")
			if isPluginTree(cand) {
				return cand
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		for _, rel := range []string{
			"plugins",
			filepath.Join("..", "plugins"),
			filepath.Join("..", "..", "plugins"),
		} {
			cand := filepath.Join(exeDir, rel)
			if abs, err := filepath.Abs(cand); err == nil && isPluginTree(abs) {
				return abs
			}
		}
	}
	return ""
}

func isPluginTree(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if fileExists(filepath.Join(dir, e.Name(), "plugin.json")) {
			return true
		}
	}
	return false
}
