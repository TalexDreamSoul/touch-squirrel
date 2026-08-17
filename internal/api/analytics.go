package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/analytics"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
	"github.com/grok-free-register/grok-reg/internal/runmetrics"
)

// calendarDay is one cell of a contribution-style heatmap.
type calendarDay struct {
	Date   string           `json:"date"`
	Value  int64            `json:"value"`
	Detail map[string]int64 `json:"detail,omitempty"`
}

// calendar is one heatmap series plus the headline numbers shown beside it.
type calendar struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Description string        `json:"description"`
	Unit        string        `json:"unit"`
	Total       int64         `json:"total"`
	Max         int64         `json:"max"`
	ActiveDays  int           `json:"active_days"`
	Streak      int           `json:"streak"`
	Days        []calendarDay `json:"days"`
	// Partial marks a series whose history only starts when the ledger did, so
	// the UI can say "recorded since X" instead of implying the empty cells
	// were quiet days.
	Partial bool   `json:"partial,omitempty"`
	Since   string `json:"since,omitempty"`
}

type distributionItem struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
	Tone  string `json:"tone,omitempty"`
}

type distribution struct {
	Key   string             `json:"key"`
	Label string             `json:"label"`
	Total int64              `json:"total"`
	Items []distributionItem `json:"items"`
}

// handleAnalyticsOverview is the single feed behind the analytics dashboard.
//
// It mixes two kinds of source. Run metrics, the account store and the local
// pool keep full history on disk, so their calendars are rebuilt from scratch
// on every request and are correct all the way back. Patrol and degrade only
// keep their last 50 raw records, so their calendars come from the day ledger
// and start when the ledger did — those are flagged Partial.
func (s *Server) handleAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	days := 90
	if v := strings.TrimSpace(r.URL.Query().Get("days")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			days = n
		}
	}
	if days < 7 {
		days = 7
	}
	if days > 365 {
		days = 365
	}

	loc, source := s.resolveTimezone(r.URL.Query().Get("tz"))

	now := time.Now().In(loc)
	start := now.AddDate(0, 0, -(days - 1))
	from := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	dates := make([]string, 0, days)
	for i := 0; i < days; i++ {
		dates = append(dates, from.AddDate(0, 0, i).Format(analytics.DateFormat))
	}

	ledger := s.analytics.Series(days, loc)
	ledgerByDate := make(map[string]map[string]int64, len(ledger))
	for _, day := range ledger {
		ledgerByDate[day.Date] = day.Metrics
	}
	ledgerSince := s.analytics.Earliest(loc)

	registerDaily, registerRuns, stages, points := s.registerHistory(from, loc)
	accountDaily := s.accountDaily(from, loc)
	uploadDaily := s.uploadDaily(from, loc)

	calendars := []calendar{
		buildCalendar("register", "注册产出", "每日注册完成的账号数，按结果拆分", "个账号", dates, registerDaily),
		buildCalendar("accounts", "入池凭证", "每日进入统一账号库的凭证数", "条凭证", dates, accountDaily),
		buildCalendar("upload", "上传同步", "每日同步到主号池的凭证数", "条凭证", dates, uploadDaily),
		buildLedgerCalendar("patrol", "号池巡检", "每日巡检轮次与覆盖账号数", "次巡检", dates, ledgerByDate,
			"patrol.runs", map[string]string{
				"checked": "patrol.checked", "healthy": "patrol.healthy",
				"rate_limited": "patrol.rate_limited", "dead": "patrol.dead", "errors": "patrol.errors",
			}, ledgerSince),
		buildLedgerCalendar("degrade", "降智检测", "每日抽样扫描轮次与检出结果", "次扫描", dates, ledgerByDate,
			"degrade.scans", map[string]string{
				"checked": "degrade.checked", "normal": "degrade.normal",
				"degraded": "degrade.degraded", "exhausted": "degrade.exhausted", "errored": "degrade.errored",
			}, ledgerSince),
		buildLedgerCalendar("cleanup", "额度清理", "每日清理轮次与删除的耗尽账号数", "次清理", dates, ledgerByDate,
			"cleanup.runs", map[string]string{
				"scanned": "cleanup.scanned", "quota_hits": "cleanup.quota_hits", "deleted": "cleanup.deleted",
			}, ledgerSince),
	}

	_, offset := now.Zone()
	payload := map[string]any{
		"ok": true,
		"range": map[string]any{
			"days": days,
			"from": dates[0],
			"to":   dates[len(dates)-1],
		},
		"timezone": map[string]any{
			"name": loc.String(),
			// request | config | server — which input decided the day boundary.
			"source":         source,
			"offset_minutes": offset / 60,
		},
		"calendars":     calendars,
		"distributions": s.analyticsDistributions(),
		"stages":        stages,
		"points":        points,
		"register_runs": registerRuns,
		"ledger": map[string]any{
			"since":  ledgerSince,
			"totals": s.analytics.Totals(days, loc),
		},
	}
	writeJSON(w, http.StatusOK, payload)
}

