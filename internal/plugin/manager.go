package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EnabledFile is the on-disk enable map under the squirrel home.
type EnabledFile struct {
	Plugins map[string]EnabledEntry `json:"plugins"`
}

// EnabledEntry records which installed version is active.
type EnabledEntry struct {
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

// Installed is a discovered plugin instance on disk.
type Installed struct {
	Manifest Manifest
	Root     string
	Enabled  bool
	InTree   bool
}

// Manager discovers in-tree + installed plugins and tracks enable state.
type Manager struct {
	// HomePlugins is ~/.touch-squirrel/plugins
	HomePlugins string
	// EnabledPath is ~/.touch-squirrel/enabled.json
	EnabledPath string
	// InTreeRoot is optional repo plugins/ directory for first-party packs.
	InTreeRoot string
}

// NewManager builds a manager for the given home paths and optional in-tree root.
func NewManager(homePlugins, enabledPath, inTreeRoot string) *Manager {
	return &Manager{
		HomePlugins: homePlugins,
		EnabledPath: enabledPath,
		InTreeRoot:  inTreeRoot,
	}
}

// List returns installed + in-tree plugins (one row per id, preferred source wins).
func (m *Manager) List() ([]Installed, error) {
	byID := map[string]Installed{}

	// in-tree first (lower priority if home install exists)
	if m.InTreeRoot != "" {
		items, err := scanPluginTree(m.InTreeRoot, true)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, it := range items {
			byID[it.Manifest.ID] = it
		}
	}

	// home installs override in-tree for same id
	if m.HomePlugins != "" {
		items, err := scanVersionedPlugins(m.HomePlugins)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, it := range items {
			byID[it.Manifest.ID] = it
		}
	}

	en, err := m.loadEnabled()
	if err != nil {
		return nil, err
	}
	out := make([]Installed, 0, len(byID))
	for id, it := range byID {
		if e, ok := en.Plugins[id]; ok {
			it.Enabled = e.Enabled
			// if enabled pins a version and home has it, prefer that path
			if e.Version != "" && m.HomePlugins != "" {
				pinned := filepath.Join(m.HomePlugins, id, e.Version)
				if mf := filepath.Join(pinned, "plugin.json"); fileExists(mf) {
					if man, err := LoadManifest(mf); err == nil {
						man.Path = pinned
						man.Source = "installed"
						it.Manifest = man
						it.Root = pinned
						it.InTree = false
					}
				}
			}
		} else if it.InTree {
			// first-party in-tree defaults to enabled for bootstrap UX
			it.Enabled = true
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest.ID < out[j].Manifest.ID
	})
	return out, nil
}

// Get returns one plugin by id.
func (m *Manager) Get(id string) (Installed, error) {
	list, err := m.List()
	if err != nil {
		return Installed{}, err
	}
	for _, it := range list {
		if it.Manifest.ID == id {
			return it, nil
		}
	}
	return Installed{}, fmt.Errorf("plugin not found: %s", id)
}

// Enable marks a plugin enabled (and records its current version).
func (m *Manager) Enable(id string) error {
	it, err := m.Get(id)
	if err != nil {
		return err
	}
	en, err := m.loadEnabled()
	if err != nil {
		return err
	}
	if en.Plugins == nil {
		en.Plugins = map[string]EnabledEntry{}
	}
	en.Plugins[id] = EnabledEntry{Version: it.Manifest.Version, Enabled: true}
	return m.saveEnabled(en)
}

// Disable marks a plugin disabled.
func (m *Manager) Disable(id string) error {
	it, err := m.Get(id)
	if err != nil {
		return err
	}
	en, err := m.loadEnabled()
	if err != nil {
		return err
	}
	if en.Plugins == nil {
		en.Plugins = map[string]EnabledEntry{}
	}
	en.Plugins[id] = EnabledEntry{Version: it.Manifest.Version, Enabled: false}
	return m.saveEnabled(en)
}

// InstallPath copies a local plugin directory into home plugins store.
// Trust model v0: any local path is allowed.
func (m *Manager) InstallPath(src string) (Installed, error) {
	src, err := filepath.Abs(src)
	if err != nil {
		return Installed{}, err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return Installed{}, err
	}
	if !fi.IsDir() {
		return Installed{}, fmt.Errorf("install source must be a directory (tgz support later): %s", src)
	}
	mfPath := filepath.Join(src, "plugin.json")
	man, err := LoadManifest(mfPath)
	if err != nil {
		return Installed{}, err
	}
	if m.HomePlugins == "" {
		return Installed{}, fmt.Errorf("home plugins dir not configured")
	}
	dst := filepath.Join(m.HomePlugins, man.ID, man.Version)
	if err := os.RemoveAll(dst); err != nil {
		return Installed{}, err
	}
	if err := copyDir(src, dst); err != nil {
		return Installed{}, err
	}
	man.Path = dst
	man.Source = "installed"
	en, err := m.loadEnabled()
	if err != nil {
		return Installed{}, err
	}
	if en.Plugins == nil {
		en.Plugins = map[string]EnabledEntry{}
	}
	en.Plugins[man.ID] = EnabledEntry{Version: man.Version, Enabled: true}
	if err := m.saveEnabled(en); err != nil {
		return Installed{}, err
	}
	return Installed{Manifest: man, Root: dst, Enabled: true, InTree: false}, nil
}

func (m *Manager) loadEnabled() (EnabledFile, error) {
	var en EnabledFile
	en.Plugins = map[string]EnabledEntry{}
	if m.EnabledPath == "" {
		return en, nil
	}
	b, err := os.ReadFile(m.EnabledPath)
	if err != nil {
		if os.IsNotExist(err) {
			return en, nil
		}
		return en, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return en, nil
	}
	if err := json.Unmarshal(b, &en); err != nil {
		return en, fmt.Errorf("enabled.json: %w", err)
	}
	if en.Plugins == nil {
		en.Plugins = map[string]EnabledEntry{}
	}
	return en, nil
}

func (m *Manager) saveEnabled(en EnabledFile) error {
	if m.EnabledPath == "" {
		return fmt.Errorf("enabled path not configured")
	}
	if err := os.MkdirAll(filepath.Dir(m.EnabledPath), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(en, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(m.EnabledPath, b, 0o600)
}

// scanPluginTree reads plugins/<id>/plugin.json (in-tree layout).
func scanPluginTree(root string, inTree bool) ([]Installed, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		mf := filepath.Join(dir, "plugin.json")
		if !fileExists(mf) {
			continue
		}
		man, err := LoadManifest(mf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", mf, err)
		}
		man.Path = dir
		if man.Source == "" && inTree {
			man.Source = "in-tree"
		}
		out = append(out, Installed{Manifest: man, Root: dir, InTree: inTree})
	}
	return out, nil
}

// scanVersionedPlugins reads plugins/<id>/<version>/plugin.json.
func scanVersionedPlugins(root string) ([]Installed, error) {
	idEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []Installed
	for _, idEnt := range idEntries {
		if !idEnt.IsDir() {
			continue
		}
		idDir := filepath.Join(root, idEnt.Name())
		// allow flat plugin.json under id dir too
		if fileExists(filepath.Join(idDir, "plugin.json")) {
			man, err := LoadManifest(filepath.Join(idDir, "plugin.json"))
			if err != nil {
				return nil, err
			}
			man.Path = idDir
			if man.Source == "" {
				man.Source = "installed"
			}
			out = append(out, Installed{Manifest: man, Root: idDir, InTree: false})
			continue
		}
		verEntries, err := os.ReadDir(idDir)
		if err != nil {
			return nil, err
		}
		// pick highest semver-ish by string sort as v0 heuristic; pin via enabled.json
		var best Installed
		for _, vEnt := range verEntries {
			if !vEnt.IsDir() {
				continue
			}
			vDir := filepath.Join(idDir, vEnt.Name())
			mf := filepath.Join(vDir, "plugin.json")
			if !fileExists(mf) {
				continue
			}
			man, err := LoadManifest(mf)
			if err != nil {
				return nil, err
			}
			man.Path = vDir
			if man.Source == "" {
				man.Source = "installed"
			}
			cand := Installed{Manifest: man, Root: vDir, InTree: false}
			if best.Manifest.ID == "" || man.Version >= best.Manifest.Version {
				best = cand
			}
		}
		if best.Manifest.ID != "" {
			out = append(out, best)
		}
	}
	return out, nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
