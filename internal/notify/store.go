// Package notify manages host-level notification channels (Feishu bot, SMTP, …).
package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Channel kinds.
const (
	KindFeishu  = "feishu"
	KindSMTP    = "smtp"
	KindWebhook = "webhook"
)

// Common event keys channels can subscribe to.
var KnownEvents = []string{
	"register.started",
	"register.finished",
	"register.failed",
	"pool.low",
	"patrol.failed",
	"hunter.notice",
	"system.test",
}

// Channel is one notification target.
type Channel struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"` // feishu|smtp|webhook
	Enabled   bool              `json:"enabled"`
	Events    []string          `json:"events"`
	Config    map[string]string `json:"config"` // kind-specific; secrets masked on list when requested
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// Store is a file-backed channel registry.
type Store struct {
	path string
	mu   sync.Mutex
}

type fileSnap struct {
	Version  int       `json:"version"`
	Channels []Channel `json:"channels"`
}

// New opens (or will create) the store at path.
func New(path string) *Store {
	return &Store{path: path}
}

// Path returns the on-disk path.
func (s *Store) Path() string { return s.path }

func (s *Store) load() (fileSnap, error) {
	var snap fileSnap
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileSnap{Version: 1, Channels: []Channel{}}, nil
		}
		return snap, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return fileSnap{Version: 1, Channels: []Channel{}}, nil
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return snap, err
	}
	if snap.Channels == nil {
		snap.Channels = []Channel{}
	}
	if snap.Version == 0 {
		snap.Version = 1
	}
	return snap, nil
}

func (s *Store) save(snap fileSnap) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	snap.Version = 1
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns channels; redact secrets when redact=true.
func (s *Store) List(redact bool) ([]Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(snap.Channels))
	for _, c := range snap.Channels {
		if redact {
			c = maskChannel(c)
		}
		out = append(out, c)
	}
	return out, nil
}

// Get returns one channel by id.
func (s *Store) Get(id string, redact bool) (Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Channel{}, err
	}
	for _, c := range snap.Channels {
		if c.ID == id {
			if redact {
				return maskChannel(c), nil
			}
			return c, nil
		}
	}
	return Channel{}, fmt.Errorf("channel not found: %s", id)
}

// Create validates and appends a channel.
func (s *Store) Create(in Channel) (Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if in.Name == "" {
		return Channel{}, fmt.Errorf("name required")
	}
	if err := validateKind(in.Kind, in.Config); err != nil {
		return Channel{}, err
	}
	if in.Config == nil {
		in.Config = map[string]string{}
	}
	if in.Events == nil {
		in.Events = []string{}
	}
	if in.ID == "" {
		in.ID = fmt.Sprintf("ntf_%d", time.Now().UnixNano())
	}
	in.CreatedAt = now
	in.UpdatedAt = now

	snap, err := s.load()
	if err != nil {
		return Channel{}, err
	}
	for _, c := range snap.Channels {
		if c.ID == in.ID {
			return Channel{}, fmt.Errorf("id already exists")
		}
	}
	snap.Channels = append(snap.Channels, in)
	if err := s.save(snap); err != nil {
		return Channel{}, err
	}
	return maskChannel(in), nil
}

// Update merges fields on an existing channel.
func (s *Store) Update(id string, patch Channel) (Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Channel{}, err
	}
	idx := -1
	for i, c := range snap.Channels {
		if c.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Channel{}, fmt.Errorf("channel not found: %s", id)
	}
	cur := snap.Channels[idx]
	if n := strings.TrimSpace(patch.Name); n != "" {
		cur.Name = n
	}
	if k := strings.ToLower(strings.TrimSpace(patch.Kind)); k != "" {
		cur.Kind = k
	}
	// enabled always applied (bool zero is false — accept pointer-like via Config meta? use Events non-nil as signal)
	// Callers pass full Enabled intentionally.
	cur.Enabled = patch.Enabled
	if patch.Events != nil {
		cur.Events = patch.Events
	}
	if patch.Config != nil {
		if cur.Config == nil {
			cur.Config = map[string]string{}
		}
		for k, v := range patch.Config {
			// skip masked placeholders so redact list round-trips don't wipe secrets
			if isMasked(v) {
				continue
			}
			if strings.TrimSpace(v) == "" && isSecretKey(k) {
				continue
			}
			cur.Config[k] = v
		}
	}
	if err := validateKind(cur.Kind, cur.Config); err != nil {
		return Channel{}, err
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	snap.Channels[idx] = cur
	if err := s.save(snap); err != nil {
		return Channel{}, err
	}
	return maskChannel(cur), nil
}

// Delete removes a channel.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return err
	}
	out := make([]Channel, 0, len(snap.Channels))
	found := false
	for _, c := range snap.Channels {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return fmt.Errorf("channel not found: %s", id)
	}
	snap.Channels = out
	return s.save(snap)
}

// Test sends a probe message through the channel.
func (s *Store) Test(id string) error {
	c, err := s.Get(id, false)
	if err != nil {
		return err
	}
	return Send(c, "system.test", "touch-squirrel 通知测试", "这是一条测试消息 · "+time.Now().Format(time.RFC3339))
}

