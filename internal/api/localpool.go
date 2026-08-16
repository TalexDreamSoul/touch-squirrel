package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/jobs"
	"github.com/grok-free-register/grok-reg/internal/localpool"
	"github.com/grok-free-register/grok-reg/internal/state"
	"github.com/grok-free-register/grok-reg/internal/transfer"
)

func (s *Server) handleLocalPoolList(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePage(r, 1, 10, 100)
	all := s.localPool.List()
	totalAll, unsynced := s.localPool.Stats()
	total := len(all)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	items := all[start:end]
	if items == nil {
		items = []localpool.Entry{}
	}
	_ = totalAll
	writeJSON(w, 200, map[string]any{
		"ok":          true,
		"total":       total,
		"unsynced":    unsynced,
		"items":       items,
		"dir":         s.localPool.Dir(),
		"page":        page,
		"page_size":   pageSize,
		"total_pages": pageCount(total, pageSize),
	})
}

func (s *Server) handleLocalPoolImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RunID string `json:"run_id"`
		All   bool   `json:"all"`
	}
	_ = decodeJSONBody(r, &body)
	if body.RunID != "" {
		dir, err := s.resolveRun(body.RunID)
		if err != nil {
			writeJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		added, entries, err := s.localPool.ImportRun(dir)
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		s.indexLocalEntries(entries)
		if len(entries) > 0 {
			s.runArtifactBackfill()
		}
		importedCount := recordRunImport(dir)
		if s.shouldAutoSync() {
			go func() { _, _, _ = s.enqueueLocalPoolUpload(false) }()
		}
		writeJSON(w, 200, map[string]any{
			"ok":             true,
			"run_id":         body.RunID,
			"added":          added,
			"entries":        entries,
			"imported_count": importedCount,
		})
		return
	}
	lookback := 5
	if body.All {
		lookback = 50
	}
	runID, added, entries, err := s.localPool.ImportLatest(s.opt.Paths.Outputs, lookback)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	s.indexLocalEntries(entries)
	if len(entries) > 0 {
		s.runArtifactBackfill()
	}
	if runID != "" {
		if dir, resolveErr := s.resolveRun(runID); resolveErr == nil {
			recordRunImport(dir)
		}
	}
	if s.shouldAutoSync() {
		go func() { _, _, _ = s.enqueueLocalPoolUpload(false) }()
	}
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"run_id":  runID,
		"added":   added,
		"entries": entries,
	})
}

func (s *Server) handleLocalPoolSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		All bool `json:"all"`
	}
	_ = decodeJSONBody(r, &body)
	job, total, err := s.enqueueLocalPoolUpload(body.All)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if job == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "queued": 0, "total": 0})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok": true, "queued": total, "total": total, "job_id": job.ID,
		"job": s.transfer.UploadSummary(job),
	})
}

func (s *Server) shouldAutoSync() bool {
	cfg, err := config.Load(s.opt.Paths.Config)
	return err == nil && cfg.LocalPoolAutoSync
}

func (s *Server) enqueueLocalPoolUpload(all bool) (*jobs.Job, int, error) {
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(cfg.CPAManagementKey) == "" || strings.TrimSpace(cfg.CPAManagementBase) == "" {
		return nil, 0, fmt.Errorf("未配置 CPA_MANAGEMENT_BASE / KEY，无法同步到主号池")
	}
	paths := s.localPool.UnsyncedPaths()
	if all {
		paths = s.localPool.AllPaths()
	}
	if len(paths) == 0 {
		return nil, 0, nil
	}
	files := make([]transfer.UploadFile, 0, len(paths))
	for _, path := range paths {
		raw, readErr := readBoundedCredential(path)
		if readErr != nil {
			return nil, 0, fmt.Errorf("读取 %s: %w", filepath.Base(path), readErr)
		}
		files = append(files, transfer.UploadFile{Name: filepath.Base(path), Content: raw})
	}
	job, err := s.transfer.PrepareUploadFiles(files, transfer.UploadOptions{
		Source: "local-sync", SkipCached: false, SkipCachedSet: true,
	})
	if err != nil {
		return nil, 0, err
	}
	s.transfer.SetUploadSuccessHook(job.ID, func(name string) error {
		return s.localPool.MarkSynced([]string{name}, cfg.CPAManagementBase, nil)
	})
	s.transfer.SetUploadFailureHook(job.ID, func(name, message string) {
		_ = s.localPool.MarkSynced([]string{name}, cfg.CPAManagementBase, fmt.Errorf("%s", message))
	})
	if !s.transfer.StartUploadJob(job, false) {
		return nil, 0, fmt.Errorf("上传任务无法入队")
	}
	return job, len(files), nil
}

// autoImportLatestRun imports CPA results when auto-import is enabled.
func (s *Server) autoImportLatestRun(runID string) {
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil || !cfg.LocalPoolAutoImport {
		return
	}
	var entries []localpool.Entry
	importedRunID := runID
	if runID != "" {
		if dir, resolveErr := s.resolveRun(runID); resolveErr == nil {
			_, entries, _ = s.localPool.ImportRun(dir)
			recordRunImport(dir)
		}
	} else {
		importedRunID, _, entries, _ = s.localPool.ImportLatest(s.opt.Paths.Outputs, 3)
		if importedRunID != "" {
			if dir, resolveErr := s.resolveRun(importedRunID); resolveErr == nil {
				recordRunImport(dir)
			}
		}
	}
	s.indexLocalEntries(entries)
	if len(entries) > 0 {
		s.runArtifactBackfill()
	}
	if cfg.LocalPoolAutoSync {
		go func() { _, _, _ = s.enqueueLocalPoolUpload(false) }()
	}
}

func recordRunImport(runDir string) int {
	files, _ := cpa.CollectCPAJSON(runDir)
	count := len(files)
	_ = state.UpdateRun(runDir, func(snapshot *state.Snapshot) {
		snapshot.ImportedCount = count
		snapshot.ImportedAt = time.Now().UTC().Format(time.RFC3339)
	})
	return count
}

func (s *Server) indexLocalEntries(entries []localpool.Entry) {
	if s.accounts == nil {
		return
	}
	if len(entries) > 0 {
		_, _ = s.accounts.UpsertLocalEntries(s.opt.Paths.LocalPool, entries)
		return
	}
	// fallback full reindex when caller didn't return entries
	_, _ = s.accounts.UpsertLocalFromDir(s.opt.Paths.LocalPool)
}
