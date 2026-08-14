package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/degrade"
)

// handleDegradeOverview returns the 降智 picture: verdict counts, scan runner
// status, per-exit flag-2 exposure and recent scans.
func (s *Server) handleDegradeOverview(w http.ResponseWriter, r *http.Request) {
	cfg, _ := config.Load(s.opt.Paths.Config)
	writeJSON(w, 200, map[string]any{
		"ok":       true,
		"overview": s.degrade.Overview(),
		"status":   s.degrade.Status(),
		"exits":    s.degrade.Exits(),
		"history":  s.degrade.History(),
		"settings": map[string]any{
			"model":            cfg.DegradeModel,
			"sample":           cfg.DegradeSample,
			"recheck_min":      cfg.DegradeRecheckMin,
			"exit_window_min":  cfg.DegradeExitWindowMin,
			"exit_account_cap": cfg.DegradeExitAccountCap,
			"exit":             degrade.ExitLabel(cfg.RegisterProxy),
		},
	})
}

// handleDegradeAccounts returns stored verdicts, newest check first.
func (s *Server) handleDegradeAccounts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	items := s.degrade.Records(q.Get("verdict"), q.Get("q"))
	limit := 200
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	total := len(items)
	if len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, 200, map[string]any{"ok": true, "items": items, "total": total})
}

// handleDegradeScan starts a sampled scan. It always runs async: pacing against
// the per-exit account limit means a scan can legitimately sit idle for
// minutes between probes.
func (s *Server) handleDegradeScan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sample  int  `json:"sample"`
		Recheck bool `json:"recheck"`
	}
	_ = decodeJSONBody(r, &body)
	if st := s.degrade.Status(); st.Running {
		writeJSON(w, 409, map[string]any{"ok": false, "error": "降智扫描正在进行中"})
		return
	}
	opt := degrade.ScanOptions{Sample: body.Sample, Recheck: body.Recheck}
	go func() {
		_, _ = s.degrade.Scan(context.Background(), opt)
	}()
	writeJSON(w, 200, map[string]any{"ok": true, "started": true})
}

// handleDegradeIsolate marks one account, or every degraded one, as isolated.
// Isolation is a local flag only — nothing is deleted from the CPA pool.
func (s *Server) handleDegradeIsolate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Isolated *bool  `json:"isolated"`
		AllDown  bool   `json:"all_degraded"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "请求体解析失败"})
		return
	}
	if body.AllDown {
		n := s.degrade.IsolateDegraded()
		writeJSON(w, 200, map[string]any{"ok": true, "isolated": n})
		return
	}
	if body.Name == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "缺少 name"})
		return
	}
	isolated := true
	if body.Isolated != nil {
		isolated = *body.Isolated
	}
	rec, err := s.degrade.SetIsolated(body.Name, isolated)
	if err != nil {
		writeJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "record": rec})
}
