package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/state"
)

func TestStoreWritesTerminalRunSidecarPreservingPlugin(t *testing.T) {
	root := t.TempDir()
	const runID = "20260813-120000"
	outputDir := filepath.Join(root, "outputs", runID)
	store := state.NewStore(filepath.Join(root, "state.json"))

	if err := store.Set(func(snapshot *state.Snapshot) {
		*snapshot = state.Snapshot{
			Status:    state.StatusRunning,
			RunID:     runID,
			Plugin:    "bridge-register",
			Phase:     state.PhaseRegister,
			Target:    4,
			OutputDir: outputDir,
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(func(snapshot *state.Snapshot) {
		snapshot.Status = state.StatusCompleted
		snapshot.Phase = state.PhaseIdle
		snapshot.Done = 4
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "run.json")); err != nil {
		t.Fatalf("run sidecar was not written: %v", err)
	}
	run, err := state.LoadRun(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if run.RunID != runID || run.Plugin != "bridge-register" || run.Status != state.StatusCompleted || run.Done != 4 {
		t.Fatalf("run sidecar lost terminal metadata: %+v", run)
	}
}