// Send dispatches title/body to a channel.
func Send(c Channel, event, title, body string) error {
	if !c.Enabled && event != "system.test" {
		return fmt.Errorf("channel disabled")
	}
	switch strings.ToLower(c.Kind) {
	case KindFeishu:
		return sendFeishu(c.Config, title, body)
	case KindSMTP:
		return sendSMTP(c.Config, title, body)
	case KindWebhook:
		return sendWebhook(c.Config, event, title, body)
	default:
		return fmt.Errorf("unsupported kind: %s", c.Kind)
	}
}

func validateKind(kind string, cfg map[string]string) error {
	if cfg == nil {
		cfg = map[string]string{}
	}
	switch kind {
	case KindFeishu:
		if strings.TrimSpace(cfg["webhook_url"]) == "" {
			return fmt.Errorf("feishu: webhook_url required")
		}
	case KindSMTP:
		if strings.TrimSpace(cfg["host"]) == "" {
			return fmt.Errorf("smtp: host required")
		}
		if strings.TrimSpace(cfg["from"]) == "" {
			return fmt.Errorf("smtp: from required")
		}
		if strings.TrimSpace(cfg["to"]) == "" {
			return fmt.Errorf("smtp: to required")
		}
	case KindWebhook:
		if strings.TrimSpace(cfg["url"]) == "" {
			return fmt.Errorf("webhook: url required")
		}
	default:
		return fmt.Errorf("kind must be feishu|smtp|webhook")
	}
	return nil
}

func isSecretKey(k string) bool {
	switch k {
	case "password", "secret", "bot_secret", "smtp_password", "token":
		return true
	default:
		return false
	}
}

func isMasked(v string) bool {
	return v == "••••" || strings.HasPrefix(v, "****") || v == "•••masked•••"
}

func maskChannel(c Channel) Channel {
	cp := c
	if cp.Config == nil {
		cp.Config = map[string]string{}
		return cp
	}
	m := map[string]string{}
	for k, v := range cp.Config {
		if isSecretKey(k) && strings.TrimSpace(v) != "" {
			m[k] = "••••"
		} else {
			m[k] = v
		}
	}
	cp.Config = m
	return cp
}

func sendFeishu(cfg map[string]string, title, body string) error {
	url := strings.TrimSpace(cfg["webhook_url"])
	if url == "" {
		return fmt.Errorf("missing webhook_url")
	}
	// optional signed bot: secret + timestamp
	secret := strings.TrimSpace(cfg["secret"])
	payload := map[string]any{
		"msg_type": "text",
		"content": map[string]string{
			"text": title + "\n" + body,
		},
	}
	if secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		stringToSign := ts + "\n" + secret
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		payload["timestamp"] = ts
		payload["sign"] = sign
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 12 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	if res.StatusCode >= 300 {
		return fmt.Errorf("feishu http %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	// Feishu often returns 200 with code != 0
	var fr struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if json.Unmarshal(raw, &fr) == nil && fr.Code != 0 {
		return fmt.Errorf("feishu code %d: %s", fr.Code, fr.Msg)
	}
	return nil
}

func sendSMTP(cfg map[string]string, title, body string) error {
	host := strings.TrimSpace(cfg["host"])
	port := strings.TrimSpace(cfg["port"])
	if port == "" {
		port = "587"
	}
	user := strings.TrimSpace(cfg["user"])
	pass := cfg["password"]
	from := strings.TrimSpace(cfg["from"])
	toRaw := strings.TrimSpace(cfg["to"])
	if host == "" || from == "" || toRaw == "" {
		return fmt.Errorf("smtp incomplete")
	}
	if strings.ContainsAny(title, "\r\n") || strings.ContainsAny(from, "\r\n") || strings.ContainsAny(toRaw, "\r\n") {
		return fmt.Errorf("smtp headers must not contain newlines")
	}
	recipients := splitCSV(toRaw)
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("invalid from address")
	}
	for _, recipient := range recipients {
		parsed, err := mail.ParseAddress(recipient)
		if err != nil || parsed.Address != recipient {
			return fmt.Errorf("invalid recipient address")
		}
	}
	addr := net.JoinHostPort(host, port)
	msg := []byte("From: " + from + "\r\n" +
		"To: " + strings.Join(recipients, ", ") + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("UTF-8", title) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body + "\r\n")

	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	return smtp.SendMail(addr, auth, from, recipients, msg)
}

func sendWebhook(cfg map[string]string, event, title, body string) error {
	url := strings.TrimSpace(cfg["url"])
	if url == "" {
		return fmt.Errorf("missing url")
	}
	payload := map[string]any{
		"event":     event,
		"title":     title,
		"body":      body,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if t := strings.TrimSpace(cfg["token"]); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	client := &http.Client{Timeout: 12 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	if res.StatusCode >= 300 {
		return fmt.Errorf("webhook http %d: %s", res.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func splitCSV(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == ' '
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// DefaultPath under squirrel home root.
func DefaultPath(homeRoot string) string {
	return filepath.Join(homeRoot, "notifications.json")
}
