package api

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/acctpool"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
)

// handleFederationPoolList exposes the master's formal CPA pool metadata
// when ClusterSharePoolList is enabled. Auth: federation token.
func (s *Server) handleFederationPoolList(w http.ResponseWriter, r *http.Request) {
	tok := federationToken(r)
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if code, msg := s.clusterAuthorize(cfg, tok); code != 0 {
		writeJSON(w, code, map[string]any{"ok": false, "error": msg})
		return
	}
	if !cfg.ClusterSharePoolList {
		writeJSON(w, 403, map[string]any{
			"ok":    false,
			"error": "主节点未开启号池列表分享（CLUSTER_SHARE_POOL_LIST）",
		})
		return
	}
	page, pageSize := parsePage(r, 1, 10, 100)
	list, total, err := s.transfer.ListRemotePage("", "", true, page, pageSize)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// strip nothing extra — AuthMeta is already slim
	writeJSON(w, 200, map[string]any{
		"ok":              true,
		"source":          "federation",
		"share_pool_list": true,
		"share_pool_pull": cfg.ClusterSharePoolPull,
		"total":           total,
		"page":            page,
		"page_size":       pageSize,
		"total_pages":     pageCount(total, pageSize),
		"files":           list,
		"capabilities": map[string]bool{
			"download":    cfg.ClusterSharePoolPull,
			"upload_cpa":  cfg.ClusterSharePoolPull,
			"enable":      false,
			"disable":     false,
			"delete":      false,
			"time_filter": false,
		},
		"master_name": firstNonEmpty(cfg.ClusterNodeName, "master"),
	})
}

// handleFederationPoolPull downloads one credential when ClusterSharePoolPull is on.
func (s *Server) handleFederationPoolPull(w http.ResponseWriter, r *http.Request) {
	tok := federationToken(r)
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if code, msg := s.clusterAuthorize(cfg, tok); code != 0 {
		writeJSON(w, code, map[string]any{"ok": false, "error": msg})
		return
	}
	if !cfg.ClusterSharePoolPull {
		writeJSON(w, 403, map[string]any{
			"ok":    false,
			"error": "主节点未开启号池凭证拉取（CLUSTER_SHARE_POOL_PULL）",
		})
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = strings.TrimSpace(r.PathValue("name"))
	}
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid name"})
		return
	}
	client := cpa.NewClient(cfg.CPAManagementBase, cfg.CPAManagementKey, max(cfg.CPAUploadTimeoutSec, 30))
	raw, err := client.Download(name)
	if err != nil {
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}

// handleUnifiedPoolList is the panel-side multi-source list:
//
//	source=accounts|local|cloud|federation  (default accounts)
//	type=xai|tavily|…                      (accounts only)
//	plugin=…  status=…  q=…                (accounts only)
//	master=<base url> when source=federation
func (s *Server) handleUnifiedPoolList(w http.ResponseWriter, r *http.Request) {
	if !s.allowSensitiveRequest(w, r) {
		return
	}
	q := r.URL.Query()
	source := strings.ToLower(strings.TrimSpace(q.Get("source")))
	if source == "" {
		source = "accounts"
	}
	page, pageSize := parsePage(r, 1, 10, 100)

	switch source {
	case "accounts", "unified", "all":
		if s.accounts == nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "accounts store unavailable"})
			return
		}
		s.tavilyMu.Lock()
		_, _ = s.accounts.RefreshTavily(tavilypool.DefaultStatePath(s.opt.Paths.Root))
		s.tavilyMu.Unlock()
		field, from, to, filterErr := parsePoolTimeFilter(q)
		if filterErr != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": filterErr.Error()})
			return
		}
		res, err := s.accounts.List(acctpool.ListFilter{
			Type:      q.Get("type"),
			Plugin:    q.Get("plugin"),
			Status:    q.Get("status"),
			Q:         q.Get("q"),
			TimeField: field,
			From:      from,
			To:        to,
			Page:      page,
			Limit:     pageSize,
		})
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		types, _ := s.accounts.Types()
		writeJSON(w, 200, map[string]any{
			"ok":           true,
			"source":       "accounts",
			"items":        res.Items,
			"total":        res.Total,
			"page":         res.Page,
			"page_size":    res.PageSize,
			"total_pages":  res.TotalPages,
			"by_type":      res.ByType,
			"types":        types,
			"db":           s.accounts.Path(),
			"capabilities": poolCapabilities("accounts", true),
		})
	case "local":
		all := s.localPool.List()
		total, unsynced := s.localPool.Stats()
		start := (page - 1) * pageSize
		if start > len(all) {
			start = len(all)
		}
		end := start + pageSize
		if end > len(all) {
			end = len(all)
		}
		items := all[start:end]
		writeJSON(w, 200, map[string]any{
			"ok":           true,
			"source":       "local",
			"total":        total,
			"unsynced":     unsynced,
			"page":         page,
			"page_size":    pageSize,
			"total_pages":  pageCount(len(all), pageSize),
			"items":        items,
			"capabilities": poolCapabilities("local", true),
		})
	case "cloud":
		list, total, err := s.transfer.ListRemotePage("", "", true, page, pageSize)
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error(), "source": "cloud"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok":           true,
			"source":       "cloud",
			"total":        total,
			"page":         page,
			"page_size":    pageSize,
			"total_pages":  pageCount(total, pageSize),
			"files":        list,
			"can_pull":     true, // local panel has CPA key
			"capabilities": poolCapabilities("cloud", true),
		})
	case "federation", "master", "fed":
		cfg, err := config.Load(s.opt.Paths.Config)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		master, masterToken, err := configuredFederationMaster(cfg, q.Get("master"))
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		body, status, err := federationGET(master, "/api/federation/pool", masterToken, map[string]string{
			"page":  strconv.Itoa(page),
			"limit": strconv.Itoa(pageSize),
		})
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error(), "source": "federation", "master": master})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	default:
		writeJSON(w, 400, map[string]any{"ok": false, "error": "source 须为 local|cloud|federation"})
	}
}

