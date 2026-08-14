package cpa

import (
	"encoding/json"
	"testing"
)

// A broken proxy must fail the probe, not quietly fall through to a direct
// connection: leaking probes onto the host IP is what gets accounts flagged.
func TestHTTPClientForRejectsUnusableProxy(t *testing.T) {
	for _, raw := range []string{"127.0.0.1:40080", "://nope", "http://"} {
		if _, err := httpClientFor(raw, 0); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
	c, err := httpClientFor("http://127.0.0.1:40080", 0)
	if err != nil || c.Transport == nil {
		t.Fatalf("valid proxy should build a proxied client: %v", err)
	}
	if c, err := httpClientFor("  ", 0); err != nil || c.Transport != nil {
		t.Fatalf("empty proxy should keep the default transport: %v", err)
	}
}

// The degradation verdict hangs on this parse, and upstream reports the chain
// of thought either as usage accounting or as explicit reasoning items.
func TestFillReasoningReadsBothSignals(t *testing.T) {
	tokensOnly := `{"output":[{"type":"message","content":[{"type":"output_text","text":"1016"}]}],
		"usage":{"output_tokens_details":{"reasoning_tokens":37}}}`
	var res ProbeResult
	fillReasoning(&res, []byte(tokensOnly))
	if !res.HasReasoning || res.ReasoningTokens != 37 || res.OutputText != "1016" {
		t.Fatalf("usage-only reasoning missed: %+v", res)
	}

	itemsOnly := `{"output":[{"type":"reasoning","summary":[{"text":"8*127"}]},
		{"type":"message","content":[{"type":"output_text","text":"1016"}]}]}`
	res = ProbeResult{}
	fillReasoning(&res, []byte(itemsOnly))
	if !res.HasReasoning || res.OutputText != "1016" {
		t.Fatalf("reasoning item missed: %+v", res)
	}

	// A downgraded account answers with no chain of thought at all.
	none := `{"output":[{"type":"message","content":[{"type":"output_text","text":"1016"}]}],
		"usage":{"output_tokens_details":{"reasoning_tokens":0}}}`
	res = ProbeResult{}
	fillReasoning(&res, []byte(none))
	if res.HasReasoning {
		t.Fatalf("no reasoning must not read as reasoning: %+v", res)
	}

	// An empty reasoning item is not evidence of thinking.
	empty := `{"output":[{"type":"reasoning","summary":[{"text":"  "}]}]}`
	res = ProbeResult{}
	fillReasoning(&res, []byte(empty))
	if res.HasReasoning {
		t.Fatalf("blank reasoning summary must not count: %+v", res)
	}

	res = ProbeResult{}
	fillReasoning(&res, []byte("not json"))
	if res.HasReasoning || res.OutputText != "" {
		t.Fatalf("garbage body should leave the result untouched: %+v", res)
	}
	if !json.Valid([]byte(tokensOnly)) {
		t.Fatal("fixture is not valid json")
	}
}

// The probe model must reach both the body and the override header, or a
// grok-4.6 degradation check silently measures grok-4.5.
func TestProbeOptionsDefaults(t *testing.T) {
	got := ProbeOptions{}.withDefaults()
	if got.Model != "grok-4.5" || got.Prompt != "ok" || got.MaxOutputTokens != 16 {
		t.Fatalf("zero value must reproduce the legacy health probe: %+v", got)
	}
	got = ProbeOptions{Model: "grok-4.6", Prompt: "127*8=?", MaxOutputTokens: 512}.withDefaults()
	if got.Model != "grok-4.6" || got.MaxOutputTokens != 512 {
		t.Fatalf("explicit options must survive: %+v", got)
	}
}
