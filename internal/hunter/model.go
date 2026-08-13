package hunter

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const MaskedSecret = "••••"

const (
	FindingNew       = "new"
	FindingConfirmed = "confirmed"
	FindingDismissed = "dismissed"

	DraftPending  = "pending"
	DraftApproved = "approved"
	DraftSending  = "sending"
	DraftSent     = "sent"
)

type Config struct {
	Scopes                 []string `json:"scopes"`
	FOFAEmail              string   `json:"fofa_email"`
	FOFAKey                string   `json:"fofa_key"`
	FOFAQueries            []string `json:"fofa_queries"`
	ShodanKey              string   `json:"shodan_key"`
	ShodanQueries          []string `json:"shodan_queries"`
	ProbeEnabled           bool     `json:"probe_enabled"`
	IsolatedNetwork        bool     `json:"isolated_network"`
	AutoDiscoverNetwork    bool     `json:"auto_discover_network"`
	CredentialAuditEnabled bool     `json:"credential_audit_enabled"`
	DiscoveryCIDRs         []string `json:"discovery_cidrs"`
	DiscoveryPorts         []int    `json:"discovery_ports"`
	DiscoveryConcurrency   int      `json:"discovery_concurrency"`
	DiscoveryTimeoutMS     int      `json:"discovery_timeout_ms"`
	MaxDiscoveryHosts      int      `json:"max_discovery_hosts"`
	MaxResults             int      `json:"max_results"`
	RatePerMinute          int      `json:"rate_per_minute"`
}

type Evidence struct {
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	Redacted    string `json:"redacted"`
}

type Finding struct {
	ID         string            `json:"id"`
	URL        string            `json:"url"`
	Host       string            `json:"host"`
	Source     string            `json:"source"`
	Query      string            `json:"query,omitempty"`
	Product    string            `json:"product,omitempty"`
	Title      string            `json:"title,omitempty"`
	Banner     string            `json:"banner,omitempty"`
	Status     string            `json:"status"`
	InScope    bool              `json:"in_scope"`
	HTTPStatus int               `json:"http_status,omitempty"`
	Evidence   []Evidence        `json:"evidence,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ObservedAt string            `json:"observed_at"`
	ProbedAt   string            `json:"probed_at,omitempty"`
	UpdatedAt  string            `json:"updated_at"`
}

type Draft struct {
	ID         string `json:"id"`
	FindingID  string `json:"finding_id"`
	ChannelID  string `json:"channel_id,omitempty"`
	To         string `json:"to"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ApprovedAt string `json:"approved_at,omitempty"`
	ApprovedBy string `json:"approved_by,omitempty"`
	SentAt     string `json:"sent_at,omitempty"`
	SendError  string `json:"send_error,omitempty"`
}

type Audit struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	TargetID  string `json:"target_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
	CreatedAt string `json:"created_at"`
}

type Snapshot struct {
	Version  int       `json:"version"`
	Config   Config    `json:"config"`
	Findings []Finding `json:"findings"`
	Drafts   []Draft   `json:"drafts"`
	Audit    []Audit   `json:"audit"`
}

func DefaultConfig() Config {
	return Config{
		ProbeEnabled:           true,
		IsolatedNetwork:        true,
		AutoDiscoverNetwork:    true,
		CredentialAuditEnabled: true,
		DiscoveryPorts:         []int{80, 443, 3000, 3001, 4000, 5000, 7860, 8000, 8080, 8081, 8317, 8443, 11434},
		DiscoveryConcurrency:   64,
		DiscoveryTimeoutMS:     500,
		MaxDiscoveryHosts:      4096,
		MaxResults:             50,
		RatePerMinute:          60,
	}
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func stableID(prefix, value string) string {
	sum := sha256.Sum256([]byte(value))
	return prefix + "_" + hex.EncodeToString(sum[:8])
}
