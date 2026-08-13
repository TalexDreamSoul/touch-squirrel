package hunter

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type NetworkDiscoverReport struct {
	Networks     []string `json:"networks"`
	ScannedHosts int      `json:"scanned_hosts"`
	ScannedPorts int      `json:"scanned_ports"`
	OpenPorts    int      `json:"open_ports"`
	Imported     int      `json:"imported"`
	Errors       []string `json:"errors,omitempty"`
}

type networkTask struct {
	IP   netip.Addr
	Port int
}

func (s *Service) DiscoverNetwork(ctx context.Context) (NetworkDiscoverReport, error) {
	cfg, err := s.Store.Config(false)
	if err != nil {
		return NetworkDiscoverReport{}, err
	}
	if !cfg.IsolatedNetwork {
		return NetworkDiscoverReport{}, fmt.Errorf("isolated network mode is disabled")
	}
	networks := discoveryNetworks(cfg)
	hosts, err := enumerateNetworkHosts(networks, cfg.MaxDiscoveryHosts)
	if err != nil {
		return NetworkDiscoverReport{}, err
	}
	ports := normalizePorts(cfg.DiscoveryPorts)
	if len(ports) == 0 {
		ports = append([]int(nil), DefaultConfig().DiscoveryPorts...)
	}
	report := NetworkDiscoverReport{Networks: networks, ScannedHosts: len(hosts), ScannedPorts: len(hosts) * len(ports)}
	if len(hosts) == 0 {
		return report, nil
	}
	scope, err := ParseScope(effectiveScopeEntries(cfg))
	if err != nil {
		return report, err
	}
	concurrency := cfg.DiscoveryConcurrency
	if concurrency <= 0 {
		concurrency = 64
	}
	if concurrency > 256 {
		concurrency = 256
	}
	timeout := time.Duration(cfg.DiscoveryTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	recentlyAudited := map[string]bool{}
	if snap, snapErr := s.Store.Snapshot(true); snapErr == nil {
		for _, finding := range snap.Findings {
			if !credentialAuditDue(finding, time.Now()) {
				recentlyAudited[finding.URL] = true
			}
		}
	}

	tasks := make(chan networkTask)
	found := make(chan Candidate)
	var openPorts atomic.Int64
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dialer := net.Dialer{Timeout: timeout}
			probe := *s.Prober
			probe.timeout = max(2*time.Second, timeout*3)
			for task := range tasks {
				address := net.JoinHostPort(task.IP.String(), strconv.Itoa(task.Port))
				conn, err := dialer.DialContext(ctx, "tcp", address)
				if err != nil {
					continue
				}
				_ = conn.Close()
				openPorts.Add(1)
				candidate, ok := probeDiscoveredEndpoint(ctx, &probe, task, scope, cfg.CredentialAuditEnabled, recentlyAudited)
				if ok {
					select {
					case found <- candidate:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(tasks)
		for _, host := range hosts {
			for _, port := range ports {
				select {
				case tasks <- networkTask{IP: host, Port: port}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	go func() {
		wg.Wait()
		close(found)
	}()

	var candidates []Candidate
	for candidate := range found {
		candidates = append(candidates, candidate)
	}
	report.OpenPorts = int(openPorts.Load())
	if ctx.Err() != nil {
		report.Errors = append(report.Errors, ctx.Err().Error())
	}
	report.Imported, err = s.Import(candidates)
	if err != nil {
		return report, err
	}
	_ = s.Store.AddAudit("network.discovery.completed", "", fmt.Sprintf("networks=%d hosts=%d open=%d imported=%d", len(networks), report.ScannedHosts, report.OpenPorts, report.Imported))
	return report, nil
}

func probeDiscoveredEndpoint(ctx context.Context, probe *Prober, task networkTask, scope Scope, auditCredentials bool, recentlyAudited map[string]bool) (Candidate, bool) {
	schemes := []string{"http", "https"}
	if task.Port == 443 || task.Port == 8443 {
		schemes[0], schemes[1] = schemes[1], schemes[0]
	}
	for _, scheme := range schemes {
		raw := scheme + "://" + formatURLHost(task.IP.String(), task.Port, scheme)
		result, err := probe.Probe(ctx, raw, "unknown", scope, true)
		if err != nil {
			continue
		}
		credentialEvidence := []Evidence(nil)
		metadata := map[string]string{"port": strconv.Itoa(task.Port), "scheme": scheme}
		if auditCredentials && result.Product == "sub2api" && !recentlyAudited[raw] {
			metadata["credential_audited_at"] = nowRFC3339()
			credentialEvidence, _ = probe.AuditDefaultCredentials(ctx, raw, result.Product, scope, true)
		}
		return Candidate{
			URL:      raw,
			Host:     task.IP.String(),
			IP:       task.IP.String(),
			Source:   "network",
			Product:  firstNonEmpty(result.Product, "unknown"),
			Evidence: mergeEvidence(result.Evidence, credentialEvidence),
			Metadata: metadata,
		}, true
	}
	return Candidate{}, false
}

func effectiveScopeEntries(cfg Config) []string {
	entries := append([]string(nil), cfg.Scopes...)
	if cfg.IsolatedNetwork {
		entries = append(entries, cfg.DiscoveryCIDRs...)
		if cfg.AutoDiscoverNetwork {
			entries = append(entries, LocalNetworkCIDRs()...)
		}
	}
	return cleanStrings(entries)
}

func discoveryNetworks(cfg Config) []string {
	entries := append([]string(nil), cfg.DiscoveryCIDRs...)
	if cfg.AutoDiscoverNetwork {
		entries = append(entries, LocalNetworkCIDRs()...)
	}
	for _, scope := range cfg.Scopes {
		if _, err := netip.ParsePrefix(scope); err == nil {
			entries = append(entries, scope)
			continue
		}
		if _, err := netip.ParseAddr(scope); err == nil {
			entries = append(entries, scope)
		}
	}
	return cleanStrings(entries)
}

func LocalNetworkCIDRs() []string {
	seen := map[string]bool{"127.0.0.1/32": true}
	out := []string{"127.0.0.1/32"}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			prefix, err := netip.ParsePrefix(addr.String())
			if err != nil || !prefix.Addr().Is4() {
				continue
			}
			bits := prefix.Bits()
			if bits < 24 {
				bits = 24
			}
			value := netip.PrefixFrom(prefix.Addr(), bits).Masked().String()
			if !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	sort.Strings(out)
	return out
}

func enumerateNetworkHosts(networks []string, limit int) ([]netip.Addr, error) {
	if limit <= 0 {
		limit = 4096
	}
	type cursor struct {
		prefix netip.Prefix
		next   netip.Addr
		done   bool
	}
	cursors := make([]cursor, 0, len(networks))
	for _, raw := range networks {
		var prefix netip.Prefix
		if p, err := netip.ParsePrefix(strings.TrimSpace(raw)); err == nil {
			prefix = p.Masked()
		} else if ip, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil {
			bits := 128
			if ip.Is4() {
				bits = 32
			}
			prefix = netip.PrefixFrom(ip, bits)
		} else {
			return nil, fmt.Errorf("invalid discovery network %q", raw)
		}
		cursors = append(cursors, cursor{prefix: prefix, next: prefix.Addr()})
	}
	seen := map[netip.Addr]bool{}
	out := make([]netip.Addr, 0, min(limit, 4096))
	for len(out) < limit {
		progressed := false
		for i := range cursors {
			cur := &cursors[i]
			if cur.done {
				continue
			}
			if !cur.next.IsValid() || !cur.prefix.Contains(cur.next) {
				cur.done = true
				continue
			}
			ip := cur.next
			cur.next = cur.next.Next()
			progressed = true
			if !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
				if len(out) >= limit {
					break
				}
			}
		}
		if !progressed {
			break
		}
	}
	return out, nil
}

func normalizePorts(in []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(in))
	for _, port := range in {
		if port < 1 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		out = append(out, port)
	}
	sort.Ints(out)
	return out
}
