// Package analytics keeps an hour-grained counter ledger for the panel charts.
//
// Patrol and degrade only retain their last 50 raw records, so a calendar
// heatmap built from them would be empty past a few hours. This ledger folds
// each event into a counter bucket the moment it happens, which stays small
// enough to keep for years. Sources that already persist full history (run
// metrics, the account store, the upload cache) are aggregated on read instead
// and never written here.
//
// Buckets are keyed by UTC hour rather than by calendar day. A day bucket can
// only ever be read back in the timezone that wrote it, and the panel lets each
// viewer pick their own — so the day boundary has to be applied on read.
package analytics

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// retentionHours bounds the ledger at ~400 days, comfortably more than the
// longest calendar range the panel offers (365 days).
const retentionHours = 400 * 24

// DateFormat is the calendar-day layout used by the panel's calendars.
const DateFormat = "2006-01-02"

// bucketFormat is the on-disk key layout: UTC hour.
const bucketFormat = "2006-01-02T15"

// Day is one calendar day of counters, resolved in a caller-chosen timezone.
type Day struct {
	Date    string           `json:"date"`
	Metrics map[string]int64 `json:"metrics"`
}

// Store is a persisted UTC-hour → metric → count ledger.
type Store struct {
	mu    sync.Mutex
	path  string
	hours map[string]map[string]int64
}

type persisted struct {
	Version int                         `json:"version"`
	Hours   map[string]map[string]int64 `json:"hours,omitempty"`
	// Days holds the v1 layout (server-local calendar days). Read for migration
	// only; never written.
	Days map[string]map[string]int64 `json:"days,omitempty"`
}

// New restores the ledger from disk. A missing or corrupt file starts empty:
// analytics data is derived, never authoritative, so it must not block startup.
func New(path string) *Store {
	s := &Store{path: path, hours: map[string]map[string]int64{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var p persisted
	if json.Unmarshal(b, &p) != nil {
		return s
	}
	for hour, metrics := range p.Hours {
		s.hours[hour] = metrics
	}
	// v1 buckets recorded a calendar day with no hour. Midday UTC keeps them on
	// that same date for every timezone within UTC-12..UTC+12.
	for day, metrics := range p.Days {
		key := day + "T12"
		if existing := s.hours[key]; existing != nil {
			for name, value := range metrics {
				existing[name] += value
			}
			continue
		}
		s.hours[key] = metrics
	}
	return s
}

// Add folds counts into the current UTC hour. Metrics with a zero delta are
// skipped so a no-op run does not create an empty bucket.
func (s *Store) Add(metrics map[string]int64) {
	s.AddAt(time.Now(), metrics)
}

// AddAt folds counts into the bucket for t's UTC hour.
func (s *Store) AddAt(t time.Time, metrics map[string]int64) {
	if s == nil || len(metrics) == 0 {
		return
	}
	key := t.UTC().Format(bucketFormat)
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.hours[key]
	if bucket == nil {
		bucket = map[string]int64{}
		s.hours[key] = bucket
	}
	changed := false
	for name, delta := range metrics {
		if delta == 0 {
			continue
		}
		bucket[name] += delta
		changed = true
	}
	if !changed {
		if len(bucket) == 0 {
			delete(s.hours, key)
		}
		return
	}
	s.pruneLocked()
	s.saveLocked()
}

// Series returns the last n calendar days in loc as a dense, ascending slice —
// every day present even when it has no events, because a heatmap needs the
// gaps. A nil loc means UTC.
func (s *Store) Series(n int, loc *time.Location) []Day {
	if n <= 0 {
		n = 90
	}
	if n > retentionHours/24 {
		n = retentionHours / 24
	}
	if loc == nil {
		loc = time.UTC
	}

	byDay := s.foldToDays(loc)
	now := time.Now().In(loc)
	out := make([]Day, 0, n)
	for i := n - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format(DateFormat)
		bucket := byDay[date]
		if bucket == nil {
			bucket = map[string]int64{}
		}
		out = append(out, Day{Date: date, Metrics: bucket})
	}
	return out
}

// Totals sums every metric across the last n days in loc.
func (s *Store) Totals(n int, loc *time.Location) map[string]int64 {
	out := map[string]int64{}
	for _, day := range s.Series(n, loc) {
		for name, value := range day.Metrics {
			out[name] += value
		}
	}
	return out
}

// Earliest reports the first calendar day in loc that holds any event, so the
// UI can distinguish "nothing happened" from "not recorded yet". Empty when the
// ledger has never recorded anything.
func (s *Store) Earliest(loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	earliest := ""
	for date, metrics := range s.foldToDays(loc) {
		if len(metrics) == 0 {
			continue
		}
		if earliest == "" || date < earliest {
			earliest = date
		}
	}
	return earliest
}

// foldToDays re-buckets the UTC-hour ledger into calendar days in loc.
func (s *Store) foldToDays(loc *time.Location) map[string]map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]map[string]int64{}
	for hour, metrics := range s.hours {
		parsed, err := time.ParseInLocation(bucketFormat, hour, time.UTC)
		if err != nil {
			continue
		}
		date := parsed.In(loc).Format(DateFormat)
		bucket := out[date]
		if bucket == nil {
			bucket = map[string]int64{}
			out[date] = bucket
		}
		for name, value := range metrics {
			bucket[name] += value
		}
	}
	return out
}

func (s *Store) pruneLocked() {
	if len(s.hours) <= retentionHours {
		return
	}
	keys := make([]string, 0, len(s.hours))
	for hour := range s.hours {
		keys = append(keys, hour)
	}
	sort.Strings(keys)
	for _, hour := range keys[:len(keys)-retentionHours] {
		delete(s.hours, hour)
	}
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	b, err := json.Marshal(persisted{Version: 2, Hours: s.hours})
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// Location resolves an IANA timezone name, falling back to the server's local
// zone when the name is empty or unknown. The bool reports whether name was
// actually used, so callers can tell the UI which zone it is looking at.
func Location(name string) (*time.Location, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.Local, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local, false
	}
	return loc, true
}
