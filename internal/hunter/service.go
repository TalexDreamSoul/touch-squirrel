package hunter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DiscoverReport struct {
	Imported int      `json:"imported"`
	Errors   []string `json:"errors,omitempty"`
}

type Service struct {
	Store  *Store
	FOFA   PassiveProvider
	Shodan PassiveProvider
	Prober *Prober

	probeMu   sync.Mutex
	lastProbe time.Time
}

func NewService(path string) *Service {
	return &Service{
		Store:  NewStore(path),
		FOFA:   FOFAClient{},
		Shodan: ShodanClient{},
		Prober: NewProber(nil),
	}
}

func (s *Service) Discover(ctx context.Context, sources []string) (DiscoverReport, error) {
	cfg, err := s.Store.Config(false)
	if err != nil {
		return DiscoverReport{}, err
	}
	requested := map[string]bool{}
	for _, source := range sources {
		requested[strings.ToLower(strings.TrimSpace(source))] = true
	}
	if len(requested) == 0 {
		requested["fofa"] = true
		requested["shodan"] = true
	}
	fofaEmail := firstNonEmpty(os.Getenv("FOFA_EMAIL"), cfg.FOFAEmail)
	fofaKey := firstNonEmpty(os.Getenv("FOFA_API_KEY"), cfg.FOFAKey)
	shodanKey := firstNonEmpty(os.Getenv("SHODAN_API_KEY"), cfg.ShodanKey)
	report := DiscoverReport{}
	if requested["fofa"] {
		if fofaEmail == "" || fofaKey == "" {
			report.Errors = append(report.Errors, "FOFA credentials are not configured")
		} else {
			for _, query := range cfg.FOFAQueries {
				items, err := s.FOFA.Search(ctx, ProviderRequest{Email: fofaEmail, Key: fofaKey, Query: query, Limit: cfg.MaxResults})
				if err != nil {
					report.Errors = append(report.Errors, "FOFA: "+err.Error())
					continue
				}
				n, err := s.Import(items)
				report.Imported += n
				if err != nil {
					report.Errors = append(report.Errors, "FOFA import: "+err.Error())
				}
			}
		}
	}
	if requested["shodan"] {
		if shodanKey == "" {
			report.Errors = append(report.Errors, "Shodan credentials are not configured")
		} else {
			for _, query := range cfg.ShodanQueries {
				items, err := s.Shodan.Search(ctx, ProviderRequest{Key: shodanKey, Query: query, Limit: cfg.MaxResults})
				if err != nil {
					report.Errors = append(report.Errors, "Shodan: "+err.Error())
					continue
				}
				n, err := s.Import(items)
				report.Imported += n
				if err != nil {
					report.Errors = append(report.Errors, "Shodan import: "+err.Error())
				}
			}
		}
	}
	_ = s.Store.AddAudit("discovery.completed", "", fmt.Sprintf("imported=%d errors=%d", report.Imported, len(report.Errors)))
	return report, nil
}

