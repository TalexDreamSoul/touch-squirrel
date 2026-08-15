package acctpool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/localpool"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
)

const (
	metaLocalPool = "migrate_local_pool_v1"
	metaTavily    = "migrate_tavily_keys_v1"
)

// MigrateOptions points at legacy on-disk sources.
type MigrateOptions struct {
	LocalPoolDir   string // home/local-pool
	TavilyKeysPath string // plugins-data/tavily-pool/keys.json
	Force          bool   // re-run even if meta set
}

// MigrateReport summarizes what was imported.
type MigrateReport struct {
	LocalPoolImported int  `json:"local_pool_imported"`
	TavilyImported    int  `json:"tavily_imported"`
	Skipped           bool `json:"skipped,omitempty"`
}

// AutoMigrate imports legacy local-pool + tavily keys once (idempotent upsert).
func (s *Store) AutoMigrate(opt MigrateOptions) (MigrateReport, error) {
	var rep MigrateReport
	if s == nil {
		return rep, fmt.Errorf("nil store")
	}

	// always upsert-safe; meta only skips when !Force and sources empty-ish
	if !opt.Force {
		_, lpDone := s.metaGet(metaLocalPool)
		_, tvDone := s.metaGet(metaTavily)
		if lpDone && tvDone {
			rep.Skipped = true
			return rep, nil
		}
	}

	if opt.LocalPoolDir != "" {
		n, err := s.migrateLocalPool(opt.LocalPoolDir)
		if err != nil {
			return rep, fmt.Errorf("local-pool: %w", err)
		}
		rep.LocalPoolImported = n
		_ = s.metaSet(metaLocalPool, time.Now().UTC().Format(time.RFC3339))
	} else {
		_ = s.metaSet(metaLocalPool, "skipped-no-dir")
	}

	if opt.TavilyKeysPath != "" {
		n, err := s.migrateTavilyKeys(opt.TavilyKeysPath)
		if err != nil {
			return rep, fmt.Errorf("tavily: %w", err)
		}
		rep.TavilyImported = n
		_ = s.metaSet(metaTavily, time.Now().UTC().Format(time.RFC3339))
	} else {
		_ = s.metaSet(metaTavily, "skipped-no-path")
	}
	return rep, nil
}

