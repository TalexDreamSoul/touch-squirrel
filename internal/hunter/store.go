package hunter

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store { return &Store{path: path} }
func (s *Store) Path() string     { return s.path }

func (s *Store) load() (Snapshot, error) {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{Version: 3, Config: DefaultConfig(), Findings: []Finding{}, Drafts: []Draft{}, Audit: []Audit{}}, nil
		}
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return Snapshot{}, fmt.Errorf("hunter store: %w", err)
	}
	var presence struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	_ = json.Unmarshal(b, &presence)
	if snap.Version < 3 {
		defaults := DefaultConfig()
		if _, ok := presence.Config["isolated_network"]; !ok {
			snap.Config.IsolatedNetwork = defaults.IsolatedNetwork
		}
		if _, ok := presence.Config["auto_discover_network"]; !ok {
			snap.Config.AutoDiscoverNetwork = defaults.AutoDiscoverNetwork
		}
		if _, ok := presence.Config["credential_audit_enabled"]; !ok {
			snap.Config.CredentialAuditEnabled = defaults.CredentialAuditEnabled
		}
		snap.Version = 3
	}
	snap.Config = normalizeConfig(snap.Config)
	return snap, nil
}

func (s *Store) save(snap Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	snap.Version = 3
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Config(redact bool) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Config{}, err
	}
	cfg := snap.Config
	if redact {
		if cfg.FOFAKey != "" {
			cfg.FOFAKey = MaskedSecret
		}
		if cfg.ShodanKey != "" {
			cfg.ShodanKey = MaskedSecret
		}
	}
	return cfg, nil
}

func (s *Store) SaveConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return err
	}
	if cfg.FOFAKey == MaskedSecret {
		cfg.FOFAKey = snap.Config.FOFAKey
	}
	if cfg.ShodanKey == MaskedSecret {
		cfg.ShodanKey = snap.Config.ShodanKey
	}
	cfg.Scopes = cleanStrings(cfg.Scopes)
	cfg.FOFAQueries = cleanStrings(cfg.FOFAQueries)
	cfg.ShodanQueries = cleanStrings(cfg.ShodanQueries)
	cfg.DiscoveryCIDRs = cleanStrings(cfg.DiscoveryCIDRs)
	if _, err := ParseScope(cfg.Scopes); err != nil {
		return err
	}
	for _, raw := range cfg.DiscoveryCIDRs {
		if _, err := netip.ParsePrefix(raw); err != nil {
			if _, addrErr := netip.ParseAddr(raw); addrErr != nil {
				return fmt.Errorf("invalid discovery CIDR %q", raw)
			}
		}
	}
	for _, port := range cfg.DiscoveryPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid discovery port %d", port)
		}
	}
	cfg.DiscoveryPorts = normalizePorts(cfg.DiscoveryPorts)
	cfg = normalizeConfig(cfg)
	if cfg.MaxResults <= 0 || cfg.MaxResults > 1000 {
		return fmt.Errorf("max_results must be between 1 and 1000")
	}
	if cfg.RatePerMinute <= 0 || cfg.RatePerMinute > 600 {
		return fmt.Errorf("rate_per_minute must be between 1 and 600")
	}
	if cfg.DiscoveryConcurrency < 1 || cfg.DiscoveryConcurrency > 256 {
		return fmt.Errorf("discovery_concurrency must be between 1 and 256")
	}
	if cfg.DiscoveryTimeoutMS < 100 || cfg.DiscoveryTimeoutMS > 10000 {
		return fmt.Errorf("discovery_timeout_ms must be between 100 and 10000")
	}
	if cfg.MaxDiscoveryHosts < 1 || cfg.MaxDiscoveryHosts > 65536 {
		return fmt.Errorf("max_discovery_hosts must be between 1 and 65536")
	}
	snap.Config = cfg
	snap.Audit = appendAudit(snap.Audit, "config.updated", "", "scope and provider settings updated")
	return s.save(snap)
}

