package api

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/grok-free-register/grok-reg/internal/acctpool"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
)

const (
	maxPoolBatchItems = 500
	maxPoolFileBytes  = 64 << 20
	maxPoolBatchBytes = 256 << 20
)

var poolDownloadSlots = make(chan struct{}, 2)

type poolBatchRequest struct {
	Source      string   `json:"source"`
	Action      string   `json:"action,omitempty"`
	IDs         []string `json:"ids"`
	Master      string   `json:"master,omitempty"`
	masterToken string
}

type poolBatchItemResult struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func decodePoolBatchRequest(r *http.Request, requireAction bool) (poolBatchRequest, error) {
	var request poolBatchRequest
	if err := decodeJSONBody(r, &request); err != nil {
		return request, fmt.Errorf("invalid JSON body: %w", err)
	}
	request.Source = strings.ToLower(strings.TrimSpace(request.Source))
	switch request.Source {
	case "accounts", "local", "cloud", "federation":
	case "unified", "all":
		request.Source = "accounts"
	default:
		return request, fmt.Errorf("source 须为 accounts|local|cloud|federation")
	}
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	if requireAction {
		switch request.Action {
		case "enable", "disable", "upload_cpa", "delete":
		default:
			return request, fmt.Errorf("unsupported batch action %q", request.Action)
		}
	}
	if len(request.IDs) == 0 {
		return request, fmt.Errorf("ids cannot be empty")
	}
	if len(request.IDs) > maxPoolBatchItems {
		return request, fmt.Errorf("最多选择 %d 条凭证", maxPoolBatchItems)
	}
	seen := make(map[string]struct{}, len(request.IDs))
	for i, id := range request.IDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return request, fmt.Errorf("ids cannot contain empty values")
		}
		if _, exists := seen[id]; exists {
			return request, fmt.Errorf("duplicate id %q", id)
		}
		seen[id] = struct{}{}
		request.IDs[i] = id
	}
	request.Master = strings.TrimRight(strings.TrimSpace(request.Master), "/")
	if request.Source == "federation" && request.Master == "" {
		return request, fmt.Errorf("federation source requires master")
	}
	return request, nil
}

func (s *Server) handlePoolBatch(w http.ResponseWriter, r *http.Request) {
	if !s.allowSensitiveRequest(w, r) {
		return
	}
	request, err := decodePoolBatchRequest(r, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	capability := request.Action
	if !poolCapabilities(request.Source, true)[capability] {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": fmt.Sprintf("%s source does not support %s", request.Source, request.Action),
		})
		return
	}

	var cfg config.Config
	if request.Action == "upload_cpa" || request.Source == "cloud" || request.Source == "federation" {
		cfg, err = config.Load(s.opt.Paths.Config)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	var uploader *cpa.Uploader
	if request.Source == "federation" {
		request.Master, request.masterToken, err = configuredFederationMaster(cfg, request.Master)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	if request.Action == "upload_cpa" {
		uploader, err = poolUploader(cfg)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	var cpaClient *cpa.Client
	if request.Source == "cloud" {
		cpaClient = cpa.NewClient(cfg.CPAManagementBase, cfg.CPAManagementKey, max(cfg.CPAUploadTimeoutSec, 30))
	}
	var accounts map[string]acctpool.Account
	if request.Source == "accounts" {
		if s.accounts == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "accounts store unavailable"})
			return
		}
		accounts, err = s.accounts.GetMany(request.IDs)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}

	results := make([]poolBatchItemResult, 0, len(request.IDs))
	succeeded := 0
	for _, id := range request.IDs {
		itemErr := s.applyPoolBatchItem(request, id, accounts[id], uploader, cpaClient, cfg)
		result := poolBatchItemResult{ID: id, OK: itemErr == nil}
		if itemErr != nil {
			result.Error = itemErr.Error()
		} else {
			succeeded++
		}
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        succeeded == len(request.IDs),
		"source":    request.Source,
		"action":    request.Action,
		"total":     len(request.IDs),
		"succeeded": succeeded,
		"failed":    len(request.IDs) - succeeded,
		"results":   results,
	})
}

