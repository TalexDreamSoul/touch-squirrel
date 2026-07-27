package tavilypool

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// KeyStatus mirrors hikari-style lifecycle (subset).
type KeyStatus string

const (
	StatusActive    KeyStatus = "active"
	StatusExhausted KeyStatus = "exhausted"
	StatusDisabled  KeyStatus = "disabled"
)

// Key is one upstream Tavily API key (secret kept on disk, not logged).
type Key struct {
	ID             string    `json:"id"`
	APIKey         string    `json:"api_key"`
	Status         KeyStatus `json:"status"`
	LastUsedUnix   int64     `json:"last_used_unix"`
	ExhaustedUntil int64     `json:"exhausted_until,omitempty"` // unix; 0 = none
	CreatedAt      string    `json:"created_at"`
	Note           string    `json:"note,omitempty"`
	Success        int64     `json:"success"`
	Failure        int64     `json:"failure"`
}

// Snapshot is the on-disk key table.
type Snapshot struct {
	Keys []Key `json:"keys"`
}

// Pool is a file-backed multi-key store with LRU selection.
type Pool struct {
	Path string
	mu   sync.Mutex
}

func New(path string) *Pool {
	return &Pool{Path: path}
}

func (p *Pool) load() (Snapshot, error) {
	var snap Snapshot
	b, err := os.ReadFile(p.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{Keys: []Key{}}, nil
		}
		return snap, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return Snapshot{Keys: []Key{}}, nil
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return snap, err
	}
	if snap.Keys == nil {
		snap.Keys = []Key{}
	}
	// recover monthly exhausted keys
	now := time.Now().Unix()
	changed := false
	for i := range snap.Keys {
		k := &snap.Keys[i]
		if k.Status == StatusExhausted && k.ExhaustedUntil > 0 && now >= k.ExhaustedUntil {
			k.Status = StatusActive
			k.ExhaustedUntil = 0
			changed = true
		}
	}
	if changed {
		_ = p.saveLocked(snap)
	}
	return snap, nil
}

func (p *Pool) saveLocked(snap Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := p.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.Path)
}

// List returns a copy of keys with api_key redacted when redact=true.
func (p *Pool) List(redact bool) ([]Key, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, err := p.load()
	if err != nil {
		return nil, err
	}
	out := make([]Key, len(snap.Keys))
	copy(out, snap.Keys)
	if redact {
		for i := range out {
			out[i].APIKey = maskKey(out[i].APIKey)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastUsedUnix < out[j].LastUsedUnix
	})
	return out, nil
}

// Add inserts or revives a key. Returns short id.
func (p *Pool) Add(apiKey, note string) (Key, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return Key{}, fmt.Errorf("api key empty")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, err := p.load()
	if err != nil {
		return Key{}, err
	}
	for i := range snap.Keys {
		if snap.Keys[i].APIKey == apiKey {
			snap.Keys[i].Status = StatusActive
			snap.Keys[i].ExhaustedUntil = 0
			if note != "" {
				snap.Keys[i].Note = note
			}
			if err := p.saveLocked(snap); err != nil {
				return Key{}, err
			}
			k := snap.Keys[i]
			k.APIKey = maskKey(k.APIKey)
			return k, nil
		}
	}
	k := Key{
		ID:        shortID(),
		APIKey:    apiKey,
		Status:    StatusActive,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Note:      note,
	}
	snap.Keys = append(snap.Keys, k)
	if err := p.saveLocked(snap); err != nil {
		return Key{}, err
	}
	pub := k
	pub.APIKey = maskKey(pub.APIKey)
	return pub, nil
}

// SetStatus updates status by id.
func (p *Pool) SetStatus(id string, st KeyStatus) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, err := p.load()
	if err != nil {
		return err
	}
	for i := range snap.Keys {
		if snap.Keys[i].ID == id {
			snap.Keys[i].Status = st
			if st != StatusExhausted {
				snap.Keys[i].ExhaustedUntil = 0
			}
			return p.saveLocked(snap)
		}
	}
	return fmt.Errorf("key not found: %s", id)
}

// Acquire picks an active key by LRU and marks last_used.
func (p *Pool) Acquire() (Key, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, err := p.load()
	if err != nil {
		return Key{}, err
	}
	var best *Key
	for i := range snap.Keys {
		k := &snap.Keys[i]
		if k.Status != StatusActive {
			continue
		}
		if best == nil || k.LastUsedUnix < best.LastUsedUnix {
			best = k
		}
	}
	if best == nil {
		return Key{}, fmt.Errorf("no active tavily keys in pool")
	}
	best.LastUsedUnix = time.Now().Unix()
	out := *best
	if err := p.saveLocked(snap); err != nil {
		return Key{}, err
	}
	return out, nil
}

// ReportSuccess increments success counter.
func (p *Pool) ReportSuccess(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, err := p.load()
	if err != nil {
		return
	}
	for i := range snap.Keys {
		if snap.Keys[i].ID == id {
			snap.Keys[i].Success++
			_ = p.saveLocked(snap)
			return
		}
	}
}

// ReportFailure increments failure; on exhausted (HTTP 432 style) marks month lockout.
func (p *Pool) ReportFailure(id string, exhausted bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap, err := p.load()
	if err != nil {
		return
	}
	for i := range snap.Keys {
		if snap.Keys[i].ID == id {
			snap.Keys[i].Failure++
			if exhausted {
				snap.Keys[i].Status = StatusExhausted
				snap.Keys[i].ExhaustedUntil = nextUTCMonthUnix(time.Now())
			}
			_ = p.saveLocked(snap)
			return
		}
	}
}

func nextUTCMonthUnix(now time.Time) int64 {
	y, m, _ := now.UTC().Date()
	m++
	if m > 12 {
		m = 1
		y++
	}
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).Unix()
}

func shortID() string {
	var b [3]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:]) // 6 hex chars ~ hikari short id spirit
}

func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "…" + k[len(k)-4:]
}

// DefaultStatePath returns plugins-data path under home root.
func DefaultStatePath(homeRoot string) string {
	return filepath.Join(homeRoot, "plugins-data", "tavily-pool", "keys.json")
}
