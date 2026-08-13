package hunter

import "strings"

func ClassifyProduct(text string) string {
	v := strings.ToLower(text)
	switch {
	case strings.Contains(v, "cli proxy api server") || strings.Contains(v, "cliproxyapi") || strings.Contains(v, "x-cpa-version"):
		return "cliproxyapi"
	case strings.Contains(v, "sub2api") || (strings.Contains(v, "needs_setup") && strings.Contains(v, "step")):
		return "sub2api"
	case strings.Contains(v, "ollama"):
		return "ollama"
	case strings.Contains(v, "dify"):
		return "dify"
	case strings.Contains(v, "one-api") || strings.Contains(v, "new-api"):
		return "new-api"
	case strings.Contains(v, "litellm"):
		return "litellm"
	case strings.Contains(v, "openai") || strings.Contains(v, "/v1/models") || strings.Contains(v, "ai gateway"):
		return "openai-compatible"
	default:
		return "unknown"
	}
}

func RedactText(text string) string {
	out := text
	for _, p := range evidencePatterns {
		out = p.re.ReplaceAllStringFunc(out, redactSecret)
	}
	return out
}
