package runmetrics_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/runmetrics"
)

func TestCollectorPersistsMaskedReportedAccount(t *testing.T) {
	const (
		runID  = "20260813-120000"
		plugin = "bridge-register"
		email  = "alice@example.test"
	)

	dir := t.TempDir()
	collector := runmetrics.New(dir, runID, plugin, runmetrics.Environment{EmailProvider: "fixture-mail"})
	collector.RecordReportedAccount(email, 375, "failed", []runmetrics.Stage{{
		Name:       "oauth",
		DurationMS: 275,
		Status:     "failed",
		Error:      "provider rejected",
	}}, "provider rejected")
	collector.Finish("completed", nil)

	snapshot, err := runmetrics.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RunID != runID || snapshot.Plugin != plugin || snapshot.Status != "completed" {
		t.Fatalf("persisted run metadata=%+v", snapshot)
	}
	if len(snapshot.Accounts) != 1 {
		t.Fatalf("accounts=%+v", snapshot.Accounts)
	}
	account := snapshot.Accounts[0]
	if account.Label != "a•••@example.test" || account.ID == "" || strings.Contains(account.ID, email) {
		t.Fatalf("account identifier was not masked: %+v", account)
	}
	if account.DurationMS != 375 || account.Status != "failed" || !account.Reported || len(account.Stages) != 1 || account.Stages[0].Error != "provider rejected" {
		t.Fatalf("persisted account=%+v", account)
	}

	raw, err := os.ReadFile(filepath.Join(dir, runmetrics.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), email) {
		t.Fatalf("metrics persistence leaked raw email: %s", raw)
	}
}

func TestBuildAggregateReportsPercentilesAndMaskedExtremes(t *testing.T) {
	aggregate := runmetrics.BuildAggregate([]runmetrics.Snapshot{
		{
			RunID:  "20260813-120000",
			Plugin: "bridge-register",
			Accounts: []runmetrics.Account{
				{ID: "acct-a", Label: "a•••@example.test", DurationMS: 100, Status: "completed"},
				{ID: "acct-b", Label: "b•••@example.test", DurationMS: 200, Status: "completed"},
				{ID: "acct-c", Label: "c•••@example.test", DurationMS: 300, Status: "failed"},
			},
		},
		{
			RunID:  "20260813-130000",
			Plugin: "bridge-register",
			Accounts: []runmetrics.Account{
				{ID: "acct-d", Label: "d•••@example.test", DurationMS: 400, Status: "success"},
				{ID: "acct-e", Label: "e•••@example.test", DurationMS: 500, Status: "failed"},
			},
		},
	})

	if aggregate.RunCount != 2 || aggregate.AccountCount != 5 {
		t.Fatalf("aggregate counts=%+v", aggregate)
	}
	if aggregate.AverageAccountMS != 300 || aggregate.P50AccountMS != 300 || aggregate.P95AccountMS != 400 {
		t.Fatalf("aggregate percentiles=%+v", aggregate)
	}
	if aggregate.Fastest == nil || aggregate.Fastest.RunID != "20260813-120000" || aggregate.Fastest.AccountID != "acct-a" || aggregate.Fastest.Account != "a•••@example.test" || aggregate.Fastest.DurationMS != 100 {
		t.Fatalf("fastest=%+v", aggregate.Fastest)
	}
	if aggregate.Slowest == nil || aggregate.Slowest.RunID != "20260813-130000" || aggregate.Slowest.AccountID != "acct-e" || aggregate.Slowest.Account != "e•••@example.test" || aggregate.Slowest.DurationMS != 500 {
		t.Fatalf("slowest=%+v", aggregate.Slowest)
	}
}
