package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/home"
)

func newAnalyticsServer(t *testing.T, displayTimezone string) *Server {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(home.EnvHome, dir)
	paths, err := home.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureBase(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DisplayTimezone = displayTimezone
	if err := saveConfigWithSecrets(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	return New(Options{Paths: paths})
}

// The day boundary decides which heatmap column an event lands in, so the
// precedence between client, config and server has to be exact.
func TestAnalyticsTimezonePrecedence(t *testing.T) {
	cases := []struct {
		name       string
		configTZ   string
		requestTZ  string
		wantName   string
		wantSource string
	}{
		{"request wins over config", "Asia/Shanghai", "America/New_York", "America/New_York", "request"},
		{"config used when client sends none", "Asia/Shanghai", "", "Asia/Shanghai", "config"},
		{"server zone when neither is set", "", "", time.Local.String(), "server"},
		{"unknown request falls back to config", "Asia/Shanghai", "Not/AZone", "Asia/Shanghai", "config"},
		{"unknown config falls back to server", "Not/AZone", "", time.Local.String(), "server"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newAnalyticsServer(t, tc.configTZ)
			url := "/api/analytics/overview?days=7"
			if tc.requestTZ != "" {
				url += "&tz=" + tc.requestTZ
			}
			res := httptest.NewRecorder()
			s.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, url, nil))
			if res.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			var payload struct {
				Timezone struct {
					Name   string `json:"name"`
					Source string `json:"source"`
				} `json:"timezone"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Timezone.Name != tc.wantName || payload.Timezone.Source != tc.wantSource {
				t.Errorf("timezone = %q/%q, want %q/%q",
					payload.Timezone.Name, payload.Timezone.Source, tc.wantName, tc.wantSource)
			}
		})
	}
}

// Storing a zone that cannot be loaded would silently degrade every later
// request to the server's own zone, so it is rejected at write time.
func TestConfigRejectsUnknownDisplayTimezone(t *testing.T) {
	s := newAnalyticsServer(t, "")
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"display_timezone":"Mars/Olympus"}`))
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", res.Code, res.Body.String())
	}
}

func TestConfigAcceptsValidDisplayTimezone(t *testing.T) {
	s := newAnalyticsServer(t, "")
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"display_timezone":"Asia/Shanghai"}`))
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", res.Code, res.Body.String())
	}

	get := httptest.NewRecorder()
	s.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var wrapper struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &wrapper); err != nil {
		t.Fatal(err)
	}
	if wrapper.Config["display_timezone"] != "Asia/Shanghai" {
		t.Errorf("display_timezone = %v, want Asia/Shanghai", wrapper.Config["display_timezone"])
	}

	// And it must actually drive the analytics day boundary, not just round-trip.
	res = httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/analytics/overview?days=7", nil))
	var payload struct {
		Timezone struct {
			Name          string `json:"name"`
			OffsetMinutes int    `json:"offset_minutes"`
		} `json:"timezone"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Timezone.Name != "Asia/Shanghai" || payload.Timezone.OffsetMinutes != 480 {
		t.Errorf("analytics timezone = %q/%d, want Asia/Shanghai/480",
			payload.Timezone.Name, payload.Timezone.OffsetMinutes)
	}
}

// A calendar is a dense day range, so the first and last cells must line up
// with the requested window in the requested zone.
func TestAnalyticsCalendarSpansRequestedRange(t *testing.T) {
	s := newAnalyticsServer(t, "")
	res := httptest.NewRecorder()
	s.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/analytics/overview?days=30&tz=Asia/Shanghai", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Range struct {
			Days int    `json:"days"`
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"range"`
		Calendars []struct {
			Key  string `json:"key"`
			Days []struct {
				Date string `json:"date"`
			} `json:"days"`
		} `json:"calendars"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Now().In(shanghai).Format("2006-01-02"); payload.Range.To != want {
		t.Errorf("range.to = %q, want today in Asia/Shanghai (%q)", payload.Range.To, want)
	}
	if len(payload.Calendars) == 0 {
		t.Fatal("no calendars returned")
	}
	for _, calendar := range payload.Calendars {
		if len(calendar.Days) != payload.Range.Days {
			t.Errorf("calendar %q has %d cells, want %d", calendar.Key, len(calendar.Days), payload.Range.Days)
			continue
		}
		if calendar.Days[0].Date != payload.Range.From {
			t.Errorf("calendar %q starts at %q, want %q", calendar.Key, calendar.Days[0].Date, payload.Range.From)
		}
		if last := calendar.Days[len(calendar.Days)-1].Date; last != payload.Range.To {
			t.Errorf("calendar %q ends at %q, want %q", calendar.Key, last, payload.Range.To)
		}
	}
}
