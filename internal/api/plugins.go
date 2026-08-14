package api

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/grok-free-register/grok-reg/internal/acctpool"
	"github.com/grok-free-register/grok-reg/internal/artifact"
	"github.com/grok-free-register/grok-reg/internal/plugin"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
)

func (s *Server) pluginManager() *plugin.Manager {
	if s.plugins == nil {
		s.plugins = plugin.NewManager(s.opt.Paths.PluginsDir, s.opt.Paths.EnabledFile, plugin.ResolveInTreeRoot())
	}
	return s.plugins
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
		} else if it.RepositoryID != "" {
			src = "repository"
		}
		out = append(out, map[string]any{
			"id":              it.Manifest.ID,
			"name":            it.Manifest.Name,
			"version":         it.Manifest.Version,
			"description":     it.Manifest.Description,
			"runtime":         it.Manifest.Runtime,
			"kind":            kinds,
			"enabled":         it.Enabled,
			"source":          src,
			"path":            it.Root,
			"capabilities":    it.Manifest.Capabilities,
			"artifact_kinds":  it.Manifest.ArtifactKinds,
			"status":          it.Manifest.Status,
			"repository_id":   it.RepositoryID,
			"repository_name": it.RepositoryName,
			"repository_url":  it.RepositoryURL,
			"official":        it.Official,
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

func (s *Server) handlePluginRepositoriesList(w http.ResponseWriter, r *http.Request) {
	repositories, err := s.market.Repositories()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repositories": repositories})
}

func (s *Server) handlePluginRepositoryAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	repository, err := s.market.AddRepository(body.Name, body.URL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "repository": repository})
}

func (s *Server) handlePluginRepositoryDelete(w http.ResponseWriter, r *http.Request) {
	err := s.market.RemoveRepository(r.PathValue("id"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, plugin.ErrRepositoryNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handlePluginRepositorySync(w http.ResponseWriter, r *http.Request) {
	repository, err := s.market.Sync(r.Context(), r.PathValue("id"))
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, plugin.ErrRepositoryNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error(), "repository": repository})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "repository": repository})
}

func (s *Server) handlePluginRepositoriesSync(w http.ResponseWriter, r *http.Request) {
	results, err := s.market.SyncAll(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": results})
}

func (s *Server) handlePluginMarketList(w http.ResponseWriter, r *http.Request) {
	plugins, err := s.market.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	repositories, err := s.market.Repositories()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "plugins": plugins, "repositories": repositories})
}

func (s *Server) handlePluginMarketInstall(w http.ResponseWriter, r *http.Request) {
	installed, err := s.market.Install(r.PathValue("repository"), r.PathValue("id"))
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, plugin.ErrMarketPluginMissing) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"id":       installed.Manifest.ID,
		"version":  installed.Manifest.Version,
		"enabled":  installed.Enabled,
		"official": installed.Official,
	})
}

const (
	maxArtifactDownloadBytes = 64 << 20
	maxArtifactBatchBytes    = 256 << 20
	maxArtifactBatchItems    = 500
)

type artifactSummary struct {
	ID         string            `json:"id"`
	Plugin     string            `json:"plugin"`
	Kind       string            `json:"kind"`
	Status     artifact.Status   `json:"status"`
	Email      string            `json:"email,omitempty"`
	Account    string            `json:"account,omitempty"`
	Channel    string            `json:"channel,omitempty"`
	Filename   string            `json:"filename"`
	Labels     map[string]string `json:"labels,omitempty"`
	RunID      string            `json:"run_id,omitempty"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at,omitempty"`
	Payload    json.RawMessage   `json:"payload,omitempty"`
	SecretRefs []string          `json:"secret_refs,omitempty"`
}