func (s *Store) migrateLocalPool(dir string) (int, error) {
	// Prefer live service (reads index.json + files).
	svc := localpool.New(dir)
	entries := svc.List()
	if len(entries) == 0 {
		// fallback: raw index.json if service empty but file exists
		raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
		if err == nil && len(raw) > 0 {
			var idx struct {
				Items map[string]localpool.Entry `json:"items"`
			}
			if json.Unmarshal(raw, &idx) == nil {
				for _, e := range idx.Items {
					entries = append(entries, e)
				}
			}
		}
	}
	n := 0
	for _, e := range entries {
		label := e.Email
		if label == "" {
			label = e.Name
		}
		status := StatusActive
		meta := map[string]string{
			"hash": e.Hash,
		}
		if e.SyncError != "" {
			meta["sync_error"] = e.SyncError
		}
		if e.SyncedAt != nil {
			meta["synced_at"] = e.SyncedAt.UTC().Format(time.RFC3339)
		}
		if e.SyncTarget != "" {
			meta["sync_target"] = e.SyncTarget
		}
		if e.Size > 0 {
			meta["size"] = fmt.Sprintf("%d", e.Size)
		}
		secretRef := filepath.Join(dir, e.Name)
		_, err := s.Upsert(Account{
			Type:       TypeXAI,
			Plugin:     "xai-accounts",
			Label:      label,
			Status:     status,
			Email:      e.Email,
			ExternalID: e.Name,
			SecretRef:  secretRef,
			Meta:       meta,
			Source:     "local-pool",
			RunID:      e.SourceRun,
			CreatedAt:  e.AddedAt.UTC().Format(time.RFC3339),
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) RefreshTavily(path string) (int, error) {
	if s == nil || strings.TrimSpace(path) == "" {
		return 0, nil
	}
	return s.migrateTavilyKeys(path)
}

func (s *Store) migrateTavilyKeys(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var snap tavilypool.Snapshot
	if len(strings.TrimSpace(string(b))) == 0 {
		return 0, nil
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return 0, err
	}
	n := 0
	for _, k := range snap.Keys {
		status := string(k.Status)
		if status == "" {
			status = StatusActive
		}
		label := k.Note
		if label == "" {
			label = maskKey(k.APIKey)
		}
		meta := map[string]string{
			"success": fmt.Sprintf("%d", k.Success),
			"failure": fmt.Sprintf("%d", k.Failure),
			"masked":  maskKey(k.APIKey),
		}
		if k.LastUsedUnix > 0 {
			meta["last_used_unix"] = fmt.Sprintf("%d", k.LastUsedUnix)
		}
		if k.ExhaustedUntil > 0 {
			meta["exhausted_until"] = fmt.Sprintf("%d", k.ExhaustedUntil)
		}
		lastUsed := ""
		if k.LastUsedUnix > 0 {
			lastUsed = time.Unix(k.LastUsedUnix, 0).UTC().Format(time.RFC3339)
		}
		_, err := s.Upsert(Account{
			Type:       TypeTavily,
			Plugin:     "tavily-pool",
			Label:      label,
			Status:     status,
			ExternalID: k.ID,
			SecretRef:  path + "#" + k.ID,
			Meta:       meta,
			Source:     "tavily-pool",
			CreatedAt:  k.CreatedAt,
			LastUsedAt: lastUsed,
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "…" + k[len(k)-4:]
}

// UpsertLocalEntries indexes local-pool credential entries into the unified store.
func (s *Store) UpsertLocalEntries(dir string, entries []localpool.Entry) (int, error) {
	if s == nil || len(entries) == 0 {
		return 0, nil
	}
	n := 0
	for _, e := range entries {
		if err := s.upsertLocalEntry(dir, e); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// UpsertLocalFromDir reindexes the whole local-pool directory.
func (s *Store) UpsertLocalFromDir(dir string) (int, error) {
	if s == nil || strings.TrimSpace(dir) == "" {
		return 0, nil
	}
	return s.migrateLocalPool(dir)
}

// UpsertTavilyKey indexes one tavily key row.
func (s *Store) UpsertTavilyKey(statePath string, k tavilypool.Key) error {
	if s == nil {
		return nil
	}
	status := string(k.Status)
	if status == "" {
		status = StatusActive
	}
	label := k.Note
	if label == "" {
		label = maskKey(k.APIKey)
	}
	meta := map[string]string{
		"success": fmt.Sprintf("%d", k.Success),
		"failure": fmt.Sprintf("%d", k.Failure),
		"masked":  maskKey(k.APIKey),
	}
	if k.LastUsedUnix > 0 {
		meta["last_used_unix"] = fmt.Sprintf("%d", k.LastUsedUnix)
	}
	if k.ExhaustedUntil > 0 {
		meta["exhausted_until"] = fmt.Sprintf("%d", k.ExhaustedUntil)
	}
	lastUsed := ""
	if k.LastUsedUnix > 0 {
		lastUsed = time.Unix(k.LastUsedUnix, 0).UTC().Format(time.RFC3339)
	}
	_, err := s.Upsert(Account{
		Type:       TypeTavily,
		Plugin:     "tavily-pool",
		Label:      label,
		Status:     status,
		ExternalID: k.ID,
		SecretRef:  statePath + "#" + k.ID,
		Meta:       meta,
		Source:     "tavily-pool",
		CreatedAt:  k.CreatedAt,
		LastUsedAt: lastUsed,
	})
	return err
}

// SetStatusByExternal updates status for (type, external_id).
func (s *Store) SetStatusByExternal(typ, externalID, status string) error {
	if s == nil {
		return nil
	}
	typ = strings.TrimSpace(typ)
	externalID = strings.TrimSpace(externalID)
	status = strings.TrimSpace(status)
	if typ == "" || externalID == "" || status == "" {
		return fmt.Errorf("type, external_id, status required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(
		`UPDATE accounts SET status = ?, updated_at = ? WHERE type = ? AND external_id = ?`,
		status, now, typ, externalID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// soft no-op if not yet migrated
		return nil
	}
	return nil
}

func (s *Store) upsertLocalEntry(dir string, e localpool.Entry) error {
	label := e.Email
	if label == "" {
		label = e.Name
	}
	meta := map[string]string{"hash": e.Hash}
	if e.SyncError != "" {
		meta["sync_error"] = e.SyncError
	}
	if e.SyncedAt != nil {
		meta["synced_at"] = e.SyncedAt.UTC().Format(time.RFC3339)
	}
	if e.SyncTarget != "" {
		meta["sync_target"] = e.SyncTarget
	}
	if e.Size > 0 {
		meta["size"] = fmt.Sprintf("%d", e.Size)
	}
	created := ""
	if !e.AddedAt.IsZero() {
		created = e.AddedAt.UTC().Format(time.RFC3339)
	}
	_, err := s.Upsert(Account{
		Type:       TypeXAI,
		Plugin:     "xai-accounts",
		Label:      label,
		Status:     StatusActive,
		Email:      e.Email,
		ExternalID: e.Name,
		SecretRef:  filepath.Join(dir, e.Name),
		Meta:       meta,
		Source:     "local-pool",
		RunID:      e.SourceRun,
		CreatedAt:  created,
	})
	return err
}
