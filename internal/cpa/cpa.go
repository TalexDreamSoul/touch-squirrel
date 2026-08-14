package cpa

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grok-free-register/grok-reg/internal/oauth"
)

const (
	CliproxyBase = "https://cli-chat-proxy.grok.com/v1"
)

var CliproxyHeaders = map[string]string{
	"x-grok-client-version":    "0.2.93",
	"x-xai-token-auth":         "xai-grok-cli",
	"X-XAI-Token-Auth":         "xai-grok-cli",
	"x-authenticateresponse":   "authenticate-response",
	"x-grok-client-identifier": "grok-shell",
	"x-compaction-at":          "400000",
	"User-Agent":               "grok-shell/0.2.93 (linux; x86_64)",
}

// Document is CPA-ready JSON.
type Document struct {
	Type          string            `json:"type"`
	AccessToken   string            `json:"access_token"`
	RefreshToken  string            `json:"refresh_token"`
	IDToken       string            `json:"id_token,omitempty"`
	TokenType     string            `json:"token_type,omitempty"`
	ExpiresIn     int               `json:"expires_in"`
	Expired       string            `json:"expired"`
	LastRefresh   string            `json:"last_refresh"`
	Sub           string            `json:"sub,omitempty"`
	Email         string            `json:"email,omitempty"`
	BaseURL       string            `json:"base_url"`
	TokenEndpoint string            `json:"token_endpoint"`
	AuthKind      string            `json:"auth_kind"`
	Headers       map[string]string `json:"headers"`
}

func FromCredential(cred oauth.Credential, email string) Document {
	em := email
	if em == "" {
		em = cred.Email
	}
	return Document{
		Type:          "xai",
		AccessToken:   cred.AccessToken,
		RefreshToken:  cred.RefreshToken,
		IDToken:       cred.IDToken,
		TokenType:     cred.TokenType,
		ExpiresIn:     cred.ExpiresIn,
		Expired:       cred.ExpiresAt,
		LastRefresh:   cred.LastRefresh,
		Sub:           cred.Subject,
		Email:         em,
		BaseURL:       CliproxyBase,
		TokenEndpoint: cred.TokenEndpoint,
		AuthKind:      "oauth",
		Headers:       cloneHeaders(CliproxyHeaders),
	}
}

func Filename(doc Document, secret []byte) string {
	subject := doc.Sub
	if subject == "" {
		subject = doc.RefreshToken
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(subject))
	return "xai-" + hex.EncodeToString(mac.Sum(nil))[:16] + ".json"
}

func WriteAtomic(dir string, doc Document, secret []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := Filename(doc, secret)
	path := filepath.Join(dir, name)
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	_ = os.Chmod(path, 0o600)
	return path, nil
}

// ProbeOptions tunes one probe request. The zero value reproduces the legacy
// health probe (grok-4.5, 16 output tokens, "ok").
type ProbeOptions struct {
	Model           string
	Prompt          string
	MaxOutputTokens int
}

func (o ProbeOptions) withDefaults() ProbeOptions {
	if strings.TrimSpace(o.Model) == "" {
		o.Model = "grok-4.5"
	}
	if o.Prompt == "" {
		o.Prompt = "ok"
	}
	if o.MaxOutputTokens <= 0 {
		o.MaxOutputTokens = 16
	}
	return o
}

// ProbeResult records what a probe observed beyond alive/dead. HasReasoning is
// the degradation signal: a downgraded (风控降智) account answers with no chain
// of thought at all, so reasoning tokens drop to zero.
type ProbeResult struct {
	Model           string `json:"model"`
	Status          int    `json:"status"`
	HasReasoning    bool   `json:"has_reasoning"`
	ReasoningTokens int    `json:"reasoning_tokens"`
	OutputText      string `json:"output_text,omitempty"`
	DurationMS      int64  `json:"duration_ms"`
}

// httpClientFor builds a probe client bound to proxy. An unusable proxy is an
// error rather than a silent direct-connection fallback: leaking probes onto
// the host IP is exactly what trips xAI's per-IP account counter.
func httpClientFor(proxy string, timeout time.Duration) (*http.Client, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return &http.Client{Timeout: timeout}, nil
	}
	u, err := url.Parse(proxy)
	if err != nil {
		return nil, fmt.Errorf("proxy 解析失败: %v", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("proxy 无效: %s", proxy)
	}
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("proxy 不可用: 默认 transport 类型异常")
	}
	tr = tr.Clone()
	tr.Proxy = http.ProxyURL(u)
	return &http.Client{Timeout: timeout, Transport: tr}, nil
}

// Probe hits cli-chat-proxy with minimal responses call (acpa_watchdog shape).
// New tokens often get transient 403 permission-denied; warmup + short retries.
// Returns nil if alive.
func Probe(doc Document, proxy string) error {
	_, err := ProbeDetail(doc, proxy, ProbeOptions{})
	return err
}