func (s *Store) Snapshot(redact bool) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Snapshot{}, err
	}
	sanitized := false
	for i := range snap.Findings {
		clean := sanitizeFinding(snap.Findings[i])
		if !reflect.DeepEqual(clean, snap.Findings[i]) {
			snap.Findings[i] = clean
			sanitized = true
		}
	}
	if sanitized {
		if err := s.save(snap); err != nil {
			return Snapshot{}, err
		}
	}
	if redact {
		if snap.Config.FOFAKey != "" {
			snap.Config.FOFAKey = MaskedSecret
		}
		if snap.Config.ShodanKey != "" {
			snap.Config.ShodanKey = MaskedSecret
		}
	}
	sort.Slice(snap.Findings, func(i, j int) bool { return snap.Findings[i].UpdatedAt > snap.Findings[j].UpdatedAt })
	sort.Slice(snap.Drafts, func(i, j int) bool { return snap.Drafts[i].CreatedAt > snap.Drafts[j].CreatedAt })
	return snap, nil
}

func (s *Store) UpsertFinding(in Finding) (Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in = sanitizeFinding(in)
	snap, err := s.load()
	if err != nil {
		return Finding{}, err
	}
	now := nowRFC3339()
	if in.ID == "" {
		in.ID = stableID("fnd", in.URL)
	}
	if in.Status == "" {
		in.Status = FindingNew
	}
	if in.ObservedAt == "" {
		in.ObservedAt = now
	}
	in.UpdatedAt = now
	for i := range snap.Findings {
		if snap.Findings[i].ID == in.ID {
			previous := snap.Findings[i]
			in.ObservedAt = previous.ObservedAt
			in.Metadata = mergeMetadata(previous.Metadata, in.Metadata)
			if in.Status == FindingNew && previous.Status != FindingNew {
				in.Status = previous.Status
			}
			if in.ProbedAt == "" {
				in.ProbedAt = previous.ProbedAt
				in.HTTPStatus = previous.HTTPStatus
				in.Evidence = mergeEvidence(previous.Evidence, in.Evidence)
			}
			snap.Findings[i] = in
			snap.Audit = appendAudit(snap.Audit, "finding.updated", in.ID, in.URL)
			return in, s.save(snap)
		}
	}
	snap.Findings = append(snap.Findings, in)
	snap.Audit = appendAudit(snap.Audit, "finding.created", in.ID, in.URL)
	return in, s.save(snap)
}

func (s *Store) Finding(id string) (Finding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Finding{}, err
	}
	for _, f := range snap.Findings {
		if f.ID == id {
			return f, nil
		}
	}
	return Finding{}, fmt.Errorf("finding not found: %s", id)
}

func (s *Store) SetFindingStatus(id, status string) (Finding, error) {
	if status != FindingNew && status != FindingConfirmed && status != FindingDismissed {
		return Finding{}, fmt.Errorf("invalid finding status")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Finding{}, err
	}
	for i := range snap.Findings {
		if snap.Findings[i].ID == id {
			snap.Findings[i].Status = status
			snap.Findings[i].UpdatedAt = nowRFC3339()
			snap.Audit = appendAudit(snap.Audit, "finding.status", id, status)
			return snap.Findings[i], s.save(snap)
		}
	}
	return Finding{}, fmt.Errorf("finding not found: %s", id)
}

func (s *Store) CreateDraft(in Draft) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Draft{}, err
	}
	if strings.TrimSpace(in.FindingID) == "" || strings.TrimSpace(in.ChannelID) == "" || strings.TrimSpace(in.To) == "" || strings.TrimSpace(in.Subject) == "" || strings.TrimSpace(in.Body) == "" {
		return Draft{}, fmt.Errorf("finding_id, channel_id, to, subject and body are required")
	}
	if strings.ContainsAny(in.To+in.Subject, "\r\n") {
		return Draft{}, fmt.Errorf("mail headers must not contain newlines")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(in.To))
	if err != nil || address.Address != strings.TrimSpace(in.To) {
		return Draft{}, fmt.Errorf("invalid recipient address")
	}
	in.To = address.Address
	in.Subject = RedactText(strings.TrimSpace(in.Subject))
	in.Body = RedactText(in.Body)
	confirmed := false
	for _, f := range snap.Findings {
		if f.ID == in.FindingID && f.Status == FindingConfirmed {
			confirmed = true
			break
		}
	}
	if !confirmed {
		return Draft{}, fmt.Errorf("finding must be confirmed before drafting")
	}
	in.ID = fmt.Sprintf("drf_%d", time.Now().UnixNano())
	in.Status = DraftPending
	in.CreatedAt = nowRFC3339()
	snap.Drafts = append(snap.Drafts, in)
	snap.Audit = appendAudit(snap.Audit, "draft.created", in.ID, in.FindingID)
	return in, s.save(snap)
}

