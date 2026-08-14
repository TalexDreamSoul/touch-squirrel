package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Status string

const (
	StatusStopped   Status = "stopped"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
	StatusFailed    Status = "failed"
	StatusError     Status = "error" // legacy snapshots
)

type Phase string

const (
	PhaseIdle      Phase = "idle"
	PhaseClearance Phase = "clearance"
	PhaseRegister  Phase = "register"
	PhaseOAuth     Phase = "oauth"
	PhaseProbe     Phase = "probe"
)

type Workers struct {
	S     int `json:"s"`
	P     int `json:"p"`
	C     int `json:"c"`
	OAuth int `json:"oauth"`
}

// Snapshot is written atomically for `grok status`.
type Snapshot struct {
	Status        Status  `json:"status"`
	RunID         string  `json:"run_id"`
	Plugin        string  `json:"plugin,omitempty"`
	Target        int     `json:"target"`
	Done          int     `json:"done"`
	SSOCount      int     `json:"sso_count"`
	OAuthCount    int     `json:"oauth_count"`
	FailCount     int     `json:"fail_count"`
	ImportedCount int     `json:"imported_count,omitempty"`
	ImportedAt    string  `json:"imported_at,omitempty"`
	Phase         Phase   `json:"phase"`
	PhaseDetail   string  `json:"phase_detail"`
	Workers       Workers `json:"workers"`
	PID           int     `json:"pid"`
	StartedAt     string  `json:"started_at"`
	UpdatedAt     string  `json:"updated_at"`
	Error         string  `json:"error"`
	LogPath       string  `json:"log_path"`
	OutputDir     string  `json:"output_dir"`
	RatePerMin    float64 `json:"rate_per_min"`
}

type Store struct {
	path string
	mu   sync.Mutex
	cur  Snapshot
}

func NewStore(path string) *Store {
	store := &Store{path: path}
	if snapshot, err := loadSnapshot(path); err == nil {
		store.cur = snapshot
	}
	return store
}

func (s *Store) Path() string { return s.path }

func (s *Store) Get() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *Store) Set(fn func(*Snapshot)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cur)
	s.cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return s.writeLocked()
}

func (s *Store) Load() (Snapshot, error) {
	snap, err := loadSnapshot(s.path)
	if err != nil {
		return Snapshot{Status: StatusStopped}, err
	}
	s.mu.Lock()
	s.cur = snap
	s.mu.Unlock()
	return snap, nil
}

func (s *Store) writeLocked() error {
	if err := writeSnapshot(s.path, s.cur); err != nil {
		return err
	}
	if s.cur.RunID == "" || s.cur.OutputDir == "" {
		return nil
	}
	outputDir := filepath.Clean(s.cur.OutputDir)
	if filepath.Base(outputDir) != s.cur.RunID {
		return nil
	}
	return writeSnapshot(filepath.Join(outputDir, "run.json"), s.cur)
}

func LoadRun(runDir string) (Snapshot, error) {
	return loadSnapshot(filepath.Join(runDir, "run.json"))
}

func UpdateRun(runDir string, fn func(*Snapshot)) error {
	snapshot, err := LoadRun(runDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if snapshot.RunID == "" {
		snapshot.RunID = filepath.Base(filepath.Clean(runDir))
		snapshot.OutputDir = filepath.Clean(runDir)
	}
	fn(&snapshot)
	snapshot.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return writeSnapshot(filepath.Join(runDir, "run.json"), snapshot)
}

func loadSnapshot(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func writeSnapshot(path string, snapshot Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
