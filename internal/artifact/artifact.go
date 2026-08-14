package artifact

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status is the lifecycle of a stored artifact.
type Status string

const (
	StatusFresh   Status = "fresh"
	StatusHealthy Status = "healthy"
	StatusLimited Status = "limited"
	StatusDead    Status = "dead"
	StatusDiscard Status = "discarded"
)

var ErrNotFound = errors.New("artifact not found")

// Artifact is the host-level unit of stored plugin output.
type Artifact struct {
	ID         string            `json:"id"`
	Plugin     string            `json:"plugin"`
	Kind       string            `json:"kind"`
	Status     Status            `json:"status"`
	Labels     map[string]string `json:"labels,omitempty"`
	SecretRefs []string          `json:"secretRefs,omitempty"`
	Payload    json.RawMessage   `json:"payload,omitempty"`
	RunID      string            `json:"runId,omitempty"`
	CreatedAt  string            `json:"createdAt"`
	UpdatedAt  string            `json:"updatedAt"`
}

// Store persists artifacts as one JSON file each under root/<plugin>/.
type Store struct {
	Root string
	mu   sync.Mutex
}

func NewStore(root string) *Store {
	return &Store{Root: root}
}

func (s *Store) Ensure() error {
	if s == nil || s.Root == "" {
		return fmt.Errorf("artifact store root empty")
	}
	return os.MkdirAll(s.Root, 0o700)
}

// Put writes a new artifact (generates id if empty).
func (s *Store) Put(a Artifact) (Artifact, error) {
	if s == nil {
		return Artifact{}, fmt.Errorf("nil artifact store")
	}
	if err := s.Ensure(); err != nil {
		return Artifact{}, err
	}
	if strings.TrimSpace(a.Plugin) == "" {
		return Artifact{}, fmt.Errorf("plugin required")
	}
	if strings.TrimSpace(a.Kind) == "" {
		return Artifact{}, fmt.Errorf("kind required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if a.ID == "" {
		a.ID = newID()
	}
	if a.Status == "" {
		a.Status = StatusFresh
	}
	if a.CreatedAt == "" {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	if a.Labels == nil {
		a.Labels = map[string]string{}
	}

	dir := filepath.Join(s.Root, sanitize(a.Plugin))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Artifact{}, err
	}
	path := filepath.Join(dir, sanitize(a.ID)+".json")
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return Artifact{}, err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return Artifact{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// List returns artifacts, optionally filtered by plugin and/or kind.
func (s *Store) List(plugin, kind string, limit int) ([]Artifact, error) {
	if s == nil {
		return nil, fmt.Errorf("nil artifact store")
	}
	if err := s.Ensure(); err != nil {
		return nil, err
	}
	var roots []string
	if plugin != "" {
		roots = []string{filepath.Join(s.Root, sanitize(plugin))}
	} else {
		entries, err := os.ReadDir(s.Root)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				roots = append(roots, filepath.Join(s.Root, e.Name()))
			}
		}
	}
	var out []Artifact
	for _, dir := range roots {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var a Artifact
			if err := json.Unmarshal(b, &a); err != nil {
				continue
			}
			if kind != "" && a.Kind != kind {
				continue
			}
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Get returns one artifact by its host-generated ID.
func (s *Store) Get(id string) (Artifact, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Artifact{}, ErrNotFound
	}
	list, err := s.List("", "", 0)
	if err != nil {
		return Artifact{}, err
	}
	for _, a := range list {
		if a.ID == id {
			return a, nil
		}
	}
	return Artifact{}, ErrNotFound
}

// PutJSON is a helper for plugins: marshal payload and store.
func (s *Store) PutJSON(plugin, kind string, status Status, labels map[string]string, payload any, runID string) (Artifact, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Artifact{}, err
	}
	return s.Put(Artifact{
		Plugin:  plugin,
		Kind:    kind,
		Status:  status,
		Labels:  labels,
		Payload: raw,
		RunID:   runID,
	})
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "art_" + hex.EncodeToString(b[:])
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "..", "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
	if s == "" {
		return "_"
	}
	return s
}