// ProbeDetail is Probe with the observed response detail returned. The result
// is populated whenever a response came back, including on error.
func ProbeDetail(doc Document, proxy string, opt ProbeOptions) (ProbeResult, error) {
	opt = opt.withDefaults()
	client, err := httpClientFor(proxy, 45*time.Second)
	if err != nil {
		return ProbeResult{Model: opt.Model}, err
	}
	// Warmup: mint-then-immediate chat often 403s.
	time.Sleep(3 * time.Second)

	var last error
	var lastRes ProbeResult
	// Immediate 403 retries (default 2 sleeps of 4s like ACPA_403_IMMEDIATE_*)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(4 * time.Second)
		}
		res, err := probeOnce(client, doc, opt)
		if err == nil {
			return res, nil
		}
		last, lastRes = err, res
		msg := err.Error()
		// transient permission-denied — retry
		if strings.Contains(msg, "permission-denied") || strings.Contains(msg, "chat endpoint is denied") || strings.Contains(msg, "http=403") {
			continue
		}
		// non-retryable
		return res, err
	}
	return lastRes, last
}

func probeOnce(client *http.Client, doc Document, opt ProbeOptions) (ProbeResult, error) {
	res := ProbeResult{Model: opt.Model}
	started := time.Now()
	// Match keys/acpa_watchdog.py body exactly — bare content string can 403.
	payload := map[string]any{
		"model":             opt.Model,
		"store":             false,
		"stream":            false,
		"max_output_tokens": opt.MaxOutputTokens,
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": opt.Prompt},
				},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	base := strings.TrimRight(doc.BaseURL, "/")
	if base == "" {
		base = CliproxyBase
	}
	url := base + "/responses"
	if strings.HasSuffix(base, "/responses") {
		url = base
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return res, err
	}
	sid := "probe-" + doc.Sub
	if doc.Sub == "" {
		sid = fmt.Sprintf("probe-%d", time.Now().UnixNano())
	}
	rid := fmt.Sprintf("%d", time.Now().UnixNano())
	req.Header.Set("Authorization", "Bearer "+doc.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range doc.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("x-grok-session-id", sid)
	req.Header.Set("x-grok-conv-id", sid)
	req.Header.Set("x-grok-req-id", rid)
	req.Header.Set("x-grok-turn-idx", "1")
	if len(rid) >= 8 {
		req.Header.Set("x-grok-agent-id", "agent-"+rid[:8])
	}
	req.Header.Set("x-grok-model-override", opt.Model)
	if doc.Email != "" {
		req.Header.Set("x-email", doc.Email)
	}
	if doc.Sub != "" {
		req.Header.Set("x-userid", doc.Sub)
	}
	resp, err := client.Do(req)
	if err != nil {
		res.DurationMS = time.Since(started).Milliseconds()
		return res, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	res.DurationMS = time.Since(started).Milliseconds()
	res.Status = resp.StatusCode
	txt := string(body)
	low := strings.ToLower(txt)
	if resp.StatusCode == 200 {
		fillReasoning(&res, body)
		return res, nil
	}
	// free exhausted / rate limit: still treat as "alive enough" for CPA count?
	// Match watchdog: only 200 is alive; return error with marker.
	if resp.StatusCode == 429 || strings.Contains(low, "free-usage-exhausted") || strings.Contains(low, "rate limit") {
		return res, fmt.Errorf("probe http=%d rate/exhausted body=%s", resp.StatusCode, truncate(txt, 120))
	}
	return res, fmt.Errorf("probe http=%d body=%s", resp.StatusCode, truncate(txt, 160))
}

// responsesEnvelope is the slice of the /responses payload the probe reads.
type responsesEnvelope struct {
	Output []struct {
		Type    string `json:"type"`
		Summary []struct {
			Text string `json:"text"`
		} `json:"summary"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		OutputTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

// fillReasoning extracts the chain-of-thought signal and the answer text.
// Reasoning is reported two ways depending on the upstream build — token
// accounting in usage, or explicit reasoning items in output — so both count.
func fillReasoning(res *ProbeResult, body []byte) {
	var env responsesEnvelope
	if json.Unmarshal(body, &env) != nil {
		return
	}
	res.ReasoningTokens = env.Usage.OutputTokensDetails.ReasoningTokens
	if res.ReasoningTokens > 0 {
		res.HasReasoning = true
	}
	var answer strings.Builder
	for _, item := range env.Output {
		if item.Type == "reasoning" {
			for _, s := range item.Summary {
				if strings.TrimSpace(s.Text) != "" {
					res.HasReasoning = true
				}
			}
			for _, c := range item.Content {
				if strings.TrimSpace(c.Text) != "" {
					res.HasReasoning = true
				}
			}
			continue
		}
		for _, c := range item.Content {
			if c.Text != "" {
				answer.WriteString(c.Text)
			}
		}
	}
	res.OutputText = truncate(strings.TrimSpace(answer.String()), 200)
}

func AppendSSO(accountsPath, email, password, sso string) error {
	if err := os.MkdirAll(filepath.Dir(accountsPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(accountsPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s:%s:%s\n", email, password, sso)
	return err
}

func AppendAuthSession(path, email, sso string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	doc := map[string]any{
		"email": email,
		"cookies": []map[string]string{
			{"name": "sso", "value": sso, "domain": ".x.ai", "path": "/"},
		},
	}
	raw, _ := json.Marshal(doc)
	_, err = f.Write(append(raw, '\n'))
	return err
}

func cloneHeaders(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// DefaultSecret for filename hmac (local only).
func DefaultSecret() []byte {
	return []byte("grok-reg-local-cpa-name-secret")
}