func parsePoolTimeFilter(query url.Values) (field, from, to string, err error) {
	field = strings.TrimSpace(query.Get("time_field"))
	if field == "" {
		field = "created_at"
	}
	switch field {
	case "created_at", "updated_at", "last_used_at":
	default:
		return "", "", "", fmt.Errorf("time_field 须为 created_at|updated_at|last_used_at")
	}
	from = strings.TrimSpace(query.Get("from"))
	to = strings.TrimSpace(query.Get("to"))
	var fromTime, toTime time.Time
	if from != "" {
		fromTime, err = time.Parse(time.RFC3339, from)
		if err != nil {
			return "", "", "", fmt.Errorf("from 须为 RFC3339 时间")
		}
		from = fromTime.UTC().Format(time.RFC3339)
	}
	if to != "" {
		toTime, err = time.Parse(time.RFC3339, to)
		if err != nil {
			return "", "", "", fmt.Errorf("to 须为 RFC3339 时间")
		}
		to = toTime.UTC().Format(time.RFC3339)
	}
	if !fromTime.IsZero() && !toTime.IsZero() && !fromTime.Before(toTime) {
		return "", "", "", fmt.Errorf("from 必须早于 to")
	}
	return field, from, to, nil
}

func poolCapabilities(source string, federationPull bool) map[string]bool {
	switch source {
	case "accounts":
		return map[string]bool{"enable": true, "disable": true, "upload_cpa": true, "download": true, "delete": true, "time_filter": true}
	case "local":
		return map[string]bool{"enable": false, "disable": false, "upload_cpa": true, "download": true, "delete": true, "time_filter": false}
	case "cloud":
		return map[string]bool{"enable": false, "disable": false, "upload_cpa": false, "download": true, "delete": true, "time_filter": false}
	case "federation":
		return map[string]bool{"enable": false, "disable": false, "upload_cpa": federationPull, "download": federationPull, "delete": false, "time_filter": false}
	default:
		return map[string]bool{}
	}
}

// handleUnifiedPoolPull downloads from cloud (local CPA) or federation master.
func (s *Server) handleUnifiedPoolPull(w http.ResponseWriter, r *http.Request) {
	if !s.allowSensitiveRequest(w, r) {
		return
	}
	q := r.URL.Query()
	source := strings.ToLower(strings.TrimSpace(q.Get("source")))
	name := strings.TrimSpace(q.Get("name"))
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid name"})
		return
	}
	switch source {
	case "", "cloud", "local-cpa":
		cfg, err := config.Load(s.opt.Paths.Config)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		client := cpa.NewClient(cfg.CPAManagementBase, cfg.CPAManagementKey, max(cfg.CPAUploadTimeoutSec, 30))
		raw, err := client.Download(name)
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		w.WriteHeader(200)
		_, _ = w.Write(raw)
	case "federation", "master", "fed":
		cfg, err := config.Load(s.opt.Paths.Config)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		master, masterToken, err := configuredFederationMaster(cfg, q.Get("master"))
		if err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		body, status, err := federationGET(master, "/api/federation/pool/pull", masterToken, map[string]string{
			"name": name,
		})
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if status >= 400 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		w.WriteHeader(200)
		_, _ = w.Write(body)
	default:
		writeJSON(w, 400, map[string]any{"ok": false, "error": "source 须为 cloud|federation"})
	}
}

func (s *Server) clusterAuthorize(cfg config.Config, token string) (int, string) {
	// reuse cluster service's constant-time check via PublicInfo path
	_, code, msg := s.cluster.PublicInfo(token)
	if code != 0 {
		return code, msg
	}
	return 0, ""
}

func configuredFederationMaster(cfg config.Config, requested string) (string, string, error) {
	requested = strings.TrimRight(strings.TrimSpace(requested), "/")
	endpoints := cfg.ClusterMasterEndpoints()
	if requested == "" && len(endpoints) > 0 {
		requested = strings.TrimRight(endpoints[0].URL, "/")
	}
	for _, endpoint := range endpoints {
		candidate := strings.TrimRight(strings.TrimSpace(endpoint.URL), "/")
		if candidate != requested {
			continue
		}
		token := strings.TrimSpace(endpoint.Token)
		if token == "" {
			token = strings.TrimSpace(cfg.ClusterPublicToken)
		}
		return candidate, token, nil
	}
	return "", "", fmt.Errorf("master must match a configured cluster endpoint")
}

func federationGET(masterBase, path, token string, query map[string]string) ([]byte, int, error) {
	u, err := url.Parse(strings.TrimRight(masterBase, "/") + path)
	if err != nil {
		return nil, 0, err
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("X-Cluster-Token", token)
	}
	limit := int64(8 << 20)
	if strings.HasSuffix(path, "/pull") {
		limit = maxPoolFileBytes
	}
	client := &http.Client{
		Timeout: 45 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if int64(len(b)) > limit {
		return nil, res.StatusCode, fmt.Errorf("federation response exceeds %d MiB", limit>>20)
	}
	return b, res.StatusCode, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
