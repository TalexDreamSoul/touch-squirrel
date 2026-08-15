package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/localpool"
	"github.com/grok-free-register/grok-reg/internal/state"
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
			go s.syncLocalPool(false)
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
		go s.syncLocalPool(false)
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

	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if strings.TrimSpace(cfg.CPAManagementKey) == "" || strings.TrimSpace(cfg.CPAManagementBase) == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "未配置 CPA_MANAGEMENT_BASE / KEY，无法同步到主号池"})
		return
	}

	okN, failN, total, samples := s.syncLocalPool(body.All)
	writeJSON(w, 200, map[string]any{
		"ok":      failN == 0,
		"synced":  okN,
		"failed":  failN,
		"total":   total,
		"samples": samples,
		"target":  cfg.CPAManagementBase,
	})
}

func (s *Server) shouldAutoSync() bool {
	cfg, err := config.Load(s.opt.Paths.Config)
	return err == nil && cfg.LocalPoolAutoSync
}

func (s *Server) syncLocalPool(all bool) (okN, failN, total int, samples []string) {
	cfg, err := config.Load(s.opt.Paths.Config)
	if err != nil || strings.TrimSpace(cfg.CPAManagementKey) == "" {
		return 0, 0, 0, nil
	}
	up := cpa.NewUploader(cpa.UploadConfig{
		Enabled:      true,
		BaseURL:      cfg.CPAManagementBase,
		Key:          cfg.CPAManagementKey,
		TimeoutSec:   max(cfg.CPAUploadTimeoutSec, 30),
		Retries:      cfg.CPAUploadRetries,
		NameTemplate: cfg.CPAUploadNameTemplate,
		Verify:       cfg.CPAUploadVerify,
		Mode:         cfg.CPAUploadMode,
	}, nil)

	paths := s.localPool.UnsyncedPaths()
	if all {
		paths = s.localPool.AllPaths()
	}
	total = len(paths)
	var okNames []string
	for _, p := range paths {
		res := up.UploadFile(p)
		name := filepath.Base(p)
		if res.OK {
			okN++
			okNames = append(okNames, name)
			continue
		}
		failN++
		errMsg := "upload failed"
		if res.Err != nil {
			errMsg = res.Err.Error()
		}
		_ = s.localPool.MarkSynced([]string{name}, cfg.CPAManagementBase, fmt.Errorf("%s", errMsg))
		if len(samples) < 5 {
			samples = append(samples, name+": "+errMsg)
		}
	}
	_ = s.localPool.MarkSynced(okNames, cfg.CPAManagementBase, nil)
	// reindex so synced_at lands in unified accounts
	if s.accounts != nil {
		_, _ = s.accounts.UpsertLocalFromDir(s.opt.Paths.LocalPool)
	}
	return okN, failN, total, samples
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
		go s.syncLocalPool(false)
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