func (s *Server) applyPoolBatchItem(
	request poolBatchRequest,
	id string,
	account acctpool.Account,
	uploader *cpa.Uploader,
	cpaClient *cpa.Client,
	cfg config.Config,
) error {
	switch request.Source {
	case "accounts":
		if account.ID == "" {
			return fmt.Errorf("account not found")
		}
		if account.Type == acctpool.TypeTavily && (request.Action == "enable" || request.Action == "disable" || request.Action == "delete") {
			s.tavilyMu.Lock()
			defer s.tavilyMu.Unlock()
		}
		switch request.Action {
		case "enable", "disable":
			status := acctpool.StatusActive
			if request.Action == "disable" {
				status = acctpool.StatusDisabled
			}
			if account.Type == acctpool.TypeTavily {
				pool := tavilypool.New(tavilypool.DefaultStatePath(s.opt.Paths.Root))
				previous, err := pool.Get(account.ExternalID, true)
				if err != nil {
					return err
				}
				poolStatus := tavilypool.StatusActive
				if status == acctpool.StatusDisabled {
					poolStatus = tavilypool.StatusDisabled
				}
				if err := pool.SetStatus(account.ExternalID, poolStatus); err != nil {
					return err
				}
				if err := s.accounts.SetStatus(account.ID, status); err != nil {
					if rollbackErr := pool.SetStatus(account.ExternalID, previous.Status); rollbackErr != nil {
						return fmt.Errorf("update index: %v; rollback source: %v", err, rollbackErr)
					}
					return err
				}
				return nil
			}
			return s.accounts.SetStatus(account.ID, status)
		case "upload_cpa":
			if account.Type != acctpool.TypeXAI {
				return fmt.Errorf("credential type %s is not CPA-compatible", account.Type)
			}
			name, raw, err := s.readAccountCredential(account)
			if err != nil {
				return err
			}
			result := uploader.UploadBytes(name, raw)
			if !result.OK {
				return uploadResultError(result)
			}
			return nil
		case "delete":
			if err := s.accounts.Delete(account.ID); err != nil {
				return err
			}
			if err := s.deleteAccountSource(account); err != nil {
				if _, rollbackErr := s.accounts.Upsert(account); rollbackErr != nil {
					return fmt.Errorf("delete source: %v; rollback index: %v", err, rollbackErr)
				}
				return err
			}
			return nil
		}
	case "local":
		switch request.Action {
		case "upload_cpa":
			path, err := s.localPool.PathFor(id)
			if err != nil {
				return err
			}
			raw, err := readBoundedCredential(path)
			if err != nil {
				return err
			}
			result := uploader.UploadBytes(id, raw)
			if !result.OK {
				return uploadResultError(result)
			}
			if err := s.localPool.MarkSynced([]string{id}, cfg.CPAManagementBase, nil); err != nil {
				return fmt.Errorf("uploaded but failed to persist sync metadata: %w", err)
			}
			return nil
		case "delete":
			var indexed acctpool.Account
			hasIndex := false
			if s.accounts != nil {
				indexed, err := s.accounts.GetByExternal(acctpool.TypeXAI, id)
				if err == nil {
					hasIndex = true
					if err := s.accounts.Delete(indexed.ID); err != nil {
						return err
					}
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			if err := s.localPool.Delete(id); err != nil {
				if hasIndex {
					if _, rollbackErr := s.accounts.Upsert(indexed); rollbackErr != nil {
						return fmt.Errorf("delete local credential: %v; rollback index: %v", err, rollbackErr)
					}
				}
				return err
			}
			return nil
		}
	case "cloud":
		if request.Action == "delete" {
			return cpaClient.Delete(id)
		}
	case "federation":
		if request.Action == "upload_cpa" {
			raw, err := fetchFederationCredential(request.Master, id, request.masterToken)
			if err != nil {
				return err
			}
			result := uploader.UploadBytes(id, raw)
			if !result.OK {
				return uploadResultError(result)
			}
			return nil
		}
	}
	return fmt.Errorf("unsupported action")
}

func (s *Server) handlePoolBatchDownload(w http.ResponseWriter, r *http.Request) {
	if !s.allowSensitiveRequest(w, r) {
		return
	}
	request, err := decodePoolBatchRequest(r, false)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if !poolCapabilities(request.Source, true)["download"] {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "source does not support download"})
		return
	}

	var cfg config.Config
	if request.Source == "cloud" || request.Source == "federation" {
		cfg, err = config.Load(s.opt.Paths.Config)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	var client *cpa.Client
	if request.Source == "federation" {
		request.Master, request.masterToken, err = configuredFederationMaster(cfg, request.Master)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	if request.Source == "cloud" {
		client = cpa.NewClient(cfg.CPAManagementBase, cfg.CPAManagementKey, max(cfg.CPAUploadTimeoutSec, 30))
	}
	var accounts map[string]acctpool.Account
	if request.Source == "accounts" {
		if s.accounts == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "accounts store unavailable"})
			return
		}
		accounts, err = s.accounts.GetMany(request.IDs)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}

	select {
	case poolDownloadSlots <- struct{}{}:
		defer func() { <-poolDownloadSlots }()
	default:
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "too many concurrent credential downloads"})
		return
	}
	archive, err := os.CreateTemp("", "touch-squirrel-credentials-*.zip")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	archivePath := archive.Name()
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archivePath)
	}()
	writer := zip.NewWriter(archive)
	usedNames := map[string]int{}
	failures := make([]poolBatchItemResult, 0)
	totalBytes := 0
	succeeded := 0
	for _, id := range request.IDs {
		name, raw, itemErr := s.readPoolBatchCredential(request, id, accounts[id], client)
		if itemErr == nil && len(raw) > maxPoolFileBytes {
			itemErr = fmt.Errorf("credential exceeds %d MiB", maxPoolFileBytes>>20)
		}
		if itemErr == nil && totalBytes+len(raw) > maxPoolBatchBytes {
			itemErr = fmt.Errorf("batch exceeds %d MiB", maxPoolBatchBytes>>20)
		}
		if itemErr != nil {
			failures = append(failures, poolBatchItemResult{ID: id, Error: itemErr.Error()})
			continue
		}
		entry, createErr := writer.Create(uniqueArchiveName(safeCredentialName(name), usedNames))
		if createErr != nil {
			_ = writer.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": createErr.Error()})
			return
		}
		if _, createErr = entry.Write(raw); createErr != nil {
			_ = writer.Close()
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": createErr.Error()})
			return
		}
		totalBytes += len(raw)
		succeeded++
	}
	if err := writer.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if len(failures) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"ok": false, "error": "one or more credentials could not be downloaded",
			"total": len(request.IDs), "succeeded": succeeded, "failed": len(failures), "results": failures,
		})
		return
	}
	if succeeded == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "no credentials could be downloaded"})
		return
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="credentials.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Batch-Succeeded", fmt.Sprintf("%d", succeeded))
	w.Header().Set("X-Batch-Failed", "0")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, archive)
}

