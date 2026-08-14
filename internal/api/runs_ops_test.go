package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/home"
	"github.com/grok-free-register/grok-reg/internal/localpool"
	"github.com/grok-free-register/grok-reg/internal/runmetrics"
	"github.com/grok-free-register/grok-reg/internal/state"
)

func TestRunsAPIListsPersistedMetadataAndServesMetrics(t *testing.T) {
	paths := newRunAPITestPaths(t)
	const (
		runID  = "20260813-120000"
		plugin = "bridge-register"
	)
	run, err := paths.PrepareRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.CPA, "account.json"), []byte(`{"email":"alpha@example.test","access_token":"fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	collector := runmetrics.New(run.Root, runID, plugin, runmetrics.Environment{EmailProvider: "fixture-mail"})
	for _, account := range []struct {
		email      string
		durationMS int64
		status     string
	}{
		{email: "alpha@example.test", durationMS: 100, status: "completed"},
		{email: "bravo@example.test", durationMS: 200, status: "completed"},
		{email: "charlie@example.test", durationMS: 300, status: "failed"},
		{email: "delta@example.test", durationMS: 400, status: "success"},
		{email: "echo@example.test", durationMS: 500, status: "failed"},
	} {
		collector.RecordReportedAccount(account.email, account.durationMS, account.status, nil, "")
	}
	collector.Finish("completed", nil)

	store := state.NewStore(paths.State)
	if err := store.Set(func(snapshot *state.Snapshot) {
		*snapshot = state.Snapshot{
			Status:    state.StatusCompleted,
			RunID:     runID,
			Plugin:    plugin,
			Phase:     state.PhaseIdle,
			Target:    5,
			Done:      3,
			OutputDir: run.Root,
		}
	}); err != nil {
		t.Fatal(err)
	}
	added, _, err := localpool.New(paths.LocalPool).ImportRun(run.Root)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("imported entries=%d", added)
	}

	server := New(Options{Paths: paths})
	listRes := serveRunAPI(t, server, http.MethodGet, "/api/runs")
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRes.Code, listRes.Body.String())
	}
	var listing struct {
		OK    bool `json:"ok"`
		Total int  `json:"total"`
		Runs  []struct {
			ID                 string `json:"id"`
			Plugin             string `json:"plugin"`
			Status             string `json:"status"`
			ImportedCount      int    `json:"imported_count"`
			AverageAccountMS   int64  `json:"average_account_ms"`
			AccountMetricCount int    `json:"account_metric_count"`
		} `json:"runs"`
	}
	if err := json.NewDecoder(listRes.Body).Decode(&listing); err != nil {
		t.Fatal(err)
	}
	if !listing.OK || listing.Total != 1 || len(listing.Runs) != 1 {
		t.Fatalf("run listing=%+v", listing)
	}
	listed := listing.Runs[0]
	if listed.ID != runID || listed.Plugin != plugin || listed.Status != string(state.StatusCompleted) || listed.ImportedCount != 1 || listed.AverageAccountMS != 300 || listed.AccountMetricCount != 5 {
		t.Fatalf("listed run=%+v", listed)
	}

	metricsRes := serveRunAPI(t, server, http.MethodGet, "/api/runs/"+runID+"/metrics")
	if metricsRes.Code != http.StatusOK {
		t.Fatalf("metrics status=%d body=%s", metricsRes.Code, metricsRes.Body.String())
	}
	var metrics struct {
		OK               bool                 `json:"ok"`
		RunID            string               `json:"run_id"`
		MetricsAvailable bool                 `json:"metrics_available"`
		Metrics          runmetrics.Snapshot  `json:"metrics"`
		Summary          runmetrics.Aggregate `json:"summary"`
	}
	if err := json.NewDecoder(metricsRes.Body).Decode(&metrics); err != nil {
		t.Fatal(err)
	}
	if !metrics.OK || !metrics.MetricsAvailable || metrics.RunID != runID || metrics.Metrics.Plugin != plugin || len(metrics.Metrics.Accounts) != 5 {
		t.Fatalf("metrics response=%+v", metrics)
	}
	if metrics.Metrics.Accounts[0].Label != "a•••@example.test" {
		t.Fatalf("metrics response exposed unmasked account=%+v", metrics.Metrics.Accounts[0])
	}
	if metrics.Summary.AverageAccountMS != 300 || metrics.Summary.P50AccountMS != 300 || metrics.Summary.P95AccountMS != 400 || metrics.Summary.Fastest == nil || metrics.Summary.Fastest.DurationMS != 100 || metrics.Summary.Slowest == nil || metrics.Summary.Slowest.DurationMS != 500 {
		t.Fatalf("metrics summary=%+v", metrics.Summary)
	}
}

func TestRunsAPIDeleteRefusesRunningRun(t *testing.T) {
	paths := newRunAPITestPaths(t)
	const runID = "20260813-130000"
	run, err := paths.PrepareRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run.Root, "marker"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := state.NewStore(paths.State)
	if err := store.Set(func(snapshot *state.Snapshot) {
		*snapshot = state.Snapshot{
			Status:    state.StatusRunning,
			RunID:     runID,
			Plugin:    "bridge-register",
			Phase:     state.PhaseRegister,
			OutputDir: run.Root,
		}
	}); err != nil {
		t.Fatal(err)
	}

	server := New(Options{Paths: paths})
	deleteRes := serveRunAPI(t, server, http.MethodDelete, "/api/runs/"+runID)
	if deleteRes.Code != http.StatusConflict {
		t.Fatalf("delete running run status=%d body=%s", deleteRes.Code, deleteRes.Body.String())
	}
	if _, err := os.Stat(filepath.Join(run.Root, "marker")); err != nil {
		t.Fatalf("running run was deleted: %v", err)
	}
}

func newRunAPITestPaths(t *testing.T) home.Paths {
	t.Helper()
	t.Setenv(home.EnvHome, t.TempDir())
	paths, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureBase(); err != nil {
		t.Fatal(err)
	}
	return paths
}

func serveRunAPI(t *testing.T, server *Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(method, target, nil))
	return response
}