func handleArtifactSummary(a artifact.Artifact, includePayload bool) artifactSummary {
	payload := map[string]any{}
	_ = json.Unmarshal(a.Payload, &payload)
	first := func(keys ...string) string {
		for _, key := range keys {
			if value := strings.TrimSpace(a.Labels[key]); value != "" {
				return value
			}
			if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
		return ""
	}
	email := first("email", "mail")
	account := first("username", "account", "login", "name")
	channel := first("channel", "provider", "platform", "source")
	if channel == "" {
		parts := strings.Split(a.Kind, ".")
		if len(parts) > 1 {
			suffix := parts[len(parts)-1]
			switch suffix {
			case "xai", "outlook", "tavily", "github", "chatgpt", "claude", "grok":
				channel = suffix
			}
		}
	}
	if channel == "" {
		channel = strings.TrimSuffix(a.Plugin, "-registrar")
		channel = strings.TrimSuffix(channel, "-accounts")
		channel = strings.TrimSuffix(channel, "-pool")
	}
	filename := first("source_file", "filename")
	if filename == "" {
		filename = a.ID + ".json"
	}
	row := artifactSummary{
		ID: a.ID, Plugin: a.Plugin, Kind: a.Kind, Status: a.Status,
		Email: email, Account: account, Channel: channel,
		Filename: filepath.Base(filename), Labels: a.Labels, RunID: a.RunID,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if includePayload {
		row.Payload = a.Payload
		row.SecretRefs = a.SecretRefs
	}
	return row
}

func artifactMatches(row artifactSummary, labels map[string]string, query string) bool {
	if query == "" {
		return true
	}
	values := []string{row.ID, row.Plugin, row.Kind, string(row.Status), row.Email, row.Account, row.Channel, row.Filename, row.RunID}
	for key, value := range labels {
		values = append(values, key, value)
	}
	return strings.Contains(strings.ToLower(strings.Join(values, " ")), query)
}

func sortedArtifactFacet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Server) handleArtifactsList(w http.ResponseWriter, r *http.Request) {
	pluginID := strings.TrimSpace(r.URL.Query().Get("plugin"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = min(parsed, 200)
		}
	}
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}

	store := artifact.NewStore(s.opt.Paths.ArtifactsDir)
	list, err := store.List("", "", 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	includePayload := r.URL.Query().Get("payload") == "1"
	plugins := map[string]struct{}{}
	kinds := map[string]struct{}{}
	channels := map[string]struct{}{}
	statuses := map[string]struct{}{}
	rows := make([]artifactSummary, 0, len(list))
	for _, item := range list {
		row := handleArtifactSummary(item, includePayload)
		plugins[row.Plugin] = struct{}{}
		kinds[row.Kind] = struct{}{}
		channels[row.Channel] = struct{}{}
		statuses[string(row.Status)] = struct{}{}
		if pluginID != "" && row.Plugin != pluginID {
			continue
		}
		if kind != "" && row.Kind != kind {
			continue
		}
		if status != "" && string(row.Status) != status {
			continue
		}
		if channel != "" && row.Channel != channel {
			continue
		}
		if !artifactMatches(row, item.Labels, query) {
			continue
		}
		rows = append(rows, row)
	}
	total := len(rows)
	totalPages := max(1, (total+limit-1)/limit)
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * limit
	end := min(start+limit, total)
	rows = rows[start:end]
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "artifacts": rows, "store": s.opt.Paths.ArtifactsDir,
		"total": total, "page": page, "limit": limit,
		"total_pages": totalPages,
		"facets": map[string]any{
			"plugins": sortedArtifactFacet(plugins), "kinds": sortedArtifactFacet(kinds),
			"channels": sortedArtifactFacet(channels), "statuses": sortedArtifactFacet(statuses),
		},
	})
}

func (s *Server) handleArtifactDetail(w http.ResponseWriter, r *http.Request) {
	item, err := artifact.NewStore(s.opt.Paths.ArtifactsDir).Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "artifact not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "artifact": handleArtifactSummary(item, true)})
}

func safeArtifactFilename(item artifact.Artifact) string {
	name := handleArtifactSummary(item, false).Filename
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '@', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, name)
	if name == "" || name == "." {
		name = item.ID + ".json"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	return name
}

func artifactPayload(item artifact.Artifact) []byte {
	if len(item.Payload) == 0 {
		return []byte("{}\n")
	}
	return append(append([]byte(nil), item.Payload...), '\n')
}

func (s *Server) handleArtifactDownload(w http.ResponseWriter, r *http.Request) {
	item, err := artifact.NewStore(s.opt.Paths.ArtifactsDir).Get(r.PathValue("id"))
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "artifact not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(item.Payload) > maxArtifactDownloadBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": "artifact payload exceeds download limit"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, safeArtifactFilename(item)))
	_, _ = w.Write(artifactPayload(item))
}

func (s *Server) handleArtifactsDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if len(body.IDs) == 0 || len(body.IDs) > maxArtifactBatchItems {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": fmt.Sprintf("ids must contain 1 to %d artifacts", maxArtifactBatchItems)})
		return
	}
	store := artifact.NewStore(s.opt.Paths.ArtifactsDir)
	all, err := store.List("", "", 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	byID := make(map[string]artifact.Artifact, len(all))
	for _, item := range all {
		byID[item.ID] = item
	}
	items := make([]artifact.Artifact, 0, len(body.IDs))
	seen := map[string]struct{}{}
	var totalBytes int64
	for _, id := range body.IDs {
		id = strings.TrimSpace(id)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		item, exists := byID[id]
		if !exists {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "artifact not found: " + id})
			return
		}
		if len(item.Payload) > maxArtifactDownloadBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": "artifact payload exceeds download limit: " + id})
			return
		}
		totalBytes += int64(len(item.Payload))
		if totalBytes > maxArtifactBatchBytes {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "error": "selected artifacts exceed batch download limit"})
			return
		}
		items = append(items, item)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="artifacts.zip"`)
	archive := zip.NewWriter(w)
	for _, item := range items {
		entry, err := archive.Create(item.ID + "-" + safeArtifactFilename(item))
		if err != nil {
			continue
		}
		_, _ = entry.Write(artifactPayload(item))
	}
	_ = archive.Close()
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
