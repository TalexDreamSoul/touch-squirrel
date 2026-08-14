package runmetrics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const FileName = "metrics.json"

type Environment struct {
	OS                string `json:"os,omitempty"`
	Arch              string `json:"arch,omitempty"`
	Hostname          string `json:"hostname,omitempty"`
	EmailProvider     string `json:"email_provider,omitempty"`
	TurnstileProvider string `json:"turnstile_provider,omitempty"`
	ResinPlatform     string `json:"resin_platform,omitempty"`
	ProxyEndpoint     string `json:"proxy_endpoint,omitempty"`
	EgressIP          string `json:"egress_ip,omitempty"`
}

type Stage struct {
	Name       string `json:"name"`
	StartedAt  string `json:"started_at"`
	EndedAt    string `json:"ended_at,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Status     string `json:"status,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Account struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	StartedAt   string  `json:"started_at,omitempty"`
	CompletedAt string  `json:"completed_at,omitempty"`
	DurationMS  int64   `json:"duration_ms,omitempty"`
	Status      string  `json:"status"`
	Error       string  `json:"error,omitempty"`
	Stages      []Stage `json:"stages,omitempty"`
	Reported    bool    `json:"reported,omitempty"`
}

type Snapshot struct {
	Version     int         `json:"version"`
	RunID       string      `json:"run_id"`
	Plugin      string      `json:"plugin"`
	StartedAt   string      `json:"started_at"`
	CompletedAt string      `json:"completed_at,omitempty"`
	DurationMS  int64       `json:"duration_ms,omitempty"`
	Status      string      `json:"status"`
	Error       string      `json:"error,omitempty"`
	Environment Environment `json:"environment"`
	Stages      []Stage     `json:"stages,omitempty"`
	Accounts    []Account   `json:"accounts,omitempty"`
}

type accountState struct {
	account Account
	started time.Time
	stages  map[string]time.Time
}

type Collector struct {
	mu          sync.Mutex
	path        string
	snapshot    Snapshot
	stages      map[string]time.Time
	accounts    map[string]*accountState
	accountSeen map[string]bool
}

func New(outputDir, runID, plugin string, environment Environment) *Collector {
	now := time.Now().UTC()
	c := &Collector{
		path: filepath.Join(outputDir, FileName),
		snapshot: Snapshot{
			Version:     1,
			RunID:       runID,
			Plugin:      plugin,
			StartedAt:   now.Format(time.RFC3339Nano),
			Status:      "running",
			Environment: environment,
			Accounts:    []Account{},
			Stages:      []Stage{},
		},
		stages:      map[string]time.Time{},
		accounts:    map[string]*accountState{},
		accountSeen: map[string]bool{},
	}
	c.mu.Lock()
	_ = c.persistLocked()
	c.mu.Unlock()
	return c
}

func NewEnvironment(emailProvider, turnstileProvider, resinPlatform, proxyRaw string) Environment {
	host, _ := os.Hostname()
	return Environment{
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		Hostname:          host,
		EmailProvider:     strings.TrimSpace(emailProvider),
		TurnstileProvider: strings.TrimSpace(turnstileProvider),
		ResinPlatform:     strings.TrimSpace(resinPlatform),
		ProxyEndpoint:     RedactedEndpoint(proxyRaw),
	}
}

func RedactedEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "configured"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = ""
	return u.String()
}