// resolveTimezone picks the zone that decides where each calendar day starts:
// the client's request wins, then the configured default, then the server's own
// zone. An unknown name falls through rather than erroring — a bad tz should
// shift the columns, not break the dashboard.
func (s *Server) resolveTimezone(requested string) (*time.Location, string) {
	if loc, ok := analytics.Location(requested); ok {
		return loc, "request"
	}
	cfg, err := config.Load(s.opt.Paths.Config)
	if err == nil {
		if loc, ok := analytics.Location(cfg.DisplayTimezone); ok {
			return loc, "config"
		}
	}
	return time.Local, "server"
}

// registerHistory rebuilds per-day registration outcomes from the run metric
// snapshots on disk, which retain every account ever registered.
func (s *Server) registerHistory(from time.Time, loc *time.Location) (map[string]map[string]int64, int, []runmetrics.StageAggregate, []runmetrics.Point) {
	daily := map[string]map[string]int64{}
	dirs, err := cpa.ListRunDirs(s.opt.Paths.Outputs, 0)
	if err != nil {
		return daily, 0, nil, nil
	}
	runs := make([]runmetrics.Snapshot, 0, len(dirs))
	for _, dir := range dirs {
		snapshot, loadErr := runmetrics.Load(dir)
		if loadErr != nil {
			continue
		}
		// A run's accounts all finish within the run, so a run that ended
		// before the window cannot contribute a cell to it.
		if end := runEnd(snapshot); !end.IsZero() && end.Before(from) {
			continue
		}
		counted := false
		for _, account := range snapshot.Accounts {
			stamp := account.CompletedAt
			if stamp == "" {
				stamp = account.StartedAt
			}
			completed, parseErr := time.Parse(time.RFC3339Nano, stamp)
			if parseErr != nil || completed.Before(from) {
				continue
			}
			key := completed.In(loc).Format(analytics.DateFormat)
			bucket := daily[key]
			if bucket == nil {
				bucket = map[string]int64{}
				daily[key] = bucket
			}
			bucket["value"]++
			switch account.Status {
			case "completed", "success":
				bucket["success"]++
			case "failed", "error":
				bucket["failed"]++
			default:
				bucket["incomplete"]++
			}
			counted = true
		}
		if counted {
			runs = append(runs, snapshot)
		}
	}
	aggregate := runmetrics.BuildAggregate(runs)
	return daily, len(runs), aggregate.Stages, aggregate.Points
}

// runEnd is when a run stopped producing accounts. A run still in flight has no
// completion stamp; it returns the zero time so the caller keeps the run.
func runEnd(snapshot runmetrics.Snapshot) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, snapshot.CompletedAt)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// accountDaily buckets the unified account store by creation day.
func (s *Server) accountDaily(from time.Time, loc *time.Location) map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	if s.accounts == nil {
		return out
	}
	counts, err := s.accounts.DailyCounts("created_at", from.UTC().Format(time.RFC3339), loc)
	if err != nil {
		return out
	}
	for date, n := range counts {
		out[date] = map[string]int64{"value": int64(n)}
	}
	return out
}

// uploadDaily buckets local pool entries by the day they synced to the master
// pool. Unsynced entries have no day and are simply absent.
func (s *Server) uploadDaily(from time.Time, loc *time.Location) map[string]map[string]int64 {
	out := map[string]map[string]int64{}
	if s.localPool == nil {
		return out
	}
	for _, entry := range s.localPool.List() {
		if entry.SyncedAt == nil || entry.SyncedAt.Before(from) {
			continue
		}
		key := entry.SyncedAt.In(loc).Format(analytics.DateFormat)
		bucket := out[key]
		if bucket == nil {
			bucket = map[string]int64{}
			out[key] = bucket
		}
		bucket["value"]++
	}
	return out
}