func (s *Service) Import(items []Candidate) (int, error) {
	cfg, err := s.Store.Config(false)
	if err != nil {
		return 0, err
	}
	scope, err := ParseScope(effectiveScopeEntries(cfg))
	if err != nil {
		return 0, err
	}
	imported := 0
	for _, item := range items {
		u, err := url.Parse(strings.TrimSpace(item.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
			continue
		}
		u.Path, u.RawPath, u.RawQuery, u.Fragment = "", "", "", ""
		host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
		ip, _ := netip.ParseAddr(strings.TrimSpace(item.IP))
		banner := limitText(item.Banner, 512)
		evidence := mergeEvidence(DetectEvidence([]byte(banner)), item.Evidence)
		product := item.Product
		if product == "" || product == "unknown" {
			product = ClassifyProduct(item.Title + " " + banner)
		}
		_, err = s.Store.UpsertFinding(Finding{
			URL:        strings.TrimRight(u.String(), "/"),
			Host:       host,
			Source:     firstNonEmpty(item.Source, "local"),
			Query:      item.Query,
			Product:    product,
			Title:      RedactText(limitText(item.Title, 256)),
			Banner:     RedactText(banner),
			Status:     FindingNew,
			InScope:    scope.Allows(host, ip),
			Evidence:   evidence,
			Metadata:   sanitizeMetadata(item.Metadata),
			ObservedAt: nowRFC3339(),
		})
		if err != nil {
			return imported, err
		}
		imported++
	}
	return imported, nil
}

func (s *Service) ImportCSV(r io.Reader) (int, error) {
	cr := csv.NewReader(io.LimitReader(r, 4<<20))
	cr.TrimLeadingSpace = true
	records, err := cr.ReadAll()
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	head := map[string]int{}
	for i, v := range records[0] {
		head[strings.ToLower(strings.TrimSpace(v))] = i
	}
	if _, ok := head["url"]; !ok {
		return 0, fmt.Errorf("CSV requires a url column")
	}
	items := make([]Candidate, 0, len(records)-1)
	for _, row := range records[1:] {
		items = append(items, Candidate{
			URL:     csvValue(row, headerIndex(head, "url")),
			IP:      csvValue(row, headerIndex(head, "ip")),
			Product: csvValue(row, headerIndex(head, "product")),
			Title:   csvValue(row, headerIndex(head, "title")),
			Banner:  csvValue(row, headerIndex(head, "banner")),
			Source:  "local",
		})
	}
	return s.Import(items)
}

func (s *Service) ProbeFinding(ctx context.Context, id string) (Finding, error) {
	cfg, err := s.Store.Config(false)
	if err != nil {
		return Finding{}, err
	}
	if !cfg.ProbeEnabled {
		return Finding{}, fmt.Errorf("active probing is disabled")
	}
	finding, err := s.Store.Finding(id)
	if err != nil {
		return Finding{}, err
	}
	if !finding.InScope {
		_ = s.Store.AddAudit("probe.blocked", id, "finding is outside configured scope")
		return Finding{}, fmt.Errorf("finding is outside configured scope")
	}
	interval := time.Minute / time.Duration(max(cfg.RatePerMinute, 1))
	s.probeMu.Lock()
	wait := interval - time.Since(s.lastProbe)
	if !s.lastProbe.IsZero() && wait > 0 {
		s.probeMu.Unlock()
		return Finding{}, fmt.Errorf("probe rate limit: retry in %s", wait.Round(time.Second))
	}
	s.lastProbe = time.Now()
	s.probeMu.Unlock()

	scope, err := ParseScope(effectiveScopeEntries(cfg))
	if err != nil {
		return Finding{}, err
	}
	result, err := s.Prober.Probe(ctx, finding.URL, finding.Product, scope, cfg.IsolatedNetwork)
	if err != nil {
		_ = s.Store.AddAudit("probe.failed", id, limitText(err.Error(), 500))
		return Finding{}, err
	}
	finding.HTTPStatus = result.HTTPStatus
	finding.Product = result.Product
	finding.ProbedAt = result.ProbedAt
	finding.Evidence = mergeEvidence(finding.Evidence, result.Evidence)
	if cfg.CredentialAuditEnabled && result.Product == "sub2api" && credentialAuditDue(finding, time.Now()) {
		if finding.Metadata == nil {
			finding.Metadata = map[string]string{}
		}
		finding.Metadata["credential_audited_at"] = nowRFC3339()
		credentialEvidence, auditErr := s.Prober.AuditDefaultCredentials(ctx, finding.URL, result.Product, scope, cfg.IsolatedNetwork)
		if auditErr != nil {
			_ = s.Store.AddAudit("credential.audit.failed", id, limitText(auditErr.Error(), 500))
		} else {
			finding.Evidence = mergeEvidence(finding.Evidence, credentialEvidence)
		}
	}
	finding, err = s.Store.UpsertFinding(finding)
	if err == nil {
		_ = s.Store.AddAudit("probe.completed", id, "fixed read-only checks completed")
	}
	return finding, err
}

func credentialAuditDue(finding Finding, now time.Time) bool {
	for _, evidence := range finding.Evidence {
		if evidence.Kind == "default_credential" {
			return false
		}
	}
	raw := finding.Metadata["credential_audited_at"]
	if raw == "" {
		return true
	}
	last, err := time.Parse(time.RFC3339, raw)
	return err != nil || now.Sub(last) >= 24*time.Hour
}

func (s *Store) AddAudit(action, targetID, detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, err := s.load()
	if err != nil {
		return err
	}
	snap.Audit = appendAudit(snap.Audit, action, targetID, detail)
	return s.save(snap)
}

func headerIndex(head map[string]int, key string) int {
	if idx, ok := head[key]; ok {
		return idx
	}
	return -1
}

func csvValue(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func mergeEvidence(a, b []Evidence) []Evidence {
	seen := map[string]bool{}
	out := make([]Evidence, 0, len(a)+len(b))
	for _, item := range append(a, b...) {
		key := item.Kind + ":" + item.Fingerprint
		if !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func ParseLocalJSON(items []map[string]interface{}) []Candidate {
	out := make([]Candidate, 0, len(items))
	for _, item := range items {
		out = append(out, Candidate{
			URL:     interfaceString(item["url"]),
			IP:      interfaceString(item["ip"]),
			Product: interfaceString(item["product"]),
			Title:   interfaceString(item["title"]),
			Banner:  interfaceString(item["banner"]),
			Source:  "local",
			Metadata: map[string]string{
				"port": strconv.Itoa(valueInt(item["port"])),
			},
		})
	}
	return out
}

func interfaceString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func valueInt(v interface{}) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	default:
		return 0
	}
}
