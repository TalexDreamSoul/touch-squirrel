package plugin

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	OfficialRepositoryID         = "official"
	DefaultOfficialRepositoryURL = "https://github.com/TalexDreamSoul/touch-squirrel"
	officialRepositoryEnv        = "SQUIRREL_OFFICIAL_PLUGIN_REPO"
	maxArchiveBytes              = int64(100 << 20)
	maxExtractedBytes            = uint64(500 << 20)
	maxArchiveFiles              = 20_000
	maxCompressionRatio          = uint64(1_000)
)

var githubSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var (
	ErrOfficialRepository  = errors.New("official repository cannot be changed or deleted")
	ErrRepositoryNotFound  = errors.New("plugin repository not found")
	ErrMarketPluginMissing = errors.New("market plugin not found")
)

// Repository is a GitHub source exposed by the plugin market.
type Repository struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Official    bool       `json:"official"`
	SyncedAt    *time.Time `json:"synced_at,omitempty"`
	PluginCount int        `json:"plugin_count"`
	LastError   string     `json:"last_error,omitempty"`
	cacheValid  bool
	snapshot    string
}

type repositoryFile struct {
	Repositories []Repository `json:"repositories"`
}

type repositoryState struct {
	RepositoryURL string    `json:"repository_url"`
	Snapshot      string    `json:"snapshot"`
	SyncedAt      time.Time `json:"synced_at,omitempty"`
	PluginCount   int       `json:"plugin_count"`
	LastError     string    `json:"last_error,omitempty"`
}