// analyticsDistributions collects the current-state breakdowns that pair with
// the time series: what the pool looks like right now, not how it got there.
func (s *Server) analyticsDistributions() []distribution {
	out := []distribution{}

	if s.accounts != nil {
		if byStatus, byType, err := s.accounts.StatusCounts(); err == nil {
			out = append(out,
				distribution{Key: "account_status", Label: "账号状态", Items: sortedItems(byStatus, statusTone)},
				distribution{Key: "account_type", Label: "账号类型", Items: sortedItems(byType, nil)},
			)
		}
	}

	overview := s.degrade.Overview()
	out = append(out, distribution{Key: "degrade_verdict", Label: "降智结论", Items: []distributionItem{
		{Name: "正常", Value: int64(overview.Normal), Tone: "success"},
		{Name: "疑似降智", Value: int64(overview.Degraded), Tone: "critical"},
		{Name: "额度耗尽", Value: int64(overview.Exhausted), Tone: "warning"},
		// An errored probe says nothing about the account, so it stays neutral.
		{Name: "检测错误", Value: int64(overview.Errored), Tone: "neutral"},
		{Name: "未检测", Value: int64(overview.Unchecked), Tone: "neutral"},
	}})

	pool := s.patrol.Overview()
	out = append(out, distribution{Key: "pool_health", Label: "号池健康", Items: []distributionItem{
		{Name: "健康", Value: int64(pool.Healthy), Tone: "success"},
		{Name: "临时限流", Value: int64(pool.RateLimited), Tone: "warning"},
		{Name: "不可用", Value: int64(pool.Dead), Tone: "critical"},
		{Name: "已停用", Value: int64(pool.Disabled), Tone: "neutral"},
	}})

	for i := range out {
		var total int64
		for _, item := range out[i].Items {
			total += item.Value
		}
		out[i].Total = total
	}
	return out
}

// statusTone maps an account status to the panel's chart tone vocabulary
// ("success" | "warning" | "critical" | "neutral").
func statusTone(status string) string {
	switch status {
	case "active":
		return "success"
	case "exhausted":
		return "warning"
	case "disabled":
		return "neutral"
	default:
		return "critical"
	}
}

func sortedItems(counts map[string]int, tone func(string) string) []distributionItem {
	items := make([]distributionItem, 0, len(counts))
	for name, value := range counts {
		item := distributionItem{Name: name, Value: int64(value)}
		if tone != nil {
			item.Tone = tone(name)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Value != items[j].Value {
			return items[i].Value > items[j].Value
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func buildCalendar(key, label, description, unit string, dates []string, daily map[string]map[string]int64) calendar {
	c := calendar{Key: key, Label: label, Description: description, Unit: unit, Days: make([]calendarDay, 0, len(dates))}
	for _, date := range dates {
		bucket := daily[date]
		cell := calendarDay{Date: date}
		if len(bucket) > 0 {
			cell.Value = bucket["value"]
			detail := map[string]int64{}
			for name, value := range bucket {
				if name != "value" {
					detail[name] = value
				}
			}
			if len(detail) > 0 {
				cell.Detail = detail
			}
		}
		c.Days = append(c.Days, cell)
	}
	return finishCalendar(c)
}

func buildLedgerCalendar(key, label, description, unit string, dates []string,
	ledger map[string]map[string]int64, valueMetric string, detailMetrics map[string]string, since string) calendar {
	c := calendar{Key: key, Label: label, Description: description, Unit: unit,
		Partial: true, Since: since, Days: make([]calendarDay, 0, len(dates))}
	for _, date := range dates {
		bucket := ledger[date]
		cell := calendarDay{Date: date}
		if len(bucket) > 0 {
			cell.Value = bucket[valueMetric]
			detail := map[string]int64{}
			for name, metric := range detailMetrics {
				if value := bucket[metric]; value != 0 {
					detail[name] = value
				}
			}
			if len(detail) > 0 {
				cell.Detail = detail
			}
		}
		c.Days = append(c.Days, cell)
	}
	return finishCalendar(c)
}

func finishCalendar(c calendar) calendar {
	for _, day := range c.Days {
		c.Total += day.Value
		if day.Value > c.Max {
			c.Max = day.Value
		}
		if day.Value > 0 {
			c.ActiveDays++
		}
	}
	// Streak counts back from the most recent day that could still be active.
	for i := len(c.Days) - 1; i >= 0; i-- {
		if c.Days[i].Value > 0 {
			c.Streak++
			continue
		}
		// Today being empty does not break a streak — the day is not over.
		if i == len(c.Days)-1 {
			continue
		}
		break
	}
	return c
}