func DetectEgressIP(proxyRaw string, timeout time.Duration) string {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyRaw = strings.TrimSpace(proxyRaw); proxyRaw != "" {
		proxyURL, err := url.Parse(proxyRaw)
		if err != nil || (proxyURL.Scheme != "http" && proxyURL.Scheme != "https") {
			return ""
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	resp, err := client.Get("https://www.cloudflare.com/cdn-cgi/trace")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	var payload strings.Builder
	buf := make([]byte, 4096)
	for payload.Len() < 16384 {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			payload.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}
	for _, line := range strings.Split(payload.String(), "\n") {
		if ip := strings.TrimSpace(strings.TrimPrefix(line, "ip=")); strings.HasPrefix(line, "ip=") && netIP(ip) {
			return ip
		}
	}
	return ""
}

func netIP(value string) bool {
	parsed := net.ParseIP(value)
	return parsed != nil
}

func (c *Collector) SetEgressIP(ip string) {
	if c == nil || !netIP(strings.TrimSpace(ip)) {
		return
	}
	c.mu.Lock()
	c.snapshot.Environment.EgressIP = strings.TrimSpace(ip)
	_ = c.persistLocked()
	c.mu.Unlock()
}

func (c *Collector) StartStage(name string) {
	if c == nil || strings.TrimSpace(name) == "" {
		return
	}
	c.mu.Lock()
	c.stages[name] = time.Now().UTC()
	c.mu.Unlock()
}

func (c *Collector) EndStage(name, status string, err error) {
	if c == nil || strings.TrimSpace(name) == "" {
		return
	}
	now := time.Now().UTC()
	c.mu.Lock()
	started, ok := c.stages[name]
	if !ok {
		started = now
	}
	delete(c.stages, name)
	stage := Stage{Name: name, StartedAt: started.Format(time.RFC3339Nano), EndedAt: now.Format(time.RFC3339Nano), DurationMS: now.Sub(started).Milliseconds(), Status: status}
	if err != nil {
		stage.Error = err.Error()
	}
	c.snapshot.Stages = append(c.snapshot.Stages, stage)
	_ = c.persistLocked()
	c.mu.Unlock()
}

func (c *Collector) StartAccount(email string) {
	if c == nil || strings.TrimSpace(email) == "" {
		return
	}
	now := time.Now().UTC()
	c.mu.Lock()
	if _, ok := c.accounts[email]; !ok {
		c.accounts[email] = &accountState{
			account: Account{ID: accountID(email), Label: MaskEmail(email), StartedAt: now.Format(time.RFC3339Nano), Status: "running", Stages: []Stage{}},
			started: now,
			stages:  map[string]time.Time{},
		}
	}
	c.mu.Unlock()
}

func (c *Collector) StartAccountStage(email, name string) {
	if c == nil || strings.TrimSpace(email) == "" || strings.TrimSpace(name) == "" {
		return
	}
	c.StartAccount(email)
	c.mu.Lock()
	if account := c.accounts[email]; account != nil {
		account.stages[name] = time.Now().UTC()
	}
	c.mu.Unlock()
}

func (c *Collector) EndAccountStage(email, name, status string, err error) {
	if c == nil || strings.TrimSpace(email) == "" || strings.TrimSpace(name) == "" {
		return
	}
	now := time.Now().UTC()
	c.mu.Lock()
	account := c.accounts[email]
	if account != nil {
		started, ok := account.stages[name]
		if !ok {
			started = now
		}
		delete(account.stages, name)
		stage := Stage{Name: name, StartedAt: started.Format(time.RFC3339Nano), EndedAt: now.Format(time.RFC3339Nano), DurationMS: now.Sub(started).Milliseconds(), Status: status}
		if err != nil {
			stage.Error = err.Error()
		}
		account.account.Stages = append(account.account.Stages, stage)
	}
	c.mu.Unlock()
}

func (c *Collector) FinishAccount(email, status string, err error) {
	if c == nil || strings.TrimSpace(email) == "" {
		return
	}
	now := time.Now().UTC()
	c.mu.Lock()
	account := c.accounts[email]
	if account == nil || c.accountSeen[email] {
		c.mu.Unlock()
		return
	}
	for name, started := range account.stages {
		account.account.Stages = append(account.account.Stages, Stage{Name: name, StartedAt: started.Format(time.RFC3339Nano), EndedAt: now.Format(time.RFC3339Nano), DurationMS: now.Sub(started).Milliseconds(), Status: "interrupted"})
	}
	account.account.CompletedAt = now.Format(time.RFC3339Nano)
	account.account.DurationMS = now.Sub(account.started).Milliseconds()
	account.account.Status = status
	if err != nil {
		account.account.Error = err.Error()
	}
	c.snapshot.Accounts = append(c.snapshot.Accounts, account.account)
	c.accountSeen[email] = true
	delete(c.accounts, email)
	_ = c.persistLocked()
	c.mu.Unlock()
}

func (c *Collector) RecordReportedAccount(email string, durationMS int64, status string, stages []Stage, errText string) {
	if c == nil || strings.TrimSpace(email) == "" || durationMS <= 0 {
		return
	}
	now := time.Now().UTC()
	c.mu.Lock()
	if c.accountSeen[email] {
		c.mu.Unlock()
		return
	}
	started := now.Add(-time.Duration(durationMS) * time.Millisecond)
	c.snapshot.Accounts = append(c.snapshot.Accounts, Account{
		ID: accountID(email), Label: MaskEmail(email), StartedAt: started.Format(time.RFC3339Nano), CompletedAt: now.Format(time.RFC3339Nano), DurationMS: durationMS, Status: status, Error: errText, Stages: stages, Reported: true,
	})
	c.accountSeen[email] = true
	_ = c.persistLocked()
	c.mu.Unlock()
}

func (c *Collector) Finish(status string, err error) {
	if c == nil {
		return
	}
	now := time.Now().UTC()
	c.mu.Lock()
	for email, account := range c.accounts {
		account.account.CompletedAt = now.Format(time.RFC3339Nano)
		account.account.DurationMS = now.Sub(account.started).Milliseconds()
		account.account.Status = "incomplete"
		if err != nil {
			account.account.Error = err.Error()
		} else {
			account.account.Error = "run ended before account completed"
		}
		c.snapshot.Accounts = append(c.snapshot.Accounts, account.account)
		c.accountSeen[email] = true
		delete(c.accounts, email)
	}
	for name, started := range c.stages {
		c.snapshot.Stages = append(c.snapshot.Stages, Stage{Name: name, StartedAt: started.Format(time.RFC3339Nano), EndedAt: now.Format(time.RFC3339Nano), DurationMS: now.Sub(started).Milliseconds(), Status: status})
		delete(c.stages, name)
	}
	started, parseErr := time.Parse(time.RFC3339Nano, c.snapshot.StartedAt)
	if parseErr == nil {
		c.snapshot.DurationMS = now.Sub(started).Milliseconds()
	}
	c.snapshot.CompletedAt = now.Format(time.RFC3339Nano)
	c.snapshot.Status = status
	if err != nil {
		c.snapshot.Error = err.Error()
	}
	_ = c.persistLocked()
	c.mu.Unlock()
}

func Load(runDir string) (Snapshot, error) {
	data, err := os.ReadFile(filepath.Join(runDir, FileName))
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (c *Collector) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func accountID(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:6])
}

func MaskEmail(email string) string {
	parts := strings.SplitN(strings.TrimSpace(email), "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "••••"
	}
	local := []rune(parts[0])
	visible := string(local[0])
	if len(local) > 1 {
		visible += "•••"
	}
	return visible + "@" + parts[1]
}

type Extreme struct {
	RunID       string      `json:"run_id"`
	Plugin      string      `json:"plugin"`
	AccountID   string      `json:"account_id"`
	Account     string      `json:"account"`
	DurationMS  int64       `json:"duration_ms"`
	CompletedAt string      `json:"completed_at"`
	Status      string      `json:"status"`
	Environment Environment `json:"environment"`
}

type StageAggregate struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	AvgMS int64  `json:"avg_ms"`
	P50MS int64  `json:"p50_ms"`
	P95MS int64  `json:"p95_ms"`
	MaxMS int64  `json:"max_ms"`
}

