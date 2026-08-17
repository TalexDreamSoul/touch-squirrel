package analytics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

func TestAddAccumulatesPerHourAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.json")
	store := New(path)

	yesterday := time.Now().AddDate(0, 0, -1)
	store.Add(map[string]int64{"patrol.runs": 1, "patrol.healthy": 10})
	store.Add(map[string]int64{"patrol.runs": 2})
	store.AddAt(yesterday, map[string]int64{"patrol.runs": 5})

	reloaded := New(path)
	series := reloaded.Series(2, time.UTC)
	if len(series) != 2 {
		t.Fatalf("Series(2) = %d days, want 2", len(series))
	}
	if got := series[0].Metrics["patrol.runs"]; got != 5 {
		t.Errorf("yesterday patrol.runs = %d, want 5", got)
	}
	if got := series[1].Metrics["patrol.runs"]; got != 3 {
		t.Errorf("today patrol.runs = %d, want 3 (1+2 accumulated)", got)
	}
	if got := series[1].Metrics["patrol.healthy"]; got != 10 {
		t.Errorf("today patrol.healthy = %d, want 10", got)
	}
}

// The whole point of hour buckets: the same event lands on a different calendar
// day depending on the viewer's timezone.
func TestSeriesRebucketsByTimezone(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "analytics.json"))
	// 2026-03-10 02:00 UTC. In UTC+8 that is already the 10th at 10:00; in
	// UTC-8 it is still the 9th at 18:00.
	store.AddAt(time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC), map[string]int64{"degrade.scans": 1})

	east := time.FixedZone("UTC+8", 8*3600)
	west := time.FixedZone("UTC-8", -8*3600)

	if got := store.Earliest(east); got != "2026-03-10" {
		t.Errorf("Earliest(UTC+8) = %q, want 2026-03-10", got)
	}
	if got := store.Earliest(west); got != "2026-03-09" {
		t.Errorf("Earliest(UTC-8) = %q, want 2026-03-09", got)
	}
}

// A heatmap needs every calendar cell, including the quiet ones, so Series must
// emit a dense ascending range rather than only the days that saw events.
func TestSeriesIsDenseAndAscending(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "analytics.json"))
	store.AddAt(time.Now().AddDate(0, 0, -5), map[string]int64{"degrade.scans": 1})

	series := store.Series(7, time.UTC)
	if len(series) != 7 {
		t.Fatalf("Series(7) = %d days, want 7", len(series))
	}
	for i := 1; i < len(series); i++ {
		if series[i-1].Date >= series[i].Date {
			t.Fatalf("dates not ascending at %d: %q then %q", i, series[i-1].Date, series[i].Date)
		}
	}
	if want := time.Now().UTC().Format(DateFormat); series[len(series)-1].Date != want {
		t.Errorf("last day = %q, want %q", series[len(series)-1].Date, want)
	}
	var withEvents int
	for _, day := range series {
		if len(day.Metrics) > 0 {
			withEvents++
		}
	}
	if withEvents != 1 {
		t.Errorf("days carrying metrics = %d, want 1", withEvents)
	}
}

// Zero deltas are common (a patrol that found nothing dead) and must not create
// a bucket, or every quiet day would read as "active" on the heatmap.
func TestZeroDeltasDoNotCreateBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.json")
	store := New(path)
	store.Add(map[string]int64{"patrol.dead": 0})

	if len(store.hours) != 0 {
		t.Fatalf("hours = %v, want empty", store.hours)
	}
	for _, day := range store.Series(3, time.UTC) {
		if len(day.Metrics) != 0 {
			t.Errorf("day %s carries metrics %v, want none", day.Date, day.Metrics)
		}
	}
}

func TestPruneKeepsRetentionWindow(t *testing.T) {
	// Empty path keeps this in memory: persisting ~9600 growing snapshots costs
	// twenty seconds and proves nothing about pruning.
	store := New("")
	for i := 0; i < retentionHours+40; i++ {
		store.AddAt(time.Now().Add(-time.Duration(i)*time.Hour), map[string]int64{"patrol.runs": 1})
	}
	if len(store.hours) > retentionHours {
		t.Errorf("retained %d hours, want at most %d", len(store.hours), retentionHours)
	}
}

// v1 files stored calendar days. They must survive the upgrade rather than
// silently dropping the only patrol history an existing install has.
func TestMigratesLegacyDayBuckets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.json")
	if err := writeFile(path, `{"version":1,"days":{"2026-08-16":{"patrol.runs":4}}}`); err != nil {
		t.Fatal(err)
	}
	store := New(path)

	for _, zone := range []*time.Location{
		time.UTC,
		time.FixedZone("UTC+8", 8*3600),
		time.FixedZone("UTC-8", -8*3600),
	} {
		if got := store.Earliest(zone); got != "2026-08-16" {
			t.Errorf("Earliest(%s) = %q, want 2026-08-16 — legacy day must not drift", zone, got)
		}
	}
}

func TestNewToleratesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.json")
	if err := writeFile(path, "{not json"); err != nil {
		t.Fatal(err)
	}
	store := New(path)
	store.Add(map[string]int64{"patrol.runs": 1})
	if got := store.Totals(1, time.UTC)["patrol.runs"]; got != 1 {
		t.Errorf("patrol.runs = %d, want 1 after recovering from corrupt state", got)
	}
}

func TestLocationFallsBackToServerZone(t *testing.T) {
	if loc, ok := Location("Asia/Shanghai"); !ok || loc.String() != "Asia/Shanghai" {
		t.Errorf("Location(Asia/Shanghai) = %v, %v; want the named zone", loc, ok)
	}
	for _, name := range []string{"", "   ", "Not/AZone"} {
		if loc, ok := Location(name); ok || loc != time.Local {
			t.Errorf("Location(%q) = %v, %v; want server local and false", name, loc, ok)
		}
	}
}
