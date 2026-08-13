package hunter

import (
	"strings"
	"testing"
)

func TestDetectEvidenceNeverStoresRawSecret(t *testing.T) {
	raw := "configuration OPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvxyz0123456789 ready"
	items := DetectEvidence([]byte(raw))
	if len(items) != 1 {
		t.Fatalf("items=%+v", items)
	}
	got := items[0]
	if got.Kind != "openai_api_key" || got.Fingerprint == "" {
		t.Fatalf("unexpected evidence: %+v", got)
	}
	if strings.Contains(got.Redacted, "abcdefghijkl") || strings.Contains(got.Redacted, "0123456789") {
		t.Fatalf("redaction leaked secret: %q", got.Redacted)
	}
}
