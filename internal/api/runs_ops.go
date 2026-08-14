package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/runmetrics"
	"github.com/grok-free-register/grok-reg/internal/state"
)

func runMetricsSummary(runDir string) (durationMS, averageAccountMS int64, accountCount int) {
	snapshot, err := runmetrics.Load(runDir)
	if err != nil {
		return 0, 0, 0
	}
	var total int64
	for _, account := range snapshot.Accounts {
		if account.DurationMS <= 0 {
			continue
		}
		total += account.DurationMS
		accountCount++
	}
	if accountCount > 0 {
		averageAccountMS = total / int64(accountCount)
	}
	return snapshot.DurationMS, averageAccountMS, accountCount
}

func (s *Server) handleRunMetrics(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir, err := s.resolveRun(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	snapshot, err := runmetrics.Load(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run_id": id, "metrics_available": false})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"run_id":            id,
		"metrics_available": true,
		"metrics":           snapshot,
		"summary":           runmetrics.BuildAggregate([]runmetrics.Snapshot{snapshot}),
	})
}

func (s *Server) handleRegisterMetrics(w http.ResponseWriter, r *http.Request) {
	rangeName := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("range")))
	if rangeName == "" {
		rangeName = "7d"
	}
	var cutoff time.Time
	switch rangeName {
	case "24h":
		cutoff = time.Now().Add(-24 * time.Hour)
	case "7d":
		cutoff = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		cutoff = time.Now().Add(-30 * 24 * time.Hour)
	case "all":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "range must be 24h|7d|30d|all"})
		return
	}

	dirs, err := cpa.ListRunDirs(s.opt.Paths.Outputs, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	runs := make([]runmetrics.Snapshot, 0, len(dirs))
	for _, dir := range dirs {
		snapshot, loadErr := runmetrics.Load(dir)
		if loadErr != nil {
			continue
		}
		if !cutoff.IsZero() {
			started, parseErr := time.Parse(time.RFC3339Nano, snapshot.StartedAt)
			if parseErr != nil || started.Before(cutoff) {
				continue
			}
		}
		runs = append(runs, snapshot)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"range":   rangeName,
		"summary": runmetrics.BuildAggregate(runs),
	})
}

func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.resolveRun(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	tail := 400
	if value := r.URL.Query().Get("tail"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 5000 {
			tail = parsed
		}
	}
	path := filepath.Join(s.opt.Paths.LogsDir, fmt.Sprintf("run-%s.log", id))
	text, err := readLogTail(path, tail)
	if err != nil {
		code := http.StatusInternalServerError
		if os.IsNotExist(err) {
			code = http.StatusNotFound
		}
		writeJSON(w, code, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run_id": id, "path": path, "log": text})
}

func readLogTail(path string, tail int) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	if len(lines) > tail {
		text = strings.Join(lines[len(lines)-tail:], "\n")
	}
	return text, nil
}

func (s *Server) handleRunDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dir, err := s.resolveRun(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	store := state.NewStore(s.opt.Paths.State)
	current, _ := store.Load()
	if current.RunID == id && current.Status == state.StatusRunning {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "运行中的任务不能删除"})
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = os.Remove(filepath.Join(s.opt.Paths.LogsDir, fmt.Sprintf("run-%s.log", id)))
	if current.RunID == id {
		_ = store.Set(func(snapshot *state.Snapshot) {
			*snapshot = state.Snapshot{Status: state.StatusStopped, Phase: state.PhaseIdle, PhaseDetail: "任务记录已删除"}
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "run_id": id, "deleted": true})
}