func (s *Server) readPoolBatchCredential(
	request poolBatchRequest,
	id string,
	account acctpool.Account,
	client *cpa.Client,
) (string, []byte, error) {
	switch request.Source {
	case "accounts":
		if account.ID == "" {
			return "", nil, fmt.Errorf("account not found")
		}
		return s.readAccountCredential(account)
	case "local":
		path, err := s.localPool.PathFor(id)
		if err != nil {
			return "", nil, err
		}
		raw, err := readBoundedCredential(path)
		return id, raw, err
	case "cloud":
		raw, err := client.Download(id)
		return id, raw, err
	case "federation":
		raw, err := fetchFederationCredential(request.Master, id, request.masterToken)
		return id, raw, err
	default:
		return "", nil, fmt.Errorf("unsupported source")
	}
}

func (s *Server) readAccountCredential(account acctpool.Account) (string, []byte, error) {
	switch account.Type {
	case acctpool.TypeXAI:
		path, err := s.localPool.PathFor(account.ExternalID)
		if err != nil {
			return "", nil, err
		}
		raw, err := readBoundedCredential(path)
		return account.ExternalID, raw, err
	case acctpool.TypeTavily:
		pool := tavilypool.New(tavilypool.DefaultStatePath(s.opt.Paths.Root))
		key, err := pool.Get(account.ExternalID, false)
		if err != nil {
			return "", nil, err
		}
		raw, err := json.MarshalIndent(key, "", "  ")
		return "tavily-" + account.ExternalID + ".json", append(raw, '\n'), err
	default:
		return "", nil, fmt.Errorf("credential type %s is not downloadable", account.Type)
	}
}

