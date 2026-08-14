// Package degrade detects and tracks xAI free-tier 降智 (silent model
// downgrade) across the CPA account pool.
//
// The verdict comes from one real /responses call per account: a healthy
// account answers with a chain of thought, a flagged one answers with no
// reasoning at all. That call is not free — it burns the account's free quota
// and, more importantly, counts toward xAI's per-IP "how many distinct
// accounts used this exit recently" rule, which is what converts healthy
// accounts into flagged ones. Scans are therefore sampled and paced against
// that same rule rather than run over the whole pool.
package degrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/cpa"
)

// Verdicts.
const (
	VerdictNormal    = "normal"    // chain of thought present
	VerdictDegraded  = "degraded"  // answered, but no chain of thought
	VerdictExhausted = "exhausted" // free quota gone / rate limited — undecidable
	VerdictError     = "error"     // token dead, network, unexpected status
)

// probePrompt forces a little arithmetic so a reasoning model actually thinks;
// probeAnswer lets the UI show a second, weaker signal (a downgraded model
// often gets even this wrong).
const (
	probePrompt = "127*8=? 只回答数字"
	probeAnswer = "1016"
)

const historyCap = 50

// Record is the latest verdict for one CPA auth file.
type Record struct {
	Name            string     `json:"name"`
	Email           string     `json:"email,omitempty"`
	Verdict         string     `json:"verdict"`
	Model           string     `json:"model,omitempty"`
	ReasoningTokens int        `json:"reasoning_tokens"`
	AnswerOK        bool       `json:"answer_ok"`
	Exit            string     `json:"exit,omitempty"`
	Detail          string     `json:"detail,omitempty"`
	CheckedAt       time.Time  `json:"checked_at"`
	FirstDegradedAt *time.Time `json:"first_degraded_at,omitempty"`
	Isolated        bool       `json:"isolated"`
	IsolatedAt      *time.Time `json:"isolated_at,omitempty"`
}

