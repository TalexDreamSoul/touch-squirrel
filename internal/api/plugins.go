package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/grok-free-register/grok-reg/internal/acctpool"
	"github.com/grok-free-register/grok-reg/internal/artifact"
	"github.com/grok-free-register/grok-reg/internal/plugin"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
)

func (s *Server) pluginManager() *plugin.Manager {
	return plugin.NewManager(s.opt.Paths.PluginsDir, s.opt.Paths.EnabledFile, plugin.ResolveInTreeRoot())
}

func (s *Server) handlePluginsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.pluginManager().List()
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		kinds := make([]string, 0, len(it.Manifest.Kind))
		for _, k := range it.Manifest.Kind {
			kinds = append(kinds, string(k))
		}
		src := "installed"
		if it.InTree {
			src = "in-tree"
		}
		out = append(out, map[string]any{
			"id":             it.Manifest.ID,
			"name":           it.Manifest.Name,
			"version":        it.Manifest.Version,
			"description":    it.Manifest.Description,
			"runtime":        it.Manifest.Runtime,
			"kind":           kinds,
			"enabled":        it.Enabled,
			"source":         src,
			"path":           it.Root,
			"capabilities":   it.Manifest.Capabilities,
			"artifact_kinds": it.Manifest.ArtifactKinds,
			"status":         it.Manifest.Status,
		})
	}
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"plugins": out,
		"home":    s.opt.Paths.PluginsDir,
		"in_tree": plugin.ResolveInTreeRoot(),
	})
}

func (s *Server) handlePluginEnable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.pluginManager().Enable(id); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "enabled": true})
}

func (s *Server) handlePluginDisable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.pluginManager().Disable(id); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "enabled": false})
}

func (s *Server) handleArtifactsList(w http.ResponseWriter, r *http.Request) {
	pluginID := r.URL.Query().Get("plugin")
	kind := r.URL.Query().Get("kind")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	st := artifact.NewStore(s.opt.Paths.ArtifactsDir)
	list, err := st.List(pluginID, kind, limit)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// strip heavy/secret payload by default; include labels + meta only
	rows := make([]map[string]any, 0, len(list))
	includePayload := r.URL.Query().Get("payload") == "1"
	for _, a := range list {
		row := map[string]any{
			"id":         a.ID,
			"plugin":     a.Plugin,
			"kind":       a.Kind,
			"status":     a.Status,
			"labels":     a.Labels,
			"run_id":     a.RunID,
			"created_at": a.CreatedAt,
			"updated_at": a.UpdatedAt,
		}
		if includePayload {
			row["payload"] = json.RawMessage(a.Payload)
		}
		rows = append(rows, row)
	}
	writeJSON(w, 200, map[string]any{
		"ok":        true,
		"artifacts": rows,
		"store":     s.opt.Paths.ArtifactsDir,
	})
}

func (s *Server) tavilyPool() *tavilypool.Pool {
	return tavilypool.New(tavilypool.DefaultStatePath(s.opt.Paths.Root))
}

func (s *Server) handleTavilyKeysList(w http.ResponseWriter, r *http.Request) {
	keys, err := s.tavilyPool().List(true)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	active := 0
	for _, k := range keys {
		if k.Status == tavilypool.StatusActive {
			active++
		}
	}
	writeJSON(w, 200, map[string]any{
		"ok":          true,
		"keys":        keys,
		"keys_total":  len(keys),
		"keys_active": active,
		"state":       tavilypool.DefaultStatePath(s.opt.Paths.Root),
		"hint":        "CLI: squirrel pool serve --addr 127.0.0.1:8791  →  /mcp + /api/tavily/*",
	})
}

func (s *Server) handleTavilyKeysAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"api_key"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	pool := s.tavilyPool()
	k, err := pool.Add(body.APIKey, body.Note)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if s.accounts != nil {
		_ = s.accounts.UpsertTavilyKey(pool.Path, k)
	}
	arts := artifact.NewStore(s.opt.Paths.ArtifactsDir)
	_, _ = arts.PutJSON("tavily-pool", "key.tavily", artifact.StatusFresh, map[string]string{
		"key_id": k.ID,
	}, map[string]any{
		"id":     k.ID,
		"status": k.Status,
		"note":   k.Note,
		"masked": k.APIKey,
	}, "")
	writeJSON(w, 200, map[string]any{"ok": true, "key": k})
}

func (s *Server) handleTavilyKeyStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	st := tavilypool.KeyStatus(strings.TrimSpace(body.Status))
	switch st {
	case tavilypool.StatusActive, tavilypool.StatusDisabled, tavilypool.StatusExhausted:
	default:
		writeJSON(w, 400, map[string]any{"ok": false, "error": "status must be active|disabled|exhausted"})
		return
	}
	if err := s.tavilyPool().SetStatus(id, st); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if s.accounts != nil {
		_ = s.accounts.SetStatusByExternal(acctpool.TypeTavily, id, string(st))
	}
	writeJSON(w, 200, map[string]any{"ok": true, "id": id, "status": st})
}
