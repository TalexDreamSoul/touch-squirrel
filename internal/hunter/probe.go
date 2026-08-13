package hunter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxProbeBody = 256 << 10

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type netResolver struct{ r *net.Resolver }

func (r netResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return r.r.LookupNetIP(ctx, network, host)
}

type validatedTarget struct {
	URL  *url.URL
	Host string
	IPs  []netip.Addr
}

type ProbeResult struct {
	HTTPStatus int
	Product    string
	Evidence   []Evidence
	ProbedAt   string
}

type Prober struct {
	resolver Resolver
	timeout  time.Duration
}

func NewProber(resolver Resolver) *Prober {
	if resolver == nil {
		resolver = netResolver{r: net.DefaultResolver}
	}
	return &Prober{resolver: resolver, timeout: 12 * time.Second}
}

func ProbePaths(product string) []string {
	switch product {
	case "sub2api":
		return []string{"/health", "/setup/status", "/"}
	case "cliproxyapi":
		return []string{"/healthz", "/", "/management.html", "/v0/management/config", "/v1/models"}
	case "openai-compatible", "litellm", "new-api":
		return []string{"/", "/v1/models"}
	case "ollama":
		return []string{"/", "/api/tags"}
	case "dify":
		return []string{"/", "/health"}
	default:
		return []string{"/", "/health", "/healthz", "/setup/status", "/management.html", "/v0/management/config"}
	}
}

func (p *Prober) validateTarget(ctx context.Context, raw string, scope Scope, allowPrivate bool) (validatedTarget, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return validatedTarget{}, fmt.Errorf("target must be an absolute http(s) URL")
	}
	if u.User != nil {
		return validatedTarget{}, fmt.Errorf("target userinfo is not allowed")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = p.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return validatedTarget{}, fmt.Errorf("resolve %s: %w", host, err)
		}
	}
	var allowed []netip.Addr
	for _, ip := range ips {
		if addressAllowed(ip, allowPrivate) && scope.Allows(host, ip) {
			allowed = append(allowed, ip.Unmap())
		}
	}
	if len(allowed) == 0 {
		return validatedTarget{}, fmt.Errorf("target is outside configured network scope")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return validatedTarget{}, fmt.Errorf("invalid target port")
		}
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = "", "", "", ""
	return validatedTarget{URL: u, Host: host, IPs: allowed}, nil
}

func addressAllowed(ip netip.Addr, allowPrivate bool) bool {
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if allowPrivate {
		return true
	}
	return IsPublicAddress(ip)
}

func (p *Prober) Probe(ctx context.Context, raw, product string, scope Scope, allowPrivate bool) (ProbeResult, error) {
	initial, err := p.validateTarget(ctx, raw, scope, allowPrivate)
	if err != nil {
		return ProbeResult{}, err
	}
	paths := ProbePaths(product)
	ctx, cancel := context.WithTimeout(ctx, p.timeout*time.Duration(len(paths)+1))
	defer cancel()
	client := p.newPinnedClient(initial, scope, allowPrivate)

	result := ProbeResult{Product: product, ProbedAt: nowRFC3339()}
	seenEvidence := map[string]bool{}
	responses := 0
	var lastRequestErr error
	for _, path := range paths {
		u := *initial.URL
		u.Path = path
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return ProbeResult{}, err
		}
		req.Header.Set("User-Agent", "touch-squirrel-isolated-audit/1.0")
		req.Header.Set("Accept", "application/json,text/html,text/plain;q=0.8")
		res, err := client.Do(req)
		if err != nil {
			lastRequestErr = err
			continue
		}
		responses++
		body, readErr := io.ReadAll(io.LimitReader(res.Body, maxProbeBody+1))
		_ = res.Body.Close()
		if readErr != nil {
			return ProbeResult{}, readErr
		}
		if len(body) > maxProbeBody {
			body = body[:maxProbeBody]
		}
		result.HTTPStatus = res.StatusCode
		fingerprintParts := []string{string(body)}
		if value := res.Header.Get("X-CPA-Version"); value != "" {
			fingerprintParts = append(fingerprintParts, "x-cpa-version:"+value)
		}
		if value := res.Header.Get("Server"); value != "" {
			fingerprintParts = append(fingerprintParts, "server:"+value)
		}
		detected := ClassifyProduct(strings.Join(fingerprintParts, "\n"))
		if result.Product == "" || result.Product == "unknown" {
			result.Product = detected
		}
		for _, ev := range DetectEvidence(body) {
			appendUniqueEvidence(&result.Evidence, seenEvidence, ev)
		}
		if path == "/v0/management/config" && res.StatusCode >= 200 && res.StatusCode < 300 {
			appendUniqueEvidence(&result.Evidence, seenEvidence, pathEvidence("unauthenticated_management", raw, path))
		}
		if path == "/setup/status" && res.StatusCode == http.StatusOK && bytes.Contains(body, []byte(`"needs_setup":true`)) {
			appendUniqueEvidence(&result.Evidence, seenEvidence, pathEvidence("setup_wizard_exposed", raw, path))
		}
	}
	if responses == 0 && lastRequestErr != nil {
		return ProbeResult{}, lastRequestErr
	}
	return result, nil
}

