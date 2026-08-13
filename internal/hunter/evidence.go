package hunter

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

type evidencePattern struct {
	kind string
	re   *regexp.Regexp
}

var evidencePatterns = []evidencePattern{
	{kind: "openai_api_key", re: regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_-]{24,}\b`)},
	{kind: "anthropic_api_key", re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`)},
	{kind: "google_api_key", re: regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{30,}\b`)},
	{kind: "github_token", re: regexp.MustCompile(`\b(?:ghp|github_pat)_[A-Za-z0-9_]{20,}\b`)},
	{kind: "aws_access_key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
}

func DetectEvidence(body []byte) []Evidence {
	seen := map[string]bool{}
	var out []Evidence
	for _, p := range evidencePatterns {
		for _, secret := range p.re.FindAll(body, -1) {
			sum := sha256.Sum256(secret)
			fp := hex.EncodeToString(sum[:8])
			key := p.kind + ":" + fp
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Evidence{Kind: p.kind, Fingerprint: fp, Redacted: redactSecret(string(secret))})
		}
	}
	return out
}

func redactSecret(secret string) string {
	if len(secret) <= 8 {
		return "****"
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}