func (s *Server) deleteAccountSource(account acctpool.Account) error {
	switch account.Type {
	case acctpool.TypeXAI:
		err := s.localPool.Delete(account.ExternalID)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	case acctpool.TypeTavily:
		pool := tavilypool.New(tavilypool.DefaultStatePath(s.opt.Paths.Root))
		err := pool.Delete(account.ExternalID)
		if errors.Is(err, tavilypool.ErrKeyNotFound) {
			return nil
		}
		return err
	default:
		return fmt.Errorf("credential type %s does not support source deletion", account.Type)
	}
}

func poolUploader(cfg config.Config) (*cpa.Uploader, error) {
	if strings.TrimSpace(cfg.CPAManagementBase) == "" || strings.TrimSpace(cfg.CPAManagementKey) == "" {
		return nil, fmt.Errorf("未配置 CPA_MANAGEMENT_BASE / KEY")
	}
	return cpa.NewUploader(cpa.UploadConfig{
		Enabled:      true,
		BaseURL:      cfg.CPAManagementBase,
		Key:          cfg.CPAManagementKey,
		TimeoutSec:   max(cfg.CPAUploadTimeoutSec, 30),
		Retries:      cfg.CPAUploadRetries,
		NameTemplate: cfg.CPAUploadNameTemplate,
		Verify:       cfg.CPAUploadVerify,
		Mode:         cfg.CPAUploadMode,
	}, nil), nil
}

func uploadResultError(result cpa.UploadResult) error {
	if result.Err != nil {
		return result.Err
	}
	return fmt.Errorf("upload failed with status %d", result.Status)
}

func fetchFederationCredential(master, name, token string) ([]byte, error) {
	body, status, err := federationGET(master, "/api/federation/pool/pull", token, map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("federation pull status=%d body=%s", status, truncatePoolError(body, 200))
	}
	return body, nil
}

func readBoundedCredential(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxPoolFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxPoolFileBytes {
		return nil, fmt.Errorf("credential exceeds %d MiB", maxPoolFileBytes>>20)
	}
	return raw, nil
}

func safeCredentialName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	var cleaned strings.Builder
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '.' || char == '-' || char == '_' {
			cleaned.WriteRune(char)
		} else {
			cleaned.WriteByte('_')
		}
	}
	name = strings.Trim(cleaned.String(), ". ")
	if len(name) > 180 {
		name = name[:180]
	}
	if name == "" {
		name = "credential"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		name += ".json"
	}
	return name
}

func uniqueArchiveName(name string, used map[string]int) string {
	count := used[name]
	used[name] = count + 1
	if count == 0 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s-%d%s", base, count+1, ext)
}

func truncatePoolError(raw []byte, limit int) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > limit {
		return text[:limit] + "…"
	}
	return text
}