func (p *Prober) AuditDefaultCredentials(ctx context.Context, raw, product string, scope Scope, allowPrivate bool) ([]Evidence, error) {
	if product != "sub2api" {
		return nil, nil
	}
	initial, err := p.validateTarget(ctx, raw, scope, allowPrivate)
	if err != nil {
		return nil, err
	}
	client := p.newPinnedClient(initial, scope, allowPrivate)
	pairs := []struct {
		email    string
		password string
	}{
		{email: "admin@sub2api.local", password: "admin123"},
		{email: "admin@example.com", password: "admin123"},
	}
	for _, pair := range pairs {
		payload, _ := json.Marshal(map[string]string{"email": pair.email, "password": pair.password})
		u := *initial.URL
		u.Path = "/api/v1/auth/login"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "touch-squirrel-isolated-audit/1.0")
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
		_ = res.Body.Close()
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return []Evidence{{Kind: "default_credential", Fingerprint: fingerprint(pair.email + ":" + pair.password), Redacted: pair.email + ":****"}}, nil
		}
		if res.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("credential audit stopped by target rate limit")
		}
	}
	return nil, nil
}

func (p *Prober) newPinnedClient(initial validatedTarget, scope Scope, allowPrivate bool) *http.Client {
	pinned := map[string][]netip.Addr{initial.Host: initial.IPs}
	var pinMu sync.RWMutex
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    true,
		DisableKeepAlives:     true,
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: p.timeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		pinMu.RLock()
		ips := append([]netip.Addr(nil), pinned[strings.ToLower(strings.TrimSuffix(host, "."))]...)
		pinMu.RUnlock()
		if len(ips) == 0 {
			return nil, fmt.Errorf("unvalidated dial host")
		}
		var last error
		d := net.Dialer{Timeout: min(p.timeout, 5*time.Second)}
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
	client := &http.Client{Transport: transport, Timeout: p.timeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		v, err := p.validateTarget(req.Context(), req.URL.String(), scope, allowPrivate)
		if err != nil {
			return err
		}
		pinMu.Lock()
		pinned[v.Host] = v.IPs
		pinMu.Unlock()
		return nil
	}
	return client
}

func appendUniqueEvidence(items *[]Evidence, seen map[string]bool, item Evidence) {
	key := item.Kind + ":" + item.Fingerprint
	if seen[key] {
		return
	}
	seen[key] = true
	*items = append(*items, item)
}

func pathEvidence(kind, raw, path string) Evidence {
	return Evidence{Kind: kind, Fingerprint: fingerprint(raw + path), Redacted: path}
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