// ScanRecord summarises one scan.
type ScanRecord struct {
	Time       time.Time `json:"time"`
	Checked    int       `json:"checked"`
	Normal     int       `json:"normal"`
	Degraded   int       `json:"degraded"`
	Exhausted  int       `json:"exhausted"`
	Errored    int       `json:"errored"`
	PoolTotal  int       `json:"pool_total"`
	PacedMS    int64     `json:"paced_ms"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

// Overview is the panel's headline picture.
type Overview struct {
	PoolTotal int `json:"pool_total"` // accounts seen in the last scan's CPA listing
	Checked   int `json:"checked"`    // accounts with a stored verdict
	Normal    int `json:"normal"`
	Degraded  int `json:"degraded"`
	Exhausted int `json:"exhausted"`
	Errored   int `json:"errored"`
	Isolated  int `json:"isolated"`
	Unchecked int `json:"unchecked"`
}

// Status describes the scan runner.
type Status struct {
	Running bool        `json:"running"`
	Note    string      `json:"note,omitempty"`
	LastRun *time.Time  `json:"last_run,omitempty"`
	Last    *ScanRecord `json:"last,omitempty"`
}

// ExitStat is one proxy exit's flag-2 exposure inside the rolling window.
// Counts cover this panel's own probe traffic only — CPA's serving traffic
// goes out over its own exits and is invisible here.
type ExitStat struct {
	Exit      string    `json:"exit"`
	Accounts  int       `json:"accounts"`
	Requests  int       `json:"requests"`
	Degraded  int       `json:"degraded"`
	WindowMin int       `json:"window_min"`
	Cap       int       `json:"cap"`
	Over      bool      `json:"over"`
	LastAt    time.Time `json:"last_at"`
}

// ManagementAPI is the subset of the CPA client a scan needs.
type ManagementAPI interface {
	List() ([]cpa.AuthMeta, error)
	Download(name string) ([]byte, error)
}

// ProbeFunc is the injectable probe (tests replace it).
type ProbeFunc func(doc cpa.Document, proxy string, opt cpa.ProbeOptions) (cpa.ProbeResult, error)

type exitEvent struct {
	Exit     string    `json:"exit"`
	Account  string    `json:"account"`
	At       time.Time `json:"at"`
	Degraded bool      `json:"degraded"`
}

// Service owns the verdict store, the scan runner and the exit window.
type Service struct {
	statePath string
	cfgFn     func() config.Config
	newClient func(config.Config) ManagementAPI
	probeFn   ProbeFunc

	mu        sync.Mutex
	running   bool
	note      string
	records   map[string]Record
	history   []ScanRecord
	last      *ScanRecord
	lastRun   *time.Time
	poolTotal int
	events    []exitEvent
}

// New builds the service and restores persisted state.
func New(statePath string, cfgFn func() config.Config, newClient func(config.Config) ManagementAPI) *Service {
	s := &Service{
		statePath: statePath,
		cfgFn:     cfgFn,
		newClient: newClient,
		probeFn:   cpa.ProbeDetail,
		records:   map[string]Record{},
	}
	s.load()
	return s
}

type persisted struct {
	Records   map[string]Record `json:"records,omitempty"`
	History   []ScanRecord      `json:"history,omitempty"`
	Last      *ScanRecord       `json:"last,omitempty"`
	PoolTotal int               `json:"pool_total,omitempty"`
	Events    []exitEvent       `json:"events,omitempty"`
}

func (s *Service) load() {
	b, err := os.ReadFile(s.statePath)
	if err != nil {
		return
	}
	var p persisted
	if json.Unmarshal(b, &p) != nil {
		return
	}
	if p.Records != nil {
		s.records = p.Records
	}
	s.history = p.History
	s.last = p.Last
	s.poolTotal = p.PoolTotal
	s.events = p.Events
	if s.last != nil {
		t := s.last.Time
		s.lastRun = &t
	}
}

func (s *Service) saveLocked() {
	p := persisted{
		Records:   s.records,
		History:   s.history,
		Last:      s.last,
		PoolTotal: s.poolTotal,
		Events:    s.events,
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	tmp := s.statePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, s.statePath)
	}
}

// ScanOptions overrides the configured sample size for a manual run.
type ScanOptions struct {
	Sample  int  // 0 = use config
	Recheck bool // also re-probe accounts checked inside the recheck window
}

// Scan probes a sample of the pool. It is safe to call concurrently; a second
// call while one is running returns an error instead of doubling the traffic.
func (s *Service) Scan(ctx context.Context, opt ScanOptions) (*ScanRecord, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, fmt.Errorf("降智扫描正在进行中")
	}
	s.running = true
	s.note = "准备中"
	s.mu.Unlock()

	started := time.Now()
	rec := &ScanRecord{Time: started}
	err := s.scanOnce(ctx, opt, rec)
	rec.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		rec.Error = err.Error()
	}

	s.mu.Lock()
	s.running = false
	s.note = ""
	now := time.Now()
	s.lastRun = &now
	s.last = rec
	s.history = append(s.history, *rec)
	if len(s.history) > historyCap {
		s.history = s.history[len(s.history)-historyCap:]
	}
	s.saveLocked()
	s.mu.Unlock()
	return rec, nil
}

func (s *Service) scanOnce(ctx context.Context, opt ScanOptions, rec *ScanRecord) error {
	cfg := s.cfgFn()
	if strings.TrimSpace(cfg.CPAManagementKey) == "" {
		return fmt.Errorf("未配置 CPA_MANAGEMENT_KEY")
	}
	client := s.newClient(cfg)
	list, err := client.List()
	if err != nil {
		return fmt.Errorf("拉取远端列表失败: %v", err)
	}
	rec.PoolTotal = len(list)

	s.mu.Lock()
	s.poolTotal = len(list)
	s.mu.Unlock()

	sample := opt.Sample
	if sample <= 0 {
		sample = cfg.DegradeSample
	}
	if sample <= 0 {
		sample = 5
	}
	targets := s.pickTargets(list, sample, opt.Recheck, cfg)
	if len(targets) == 0 {
		return nil
	}

	exit := ExitLabel(cfg.RegisterProxy)
	window := time.Duration(max(cfg.DegradeExitWindowMin, 1)) * time.Minute
	limit := max(cfg.DegradeExitAccountCap, 1)
	probeOpt := cpa.ProbeOptions{
		Model:           degradeModel(cfg),
		Prompt:          probePrompt,
		MaxOutputTokens: 512,
	}

	// Every probe is a distinct account hitting one exit, which is the exact
	// shape of the rule that flags accounts. Pace the scan against it instead
	// of manufacturing the damage we are looking for.
	for i, m := range targets {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		waited, err := s.waitForExitSlot(ctx, exit, window, limit, i, len(targets))
		if err != nil {
			return err
		}
		rec.PacedMS += waited.Milliseconds()

		s.setNote(fmt.Sprintf("探测 %d/%d", i+1, len(targets)))
		r := s.probeOne(client, m, probeOpt, cfg.RegisterProxy, exit)
		rec.Checked++
		switch r.Verdict {
		case VerdictNormal:
			rec.Normal++
		case VerdictDegraded:
			rec.Degraded++
		case VerdictExhausted:
			rec.Exhausted++
		default:
			rec.Errored++
		}
		s.commit(r, exit, window)
	}
	return nil
}

// pickTargets prefers never-checked accounts, then the least recently checked.
func (s *Service) pickTargets(list []cpa.AuthMeta, sample int, recheck bool, cfg config.Config) []cpa.AuthMeta {
	minInterval := time.Duration(max(cfg.DegradeRecheckMin, 0)) * time.Minute
	s.mu.Lock()
	defer s.mu.Unlock()

	type cand struct {
		meta cpa.AuthMeta
		seen bool
		at   time.Time
	}
	var cands []cand
	for _, m := range list {
		if m.Disabled {
			continue
		}
		rec, ok := s.records[m.Name]
		if ok && rec.Isolated {
			continue
		}
		if ok && !recheck && minInterval > 0 && time.Since(rec.CheckedAt) < minInterval {
			continue
		}
		c := cand{meta: m, seen: ok}
		if ok {
			c.at = rec.CheckedAt
		}
		cands = append(cands, c)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].seen != cands[j].seen {
			return !cands[i].seen // never-checked first
		}
		return cands[i].at.Before(cands[j].at)
	})
	if len(cands) > sample {
		cands = cands[:sample]
	}
	out := make([]cpa.AuthMeta, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.meta)
	}
	return out
}

// waitForExitSlot blocks until the exit has room for one more distinct account
// inside the rolling window. Returns how long it waited.
func (s *Service) waitForExitSlot(ctx context.Context, exit string, window time.Duration, limit, idx, total int) (time.Duration, error) {
	started := time.Now()
	for {
		free, next := s.exitSlot(exit, window, limit)
		if free {
			return time.Since(started), nil
		}
		delay := time.Until(next) + time.Second
		if delay < time.Second {
			delay = time.Second
		}
		s.setNote(fmt.Sprintf("出口 %s 已达 %d 账号/%s 上限，等待 %s 后继续 (%d/%d)",
			exit, limit, window, delay.Round(time.Second), idx+1, total))
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return time.Since(started), ctx.Err()
		case <-t.C:
		}
	}
}

// exitSlot reports whether the exit is under cap, and when the oldest in-window
// event ages out if it is not.
func (s *Service) exitSlot(exit string, window time.Duration, limit int) (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-window)
	seen := map[string]bool{}
	oldest := time.Time{}
	for _, e := range s.events {
		if e.Exit != exit || e.At.Before(cutoff) {
			continue
		}
		if !seen[e.Account] {
			seen[e.Account] = true
			if oldest.IsZero() || e.At.Before(oldest) {
				oldest = e.At
			}
		}
	}
	if len(seen) < limit {
		return true, time.Time{}
	}
	return false, oldest.Add(window)
}

func (s *Service) probeOne(client ManagementAPI, m cpa.AuthMeta, opt cpa.ProbeOptions, proxy, exit string) Record {
	rec := Record{
		Name:      m.Name,
		Email:     m.Email,
		Model:     opt.Model,
		Exit:      exit,
		CheckedAt: time.Now(),
	}
	raw, err := client.Download(m.Name)
	if err != nil {
		rec.Verdict, rec.Detail = VerdictError, "下载失败: "+err.Error()
		return rec
	}
	var doc cpa.Document
	if json.Unmarshal(raw, &doc) != nil || doc.AccessToken == "" {
		rec.Verdict, rec.Detail = VerdictError, "auth 文件无 access_token"
		return rec
	}
	if rec.Email == "" {
		rec.Email = doc.Email
	}
	res, err := s.probeFn(doc, proxy, opt)
	rec.ReasoningTokens = res.ReasoningTokens
	rec.AnswerOK = strings.Contains(res.OutputText, probeAnswer)
	if res.Model != "" {
		rec.Model = res.Model
	}
	switch {
	case err != nil:
		low := strings.ToLower(err.Error())
		if cpa.IsQuotaExhausted("", low) || strings.Contains(low, "rate/exhausted") ||
			strings.Contains(low, "429") || strings.Contains(low, "rate limit") {
			rec.Verdict, rec.Detail = VerdictExhausted, "额度用尽/限流，本次无法判定"
			return rec
		}
		rec.Verdict, rec.Detail = VerdictError, truncate(err.Error(), 200)
	case res.HasReasoning:
		rec.Verdict = VerdictNormal
		if !rec.AnswerOK {
			rec.Detail = "有思维链但答案不符，留意"
		}
	default:
		rec.Verdict, rec.Detail = VerdictDegraded, "无思维链"
	}
	return rec
}

// commit stores one verdict and its exit event, preserving the first time the
// account was seen degraded.
func (s *Service) commit(rec Record, exit string, window time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.records[rec.Name]; ok {
		rec.Isolated = prev.Isolated
		rec.IsolatedAt = prev.IsolatedAt
		rec.FirstDegradedAt = prev.FirstDegradedAt
	}
	if rec.Verdict == VerdictDegraded && rec.FirstDegradedAt == nil {
		t := rec.CheckedAt
		rec.FirstDegradedAt = &t
	}
	s.records[rec.Name] = rec
	s.events = append(s.events, exitEvent{
		Exit:     exit,
		Account:  rec.Name,
		At:       rec.CheckedAt,
		Degraded: rec.Verdict == VerdictDegraded,
	})
	s.pruneEventsLocked(window)
	s.saveLocked()
}

// pruneEventsLocked keeps the window plus a small tail for the exit table.
func (s *Service) pruneEventsLocked(window time.Duration) {
	keep := window * 6
	if keep < time.Hour {
		keep = time.Hour
	}
	cutoff := time.Now().Add(-keep)
	out := s.events[:0]
	for _, e := range s.events {
		if e.At.After(cutoff) {
			out = append(out, e)
		}
	}
	s.events = out
	if len(s.events) > 5000 {
		s.events = s.events[len(s.events)-5000:]
	}
}

func (s *Service) setNote(note string) {
	s.mu.Lock()
	s.note = note
	s.mu.Unlock()
}

// Overview summarises stored verdicts against the last known pool size.
func (s *Service) Overview() Overview {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := Overview{PoolTotal: s.poolTotal, Checked: len(s.records)}
	for _, r := range s.records {
		switch r.Verdict {
		case VerdictNormal:
			o.Normal++
		case VerdictDegraded:
			o.Degraded++
		case VerdictExhausted:
			o.Exhausted++
		default:
			o.Errored++
		}
		if r.Isolated {
			o.Isolated++
		}
	}
	if o.PoolTotal > o.Checked {
		o.Unchecked = o.PoolTotal - o.Checked
	}
	return o
}

// Status reports the scan runner state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Running: s.running, Note: s.note, Last: s.last}
	if s.lastRun != nil {
		t := *s.lastRun
		st.LastRun = &t
	}
	return st
}

// Records returns stored verdicts filtered by verdict and a name/email query,
// newest check first.
func (s *Service) Records(verdict, q string) []Record {
	verdict = strings.TrimSpace(verdict)
	q = strings.ToLower(strings.TrimSpace(q))
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		if verdict != "" && verdict != "all" && r.Verdict != verdict {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Name), q) &&
			!strings.Contains(strings.ToLower(r.Email), q) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CheckedAt.After(out[j].CheckedAt) })
	return out
}

// History returns recent scans, newest first.
func (s *Service) History() []ScanRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ScanRecord, len(s.history))
	for i, r := range s.history {
		out[len(s.history)-1-i] = r
	}
	return out
}

// Exits summarises probe traffic per exit inside the configured window.
func (s *Service) Exits() []ExitStat {
	cfg := s.cfgFn()
	windowMin := max(cfg.DegradeExitWindowMin, 1)
	limit := max(cfg.DegradeExitAccountCap, 1)
	cutoff := time.Now().Add(-time.Duration(windowMin) * time.Minute)

	s.mu.Lock()
	defer s.mu.Unlock()
	byExit := map[string]*ExitStat{}
	accounts := map[string]map[string]bool{}
	for _, e := range s.events {
		if e.At.Before(cutoff) {
			continue
		}
		st := byExit[e.Exit]
		if st == nil {
			st = &ExitStat{Exit: e.Exit, WindowMin: windowMin, Cap: limit}
			byExit[e.Exit] = st
			accounts[e.Exit] = map[string]bool{}
		}
		st.Requests++
		if e.Degraded {
			st.Degraded++
		}
		if e.At.After(st.LastAt) {
			st.LastAt = e.At
		}
		accounts[e.Exit][e.Account] = true
	}
	out := make([]ExitStat, 0, len(byExit))
	for exit, st := range byExit {
		st.Accounts = len(accounts[exit])
		st.Over = st.Accounts >= limit
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Accounts > out[j].Accounts })
	return out
}

// SetIsolated marks or clears local isolation. This never touches the remote
// pool: the forum evidence on whether a flag clears itself is inconclusive, so
// deleting on a degraded verdict would be an irreversible bet on a guess.
func (s *Service) SetIsolated(name string, isolated bool) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[name]
	if !ok {
		return Record{}, fmt.Errorf("未找到记录: %s", name)
	}
	rec.Isolated = isolated
	if isolated {
		now := time.Now()
		rec.IsolatedAt = &now
	} else {
		rec.IsolatedAt = nil
	}
	s.records[name] = rec
	s.saveLocked()
	return rec, nil
}

// IsolateDegraded marks every stored degraded verdict as isolated in one pass.
func (s *Service) IsolateDegraded() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	now := time.Now()
	for name, rec := range s.records {
		if rec.Verdict != VerdictDegraded || rec.Isolated {
			continue
		}
		rec.Isolated = true
		rec.IsolatedAt = &now
		s.records[name] = rec
		n++
	}
	if n > 0 {
		s.saveLocked()
	}
	return n
}

// ExitLabel renders a proxy URL for display without its credentials.
func ExitLabel(proxy string) string {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return "direct"
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" {
		return "proxy"
	}
	return u.Scheme + "://" + u.Host
}

func degradeModel(cfg config.Config) string {
	if m := strings.TrimSpace(cfg.DegradeModel); m != "" {
		return m
	}
	return "grok-4.6"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