// MarketPlugin is one validated plugin discovered in a repository snapshot.
type MarketPlugin struct {
	Manifest         Manifest `json:"manifest"`
	RepositoryID     string   `json:"repository_id"`
	RepositoryName   string   `json:"repository_name"`
	Official         bool     `json:"official"`
	Installed        bool     `json:"installed"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	Root             string   `json:"-"`
}

// SyncResult reports one repository refresh without discarding other results.
type SyncResult struct {
	Repository Repository `json:"repository"`
	OK         bool       `json:"ok"`
	Error      string     `json:"error,omitempty"`
}

type installedMarketPlugin struct {
	Version       string
	RepositoryID  string
	RepositoryURL string
}

// Market persists repository settings, refreshes GitHub snapshots, and installs plugins.
type Market struct {
	cacheRoot string
	manager   *Manager
	client    *http.Client
	mu        sync.Mutex
}

func NewMarket(cacheRoot string, manager *Manager) *Market {
	client := &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many GitHub redirects")
			}
			if req.URL.Scheme != "https" || req.URL.Port() != "" || !strings.EqualFold(req.URL.Hostname(), "codeload.github.com") {
				return fmt.Errorf("unexpected GitHub redirect target: %s", req.URL.Redacted())
			}
			return nil
		},
	}
	return &Market{cacheRoot: cacheRoot, manager: manager, client: client}
}

func (m *Market) Repositories() ([]Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.repositoriesLocked()
}

func (m *Market) AddRepository(name, rawURL string) (Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalized, owner, repo, err := normalizeGitHubRepository(rawURL)
	if err != nil {
		return Repository{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = owner + "/" + repo
	}
	if len(name) > 80 {
		return Repository{}, fmt.Errorf("repository name is too long")
	}

	repositories, err := m.repositoriesLocked()
	if err != nil {
		return Repository{}, err
	}
	for _, existing := range repositories {
		if strings.EqualFold(existing.URL, normalized) || existing.ID == repositoryID(normalized) {
			return Repository{}, fmt.Errorf("repository already exists: %s", normalized)
		}
	}

	repository := Repository{
		ID:   repositoryID(normalized),
		Name: name,
		URL:  normalized,
	}
	custom := customRepositories(repositories)
	custom = append(custom, repository)
	if err := m.saveRepositoriesLocked(custom); err != nil {
		return Repository{}, err
	}
	return repository, nil
}

func (m *Market) RemoveRepository(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if id == OfficialRepositoryID {
		return ErrOfficialRepository
	}
	repositories, err := m.repositoriesLocked()
	if err != nil {
		return err
	}
	custom := customRepositories(repositories)
	found := false
	kept := make([]Repository, 0, len(custom))
	for _, repository := range custom {
		if repository.ID == id {
			found = true
			continue
		}
		kept = append(kept, repository)
	}
	if !found {
		return ErrRepositoryNotFound
	}
	if err := m.saveRepositoriesLocked(kept); err != nil {
		return err
	}
	return os.RemoveAll(m.repositoryDir(id))
}

func (m *Market) Sync(ctx context.Context, id string) (Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.syncLocked(ctx, id)
}

func (m *Market) SyncAll(ctx context.Context) ([]SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	repositories, err := m.repositoriesLocked()
	if err != nil {
		return nil, err
	}
	results := make([]SyncResult, 0, len(repositories))
	for _, repository := range repositories {
		updated, syncErr := m.syncRepositoryLocked(ctx, repository)
		result := SyncResult{Repository: updated, OK: syncErr == nil}
		if syncErr != nil {
			result.Error = syncErr.Error()
		}
		results = append(results, result)
	}
	return results, nil
}

func (m *Market) List() ([]MarketPlugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

func (m *Market) Install(repositoryID, pluginID string) (Installed, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugins, err := m.listLocked()
	if err != nil {
		return Installed{}, err
	}
	for _, candidate := range plugins {
		if candidate.RepositoryID != repositoryID || candidate.Manifest.ID != pluginID {
			continue
		}
		return m.manager.InstallRepositoryPath(candidate.Root, InstallSource{
			RepositoryID:   candidate.RepositoryID,
			RepositoryName: candidate.RepositoryName,
			RepositoryURL:  m.repositoryURLLocked(candidate.RepositoryID),
		})
	}
	return Installed{}, ErrMarketPluginMissing
}

func (m *Market) syncLocked(ctx context.Context, id string) (Repository, error) {
	repositories, err := m.repositoriesLocked()
	if err != nil {
		return Repository{}, err
	}
	for _, repository := range repositories {
		if repository.ID == id {
			return m.syncRepositoryLocked(ctx, repository)
		}
	}
	return Repository{}, ErrRepositoryNotFound
}

func (m *Market) syncRepositoryLocked(ctx context.Context, repository Repository) (Repository, error) {
	archivePath, err := m.downloadRepositoryLocked(ctx, repository)
	if err != nil {
		m.recordSyncErrorLocked(repository.ID, err)
		repository.LastError = err.Error()
		return repository, err
	}
	defer os.Remove(archivePath)

	repositoryDir := m.repositoryDir(repository.ID)
	if err := os.MkdirAll(repositoryDir, 0o700); err != nil {
		return repository, err
	}
	staging, err := os.MkdirTemp(repositoryDir, ".snapshot-")
	if err != nil {
		return repository, err
	}
	defer func() {
		if staging != "" {
			_ = os.RemoveAll(staging)
		}
	}()

	if err := extractRepositoryArchive(archivePath, staging); err != nil {
		m.recordSyncErrorLocked(repository.ID, err)
		repository.LastError = err.Error()
		return repository, err
	}
	plugins, err := discoverMarketPlugins(staging, repository, nil)
	if err != nil {
		m.recordSyncErrorLocked(repository.ID, err)
		repository.LastError = err.Error()
		return repository, err
	}
	if len(plugins) == 0 {
		err = fmt.Errorf("repository contains no valid plugin.json")
		m.recordSyncErrorLocked(repository.ID, err)
		repository.LastError = err.Error()
		return repository, err
	}

	previousState, _ := m.loadRepositoryStateLocked(repository.ID)
	state := repositoryState{
		RepositoryURL: repository.URL,
		Snapshot:      filepath.Base(staging),
		SyncedAt:      time.Now().UTC(),
		PluginCount:   len(plugins),
	}
	if err := writeJSONAtomic(m.repositoryStatePath(repository.ID), state, 0o600); err != nil {
		return repository, err
	}
	staging = ""
	if previousState.Snapshot != "" && previousState.Snapshot != state.Snapshot && filepath.Base(previousState.Snapshot) == previousState.Snapshot {
		_ = os.RemoveAll(filepath.Join(repositoryDir, previousState.Snapshot))
	}

	repository.SyncedAt = &state.SyncedAt
	repository.PluginCount = state.PluginCount
	repository.LastError = ""
	repository.cacheValid = true
	repository.snapshot = state.Snapshot
	return repository, nil
}

func (m *Market) downloadRepositoryLocked(ctx context.Context, repository Repository) (string, error) {
	_, owner, repo, err := normalizeGitHubRepository(repository.URL)
	if err != nil {
		return "", err
	}
	archiveURL := "https://codeload.github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/zip/HEAD"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/zip")
	req.Header.Set("User-Agent", "touch-squirrel-plugin-market")
	resp, err := m.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", repository.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: GitHub returned %s", repository.Name, resp.Status)
	}
	if resp.ContentLength > maxArchiveBytes {
		return "", fmt.Errorf("download %s: archive exceeds %d MiB", repository.Name, maxArchiveBytes>>20)
	}
	if err := os.MkdirAll(m.cacheRoot, 0o700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(m.cacheRoot, ".github-*.zip")
	if err != nil {
		return "", err
	}
	archivePath := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(archivePath)
		}
	}()
	written, err := io.Copy(file, io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return "", err
	}
	if written > maxArchiveBytes {
		return "", fmt.Errorf("download %s: archive exceeds %d MiB", repository.Name, maxArchiveBytes>>20)
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return archivePath, nil
}

func (m *Market) listLocked() ([]MarketPlugin, error) {
	repositories, err := m.repositoriesLocked()
	if err != nil {
		return nil, err
	}
	installed := map[string]installedMarketPlugin{}
	if m.manager != nil {
		items, listErr := m.manager.List()
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range items {
			installed[item.Manifest.ID] = installedMarketPlugin{
				Version:       item.Manifest.Version,
				RepositoryID:  item.RepositoryID,
				RepositoryURL: item.RepositoryURL,
			}
		}
	}

	var plugins []MarketPlugin
	for _, repository := range repositories {
		if !repository.cacheValid || repository.snapshot == "" {
			continue
		}
		root := filepath.Join(m.repositoryDir(repository.ID), repository.snapshot)
		items, discoverErr := discoverMarketPlugins(root, repository, installed)
		if discoverErr != nil {
			if os.IsNotExist(discoverErr) {
				continue
			}
			return nil, discoverErr
		}
		plugins = append(plugins, items...)
	}
	sort.Slice(plugins, func(i, j int) bool {
		if plugins[i].Official != plugins[j].Official {
			return plugins[i].Official
		}
		if plugins[i].Manifest.ID != plugins[j].Manifest.ID {
			return plugins[i].Manifest.ID < plugins[j].Manifest.ID
		}
		return plugins[i].RepositoryName < plugins[j].RepositoryName
	})
	return plugins, nil
}

func (m *Market) repositoriesLocked() ([]Repository, error) {
	normalized, err := currentOfficialRepositoryURL()
	if err != nil {
		return nil, err
	}
	repositories := []Repository{{
		ID:       OfficialRepositoryID,
		Name:     "Touch Squirrel 官方插件",
		URL:      normalized,
		Official: true,
	}}

	var file repositoryFile
	data, err := os.ReadFile(m.repositoriesPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("plugin repositories: %w", err)
		}
	}
	seen := map[string]bool{strings.ToLower(normalized): true}
	for _, repository := range file.Repositories {
		customURL, owner, repo, normalizeErr := normalizeGitHubRepository(repository.URL)
		if normalizeErr != nil {
			return nil, fmt.Errorf("plugin repository %q: %w", repository.Name, normalizeErr)
		}
		customKey := strings.ToLower(customURL)
		if seen[customKey] {
			continue
		}
		seen[customKey] = true
		repository.URL = customURL
		repository.ID = repositoryID(customURL)
		repository.Official = false
		if strings.TrimSpace(repository.Name) == "" {
			repository.Name = owner + "/" + repo
		}
		repositories = append(repositories, repository)
	}
	for i := range repositories {
		state, stateErr := m.loadRepositoryStateLocked(repositories[i].ID)
		if stateErr != nil {
			return nil, stateErr
		}
		repositories[i].LastError = state.LastError
		repositories[i].snapshot = state.Snapshot
		repositories[i].cacheValid = state.RepositoryURL == repositories[i].URL && state.Snapshot != "" && filepath.Base(state.Snapshot) == state.Snapshot
		if repositories[i].cacheValid {
			if !state.SyncedAt.IsZero() {
				syncedAt := state.SyncedAt
				repositories[i].SyncedAt = &syncedAt
			}
			repositories[i].PluginCount = state.PluginCount
		} else {
			repositories[i].LastError = ""
		}
	}
	return repositories, nil
}

func (m *Market) saveRepositoriesLocked(repositories []Repository) error {
	for i := range repositories {
		repositories[i].Official = false
		repositories[i].SyncedAt = nil
		repositories[i].PluginCount = 0
		repositories[i].LastError = ""
	}
	return writeJSONAtomic(m.repositoriesPath(), repositoryFile{Repositories: repositories}, 0o600)
}

func (m *Market) loadRepositoryStateLocked(id string) (repositoryState, error) {
	var state repositoryState
	data, err := os.ReadFile(m.repositoryStatePath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("plugin repository state %s: %w", id, err)
	}
	return state, nil
}

func (m *Market) recordSyncErrorLocked(id string, syncErr error) {
	state, _ := m.loadRepositoryStateLocked(id)
	state.LastError = syncErr.Error()
	_ = writeJSONAtomic(m.repositoryStatePath(id), state, 0o600)
}

func (m *Market) repositoryURLLocked(id string) string {
	repositories, _ := m.repositoriesLocked()
	for _, repository := range repositories {
		if repository.ID == id {
			return repository.URL
		}
	}
	return ""
}

func (m *Market) repositoriesPath() string {
	return filepath.Join(m.cacheRoot, "repositories.json")
}

func (m *Market) repositoryDir(id string) string {
	return filepath.Join(m.cacheRoot, "repositories", id)
}

func (m *Market) repositoryStatePath(id string) string {
	return filepath.Join(m.repositoryDir(id), "state.json")
}

func customRepositories(repositories []Repository) []Repository {
	custom := make([]Repository, 0, len(repositories))
	for _, repository := range repositories {
		if !repository.Official {
			custom = append(custom, repository)
		}
	}
	return custom
}

// CurrentOfficialRepositoryURL returns the normalized official plugin source.
func CurrentOfficialRepositoryURL() (string, error) {
	return currentOfficialRepositoryURL()
}

func currentOfficialRepositoryURL() (string, error) {
	officialURL := strings.TrimSpace(os.Getenv(officialRepositoryEnv))
	if officialURL == "" {
		officialURL = DefaultOfficialRepositoryURL
	}
	normalized, _, _, err := normalizeGitHubRepository(officialURL)
	if err != nil {
		return "", fmt.Errorf("invalid %s: %w", officialRepositoryEnv, err)
	}
	return normalized, nil
}

func normalizeGitHubRepository(raw string) (normalized, owner, repo string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", fmt.Errorf("invalid GitHub repository URL")
	}
	if u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") || u.User != nil {
		return "", "", "", fmt.Errorf("repository must use https://github.com/{owner}/{repo}")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.Port() != "" {
		return "", "", "", fmt.Errorf("repository URL cannot contain a port, query, or fragment")
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.EscapedPath(), ".git"), "/"), "/")
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("repository must use https://github.com/{owner}/{repo}")
	}
	owner, err = url.PathUnescape(parts[0])
	if err != nil {
		return "", "", "", fmt.Errorf("invalid GitHub owner")
	}
	repo, err = url.PathUnescape(parts[1])
	if err != nil {
		return "", "", "", fmt.Errorf("invalid GitHub repository")
	}
	if !githubSegmentPattern.MatchString(owner) || !githubSegmentPattern.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", "", "", fmt.Errorf("invalid GitHub owner or repository name")
	}
	return "https://github.com/" + owner + "/" + repo, owner, repo, nil
}

func repositoryID(repositoryURL string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(repositoryURL)))
	return "repo-" + hex.EncodeToString(digest[:6])
}

func discoverMarketPlugins(root string, repository Repository, installed map[string]installedMarketPlugin) ([]MarketPlugin, error) {
	seen := map[string]bool{}
	var plugins []MarketPlugin
	err := filepath.WalkDir(root, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			rel, err := filepath.Rel(root, current)
			if err != nil {
				return err
			}
			if rel != "." && len(strings.Split(filepath.ToSlash(rel), "/")) > 10 {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "plugin.json" {
			return nil
		}
		manifest, err := LoadManifest(current)
		if err != nil {
			return fmt.Errorf("%s: %w", current, err)
		}
		if seen[manifest.ID] {
			return fmt.Errorf("repository %s contains duplicate plugin id %q", repository.Name, manifest.ID)
		}
		seen[manifest.ID] = true
		installedPlugin := installed[manifest.ID]
		installedVersion := ""
		isInstalled := installedPlugin.Version == manifest.Version &&
			installedPlugin.RepositoryID == repository.ID &&
			installedPlugin.RepositoryURL == repository.URL
		if installedPlugin.RepositoryID == repository.ID && installedPlugin.RepositoryURL == repository.URL {
			installedVersion = installedPlugin.Version
		}
		plugins = append(plugins, MarketPlugin{
			Manifest:         manifest,
			RepositoryID:     repository.ID,
			RepositoryName:   repository.Name,
			Official:         repository.Official,
			Installed:        isInstalled,
			InstalledVersion: installedVersion,
			Root:             filepath.Dir(current),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].Manifest.ID < plugins[j].Manifest.ID })
	return plugins, nil
}

func extractRepositoryArchive(archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("invalid GitHub archive: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveFiles {
		return fmt.Errorf("GitHub archive contains too many files")
	}
	var extracted uint64
	for _, file := range reader.File {
		if strings.Contains(file.Name, `\`) {
			return fmt.Errorf("GitHub archive contains an invalid path")
		}
		clean := path.Clean(file.Name)
		if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("GitHub archive contains an unsafe path: %s", file.Name)
		}
		mode := file.Mode()
		if mode&os.ModeSymlink != 0 || mode&os.ModeType != 0 && !mode.IsDir() {
			return fmt.Errorf("GitHub archive contains an unsupported file: %s", file.Name)
		}
		if file.UncompressedSize64 > maxExtractedBytes || file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > maxCompressionRatio {
			return fmt.Errorf("GitHub archive contains a suspiciously large file: %s", file.Name)
		}
		extracted += file.UncompressedSize64
		if extracted > maxExtractedBytes {
			return fmt.Errorf("GitHub archive expands beyond %d MiB", maxExtractedBytes>>20)
		}
		target := filepath.Join(destination, filepath.FromSlash(clean))
		rel, err := filepath.Rel(destination, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("GitHub archive contains an unsafe path: %s", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		fileMode := os.FileMode(0o600)
		if mode.Perm()&0o111 != 0 {
			fileMode = 0o700
		}
		destinationFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if err != nil {
			source.Close()
			return err
		}
		written, copyErr := io.Copy(destinationFile, io.LimitReader(source, int64(file.UncompressedSize64)+1))
		closeErr := destinationFile.Close()
		sourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if sourceErr != nil {
			return sourceErr
		}
		if written != int64(file.UncompressedSize64) {
			return fmt.Errorf("GitHub archive size mismatch: %s", file.Name)
		}
	}
	return nil
}

func writeJSONAtomic(destination string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".json-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	ok = true
	return nil
}