type Point struct {
	RunID            string  `json:"run_id"`
	Plugin           string  `json:"plugin"`
	Time             string  `json:"time"`
	RunDurationMS    int64   `json:"run_duration_ms"`
	AverageAccountMS int64   `json:"average_account_ms"`
	P95AccountMS     int64   `json:"p95_account_ms"`
	AccountCount     int     `json:"account_count"`
	SuccessRate      float64 `json:"success_rate"`
}

type Aggregate struct {
	RunCount          int              `json:"run_count"`
	AccountCount      int              `json:"account_count"`
	AverageAccountMS  int64            `json:"average_account_ms"`
	P50AccountMS      int64            `json:"p50_account_ms"`
	P95AccountMS      int64            `json:"p95_account_ms"`
	SuccessRate       float64          `json:"success_rate"`
	FailureRate       float64          `json:"failure_rate"`
	ThroughputPerHour float64          `json:"throughput_per_hour"`
	Fastest           *Extreme         `json:"fastest,omitempty"`
	Slowest           *Extreme         `json:"slowest,omitempty"`
	Stages            []StageAggregate `json:"stages"`
	Points            []Point          `json:"points"`
}

func BuildAggregate(runs []Snapshot) Aggregate {
	result := Aggregate{RunCount: len(runs), Stages: []StageAggregate{}, Points: []Point{}}
	var durations []int64
	stageDurations := map[string][]int64{}
	var successes int
	var totalRunMS int64
	for _, run := range runs {
		var runDurations []int64
		var runSuccess int
		for _, account := range run.Accounts {
			if account.DurationMS <= 0 {
				continue
			}
			result.AccountCount++
			durations = append(durations, account.DurationMS)
			runDurations = append(runDurations, account.DurationMS)
			if account.Status == "completed" || account.Status == "success" {
				successes++
				runSuccess++
			}
			extreme := &Extreme{RunID: run.RunID, Plugin: run.Plugin, AccountID: account.ID, Account: account.Label, DurationMS: account.DurationMS, CompletedAt: account.CompletedAt, Status: account.Status, Environment: run.Environment}
			if result.Fastest == nil || account.DurationMS < result.Fastest.DurationMS {
				result.Fastest = extreme
			}
			if result.Slowest == nil || account.DurationMS > result.Slowest.DurationMS {
				result.Slowest = extreme
			}
			for _, stage := range account.Stages {
				if stage.DurationMS > 0 {
					stageDurations[stage.Name] = append(stageDurations[stage.Name], stage.DurationMS)
				}
			}
		}
		if run.DurationMS > 0 {
			totalRunMS += run.DurationMS
		}
		point := Point{RunID: run.RunID, Plugin: run.Plugin, Time: run.StartedAt, RunDurationMS: run.DurationMS, AccountCount: len(runDurations)}
		if len(runDurations) > 0 {
			point.AverageAccountMS = average(runDurations)
			point.P95AccountMS = percentile(runDurations, 0.95)
			point.SuccessRate = float64(runSuccess) / float64(len(runDurations))
		}
		result.Points = append(result.Points, point)
	}
	if len(durations) > 0 {
		result.AverageAccountMS = average(durations)
		result.P50AccountMS = percentile(durations, 0.50)
		result.P95AccountMS = percentile(durations, 0.95)
		result.SuccessRate = float64(successes) / float64(len(durations))
		result.FailureRate = 1 - result.SuccessRate
	}
	if totalRunMS > 0 {
		result.ThroughputPerHour = float64(successes) / (float64(totalRunMS) / float64(time.Hour/time.Millisecond))
	}
	for name, values := range stageDurations {
		result.Stages = append(result.Stages, StageAggregate{Name: name, Count: len(values), AvgMS: average(values), P50MS: percentile(values, 0.50), P95MS: percentile(values, 0.95), MaxMS: percentile(values, 1)})
	}
	sort.Slice(result.Stages, func(i, j int) bool { return result.Stages[i].AvgMS > result.Stages[j].AvgMS })
	sort.Slice(result.Points, func(i, j int) bool { return result.Points[i].Time < result.Points[j].Time })
	return result
}

func average(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	var total int64
	for _, value := range values {
		total += value
	}
	return total / int64(len(values))
}

func percentile(values []int64, quantile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(float64(len(ordered)-1) * quantile)
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}
