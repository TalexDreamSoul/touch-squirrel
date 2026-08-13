package hunter

import (
	"net/netip"
	"testing"
)

func TestScopeAllowsExplicitTargetsOnly(t *testing.T) {
	s, err := ParseScope([]string{"example.com", "*.owned.example", "203.0.113.0/24", "198.51.100.8"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		host string
		ip   string
		want bool
	}{
		{"example.com", "93.184.216.34", true},
		{"api.owned.example", "93.184.216.34", true},
		{"owned.example", "93.184.216.34", false},
		{"unowned.example", "203.0.113.9", true},
		{"unowned.example", "198.51.100.8", true},
		{"unowned.example", "198.51.100.9", false},
	}
	for _, tc := range cases {
		ip := netip.MustParseAddr(tc.ip)
		if got := s.Allows(tc.host, ip); got != tc.want {
			t.Errorf("Allows(%q, %s)=%v want %v", tc.host, ip, got, tc.want)
		}
	}
}

func TestPublicAddressRejectsSpecialNetworks(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.0.0.1", "169.254.169.254", "172.16.0.1",
		"192.168.1.1", "0.0.0.0", "224.0.0.1", "::1", "fc00::1", "fe80::1",
	}
	for _, raw := range blocked {
		if IsPublicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("expected %s to be blocked", raw)
		}
	}
	if !IsPublicAddress(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("expected public address to be accepted")
	}
}
