package home

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// EnvHome is the preferred data-dir override for touch-squirrel.
	EnvHome = "SQUIRREL_HOME"
	// EnvHomeLegacy keeps Grok-Register / touch-xai-register compatibility.
	EnvHomeLegacy = "GROK_HOME"
	// DirName is the default directory under the user home.
	DirName = ".touch-squirrel"
	// DirNameLegacy is the old Grok data directory.
	DirNameLegacy = ".grok"
)

// Paths holds all filesystem locations under the squirrel home.
type Paths struct {
	Root         string
	Config       string
	PID          string
	Lock         string
	State        string
	LogsDir      string
	Outputs      string
	ExportsDir   string // export job zip volumes
	UploadCache  string // upload dedup cache json
	PatrolState  string // pool patrol snapshot json
	ClusterState string // federation node registry / meta
	StatusLayout string // public status page layout json
	LocalPool    string // local credential pool dir
	TmpDir       string // multipart upload temp files
	Clearance    string // optional: bundled compose path override
	PluginsDir   string // installed plugins
	EnabledFile  string // enabled.json
	ArtifactsDir string // unified artifact store
	SecretsDir   string // encrypted secrets
	MarketCache  string // market download cache
	AccountsDB   string // unified account pool sqlite
	NotifyFile   string // notification channels json
}

// Resolve picks the data root with this precedence:
//  1. SQUIRREL_HOME
//  2. GROK_HOME (legacy)
//  3. ~/.touch-squirrel if it exists
//  4. ~/.grok if it exists (legacy data)
//  5. default ~/.touch-squirrel
func Resolve() (Paths, error) {
	root := os.Getenv(EnvHome)
	if root == "" {
		root = os.Getenv(EnvHomeLegacy)
	}
	if root == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		modern := filepath.Join(h, DirName)
		legacy := filepath.Join(h, DirNameLegacy)
		switch {
		case dirExists(modern):
			root = modern
		case dirExists(legacy):
			root = legacy
		default:
			root = modern
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		Root:         root,
		Config:       filepath.Join(root, "config.env"),
		PID:          filepath.Join(root, "run.pid"),
		Lock:         filepath.Join(root, "run.lock"),
		State:        filepath.Join(root, "state.json"),
		LogsDir:      filepath.Join(root, "logs"),
		Outputs:      filepath.Join(root, "outputs"),
		ExportsDir:   filepath.Join(root, "exports"),
		UploadCache:  filepath.Join(root, "upload-cache.json"),
		PatrolState:  filepath.Join(root, "patrol-state.json"),
		ClusterState: filepath.Join(root, "cluster-state.json"),
		StatusLayout: filepath.Join(root, "status-layout.json"),
		LocalPool:    filepath.Join(root, "local-pool"),
		TmpDir:       filepath.Join(root, "tmp"),
		PluginsDir:   filepath.Join(root, "plugins"),
		EnabledFile:  filepath.Join(root, "enabled.json"),
		ArtifactsDir: filepath.Join(root, "artifacts"),
		SecretsDir:   filepath.Join(root, "secrets"),
		MarketCache:  filepath.Join(root, "market-cache"),
		AccountsDB:   filepath.Join(root, "accounts.db"),
		NotifyFile:   filepath.Join(root, "notifications.json"),
	}
	return p, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func (p Paths) EnsureBase() error {
	for _, d := range []string{
		p.Root, p.LogsDir, p.Outputs, p.ExportsDir, p.TmpDir, p.LocalPool,
		p.PluginsDir, p.ArtifactsDir, p.SecretsDir, p.MarketCache,
	} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// NewRunID returns yyyymmdd-HHMMSS in local time.
func NewRunID() string {
	return time.Now().Format("20060102-150405")
}

// RunDirs is created before the first credential file is written.
type RunDirs struct {
	RunID     string
	Root      string
	SSO       string
	CPA       string
	Discarded string
	LogPath   string
}

func (p Paths) PrepareRun(runID string) (RunDirs, error) {
	if runID == "" {
		runID = NewRunID()
	}
	root := filepath.Join(p.Outputs, runID)
	rd := RunDirs{
		RunID:     runID,
		Root:      root,
		SSO:       filepath.Join(root, "SSO"),
		CPA:       filepath.Join(root, "CPA"),
		Discarded: filepath.Join(root, "discarded"),
		LogPath:   filepath.Join(p.LogsDir, fmt.Sprintf("run-%s.log", runID)),
	}
	for _, d := range []string{rd.Root, rd.SSO, rd.CPA, rd.Discarded} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return RunDirs{}, err
		}
	}
	return rd, nil
}
