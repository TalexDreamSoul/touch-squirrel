package hunter

import (
	"fmt"
	"net/netip"
	"strings"
)

type Scope struct {
	hosts     map[string]struct{}
	wildcards []string
	prefixes  []netip.Prefix
}

func ParseScope(entries []string) (Scope, error) {
	s := Scope{hosts: map[string]struct{}{}}
	for _, raw := range entries {
		v := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(raw, ".")))
		if v == "" {
			continue
		}
		if strings.ContainsAny(v, "/:") {
			if p, err := netip.ParsePrefix(v); err == nil {
				s.prefixes = append(s.prefixes, p.Masked())
				continue
			}
			if ip, err := netip.ParseAddr(v); err == nil {
				bits := 128
				if ip.Is4() {
					bits = 32
				}
				s.prefixes = append(s.prefixes, netip.PrefixFrom(ip, bits))
				continue
			}
			return Scope{}, fmt.Errorf("invalid scope entry %q", raw)
		}
		if ip, err := netip.ParseAddr(v); err == nil {
			bits := 128
			if ip.Is4() {
				bits = 32
			}
			s.prefixes = append(s.prefixes, netip.PrefixFrom(ip, bits))
			continue
		}
		if strings.HasPrefix(v, "*.") {
			suffix := strings.TrimPrefix(v, "*.")
			if suffix == "" || strings.Contains(suffix, "*") {
				return Scope{}, fmt.Errorf("invalid wildcard scope %q", raw)
			}
			s.wildcards = append(s.wildcards, "."+suffix)
			continue
		}
		if strings.Contains(v, "*") {
			return Scope{}, fmt.Errorf("wildcard is only allowed as *.example.com")
		}
		s.hosts[v] = struct{}{}
	}
	return s, nil
}

func (s Scope) Allows(host string, ip netip.Addr) bool {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(host, ".")))
	if _, ok := s.hosts[host]; ok {
		return true
	}
	for _, suffix := range s.wildcards {
		if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
			return true
		}
	}
	if ip.IsValid() {
		for _, p := range s.prefixes {
			if p.Contains(ip) {
				return true
			}
		}
	}
	return false
}

var nonPublicPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
	"169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24",
	"192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24",
	"224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "fc00::/7",
	"fe80::/10", "ff00::/8", "2001:db8::/32",
)

func mustPrefixes(raw ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(raw))
	for _, v := range raw {
		out = append(out, netip.MustParsePrefix(v))
	}
	return out
}

func IsPublicAddress(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