func (s *Store) Draft(id string) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Draft{}, err
	}
	for _, d := range snap.Drafts {
		if d.ID == id {
			return d, nil
		}
	}
	return Draft{}, fmt.Errorf("draft not found: %s", id)
}

func (s *Store) ApproveDraft(id, operator string) (Draft, error) {
	if strings.TrimSpace(operator) == "" {
		return Draft{}, fmt.Errorf("operator required")
	}
	return s.transitionDraft(id, DraftPending, DraftApproved, func(d *Draft) {
		d.ApprovedAt = nowRFC3339()
		d.ApprovedBy = strings.TrimSpace(operator)
	})
}

func (s *Store) BeginSend(id string) (Draft, error) {
	return s.transitionDraft(id, DraftApproved, DraftSending, func(d *Draft) { d.SendError = "" })
}

func (s *Store) MarkSent(id string) (Draft, error) {
	return s.transitionDraft(id, DraftSending, DraftSent, func(d *Draft) { d.SentAt = nowRFC3339() })
}

func (s *Store) MarkSendFailed(id, message string) (Draft, error) {
	return s.transitionDraft(id, DraftSending, DraftApproved, func(d *Draft) { d.SendError = limitText(message, 500) })
}

func (s *Store) transitionDraft(id, from, to string, apply func(*Draft)) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return Draft{}, err
	}
	for i := range snap.Drafts {
		if snap.Drafts[i].ID != id {
			continue
		}
		if snap.Drafts[i].Status != from {
			return Draft{}, fmt.Errorf("draft status must be %s", from)
		}
		apply(&snap.Drafts[i])
		snap.Drafts[i].Status = to
		snap.Audit = appendAudit(snap.Audit, "draft."+to, id, "")
		return snap.Drafts[i], s.save(snap)
	}
	return Draft{}, fmt.Errorf("draft not found: %s", id)
}

func mergeMetadata(previous, next map[string]string) map[string]string {
	if len(previous) == 0 && len(next) == 0 {
		return nil
	}
	out := make(map[string]string, len(previous)+len(next))
	for key, value := range previous {
		out[key] = value
	}
	for key, value := range next {
		out[key] = value
	}
	return out
}

func sanitizeFinding(in Finding) Finding {
	in.Title = RedactText(limitText(in.Title, 256))
	in.Banner = RedactText(limitText(in.Banner, 512))
	in.Query = RedactText(limitText(in.Query, 512))
	in.Metadata = sanitizeMetadata(in.Metadata)
	for i := range in.Evidence {
		in.Evidence[i].Redacted = RedactText(limitText(in.Evidence[i].Redacted, 128))
	}
	return in
}

func appendAudit(items []Audit, action, targetID, detail string) []Audit {
	items = append(items, Audit{ID: fmt.Sprintf("aud_%d", time.Now().UnixNano()), Action: action, TargetID: targetID, Detail: detail, CreatedAt: nowRFC3339()})
	if len(items) > 2000 {
		items = items[len(items)-2000:]
	}
	return items
}

func normalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if len(cfg.DiscoveryPorts) == 0 {
		cfg.DiscoveryPorts = append([]int(nil), defaults.DiscoveryPorts...)
	}
	if cfg.DiscoveryConcurrency <= 0 {
		cfg.DiscoveryConcurrency = defaults.DiscoveryConcurrency
	}
	if cfg.DiscoveryTimeoutMS <= 0 {
		cfg.DiscoveryTimeoutMS = defaults.DiscoveryTimeoutMS
	}
	if cfg.MaxDiscoveryHosts <= 0 {
		cfg.MaxDiscoveryHosts = defaults.MaxDiscoveryHosts
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = defaults.MaxResults
	}
	if cfg.RatePerMinute <= 0 {
		cfg.RatePerMinute = defaults.RatePerMinute
	}
	return cfg
}

func cleanStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
