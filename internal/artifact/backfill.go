package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/grok-free-register/grok-reg/internal/localpool"
	"github.com/grok-free-register/grok-reg/internal/tavilypool"
)

const (
	maxBackfillPayloadBytes = int64(64 << 20)
	maxBackfillTotalBytes   = int64(256 << 20)
	maxBackfillItems        = 10_000
	maxIdentityFiles        = 100_000
	maxIdentityHeaderBytes  = int64(1 << 20)
)

// BackfillReport summarizes legacy credentials mirrored into the artifact archive.
type BackfillReport struct {
	Imported int `json:"imported"`
	Updated  int `json:"updated"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

type backfillIdentity struct {
	ID        string
	Hash      string
	CreatedAt string
}

type artifactIdentityHeader struct {
	ID        string
	Plugin    string
	CreatedAt string
	Labels    map[string]string
}

// BackfillLegacyCredentials mirrors legacy local-pool and Tavily credentials.
// A stable source identity keeps one current artifact per credential; changed
// content atomically updates that artifact while unchanged content is skipped.
func BackfillLegacyCredentials(store *Store, localPoolDir, tavilyPath string) (BackfillReport, error) {
	var report BackfillReport
	if store == nil {
		return report, nil
	}
	files, keys, err := scanBackfillIdentities(store.Root)
	if err != nil {
		return report, err
	}
	processed := 0
	var totalBytes int64

	if strings.TrimSpace(localPoolDir) != "" {
		service := localpool.New(localPoolDir)
		for _, entry := range service.List() {
			processed++
			if processed > maxBackfillItems {
				return report, fmt.Errorf("legacy credential count exceeds %d", maxBackfillItems)
			}
			name := filepath.Base(strings.TrimSpace(entry.Name))
			if name == "" {
				report.Failed++
				continue
			}
			path, err := service.PathFor(name)
			if err != nil {
				report.Failed++
				continue
			}
			raw, err := readBoundedJSON(path, maxBackfillPayloadBytes)
			if err != nil {
				report.Failed++
				continue
			}
			totalBytes += int64(len(raw))
			if totalBytes > maxBackfillTotalBytes {
				return report, fmt.Errorf("legacy credential bytes exceed %d", maxBackfillTotalBytes)
			}
			hash := strings.TrimSpace(entry.Hash)
			if hash == "" {
				hash = contentHash(raw)
			}
			identityKey := "xai-accounts\x00file:" + name
			existing, exists := files[identityKey]
			if exists && existing.Hash == hash {
				report.Skipped++
				continue
			}
			labels := map[string]string{
				"source": "local-pool", "source_file": name, "source_hash": hash,
			}
			if entry.Email != "" {
				labels["email"] = entry.Email
			}
			createdAt := existing.CreatedAt
			if createdAt == "" && !entry.AddedAt.IsZero() {
				createdAt = entry.AddedAt.UTC().Format("2006-01-02T15:04:05Z")
			}
			id := existing.ID
			if id == "" {
				id = legacyArtifactID("local-pool:" + name)
			}
			if _, err := store.Put(Artifact{
				ID: id, Plugin: "xai-accounts", Kind: "account.xai", Status: StatusFresh,
				Labels: labels, Payload: raw, RunID: entry.SourceRun, CreatedAt: createdAt,
			}); err != nil {
				report.Failed++
				continue
			}
			files[identityKey] = backfillIdentity{ID: id, Hash: hash, CreatedAt: createdAt}
			if exists {
				report.Updated++
			} else {
				report.Imported++
			}
		}
	}

	if strings.TrimSpace(tavilyPath) != "" {
		raw, err := readBoundedJSON(tavilyPath, maxBackfillPayloadBytes)
		if err == nil {
			totalBytes += int64(len(raw))
			if totalBytes > maxBackfillTotalBytes {
				return report, fmt.Errorf("legacy credential bytes exceed %d", maxBackfillTotalBytes)
			}
			var snapshot tavilypool.Snapshot
			if json.Unmarshal(raw, &snapshot) != nil {
				report.Failed++
			} else {
				for _, key := range snapshot.Keys {
					processed++
					if processed > maxBackfillItems {
						return report, fmt.Errorf("legacy credential count exceeds %d", maxBackfillItems)
					}
					keyID := strings.TrimSpace(key.ID)
					if keyID == "" {
						report.Failed++
						continue
					}
					payload, err := json.Marshal(map[string]any{
						"id": key.ID, "api_key": key.APIKey, "status": key.Status,
						"note": key.Note, "success": key.Success, "failure": key.Failure,
					})
					if err != nil {
						report.Failed++
						continue
					}
					hash := contentHash(payload)
					identityKey := "tavily-pool\x00key:" + keyID
					existing, exists := keys[identityKey]
					if exists && existing.Hash == hash {
						report.Skipped++
						continue
					}
					id := existing.ID
					if id == "" {
						id = legacyArtifactID("tavily:" + keyID)
					}
					createdAt := existing.CreatedAt
					if createdAt == "" {
						createdAt = key.CreatedAt
					}
					if _, err := store.Put(Artifact{
						ID: id, Plugin: "tavily-pool", Kind: "key.tavily", Status: StatusFresh,
						Labels: map[string]string{
							"source": "tavily-pool", "source_file": filepath.Base(tavilyPath),
							"source_hash": hash, "key_id": keyID,
						},
						Payload: payload, CreatedAt: createdAt,
					}); err != nil {
						report.Failed++
						continue
					}
					keys[identityKey] = backfillIdentity{ID: id, Hash: hash, CreatedAt: createdAt}
					if exists {
						report.Updated++
					} else {
						report.Imported++
					}
				}
			}
		} else if !os.IsNotExist(err) {
			report.Failed++
		}
	}

	return report, nil
}

func scanBackfillIdentities(root string) (map[string]backfillIdentity, map[string]backfillIdentity, error) {
	files := map[string]backfillIdentity{}
	keys := map[string]backfillIdentity{}
	plugins, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return files, keys, nil
		}
		return nil, nil, err
	}
	count := 0
	for _, pluginDir := range plugins {
		if !pluginDir.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, pluginDir.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			count++
			if count > maxIdentityFiles {
				return nil, nil, fmt.Errorf("artifact identity count exceeds %d", maxIdentityFiles)
			}
			header, err := readArtifactIdentityHeader(filepath.Join(root, pluginDir.Name(), entry.Name()))
			if err != nil || header.ID == "" || header.Plugin == "" {
				continue
			}
			identity := backfillIdentity{ID: header.ID, Hash: header.Labels["source_hash"], CreatedAt: header.CreatedAt}
			if source := filepath.Base(strings.TrimSpace(header.Labels["source_file"])); source != "" && source != "." {
				files[header.Plugin+"\x00file:"+source] = identity
			}
			if keyID := strings.TrimSpace(header.Labels["key_id"]); keyID != "" {
				keys[header.Plugin+"\x00key:"+keyID] = identity
			}
		}
	}
	return files, keys, nil
}

func readArtifactIdentityHeader(path string) (artifactIdentityHeader, error) {
	var header artifactIdentityHeader
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return header, fmt.Errorf("artifact metadata is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return header, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxIdentityHeaderBytes))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return header, fmt.Errorf("invalid artifact metadata")
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return header, err
		}
		key, _ := keyToken.(string)
		if key == "payload" {
			return header, nil
		}
		switch key {
		case "id":
			err = decoder.Decode(&header.ID)
		case "plugin":
			err = decoder.Decode(&header.Plugin)
		case "createdAt":
			err = decoder.Decode(&header.CreatedAt)
		case "labels":
			err = decoder.Decode(&header.Labels)
		default:
			var discard any
			err = decoder.Decode(&discard)
		}
		if err != nil {
			return header, err
		}
	}
	return header, nil
}

func readBoundedJSON(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credential is not a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("credential exceeds %d bytes", limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("credential exceeds %d bytes", limit)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("credential is not valid json")
	}
	return raw, nil
}

func contentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func legacyArtifactID(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "art_legacy_" + hex.EncodeToString(sum[:8])
}
