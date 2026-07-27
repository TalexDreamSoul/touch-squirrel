package tavilypool

import (
	"io"
	"net/http"
	"strings"
)

// DefaultMCPUpstream is Tavily's public MCP endpoint (hikari default family).
const DefaultMCPUpstream = "https://mcp.tavily.com/mcp"

func (s *Server) mcpUpstream() string {
	u := strings.TrimRight(s.MCPUpstream, "/")
	if u == "" {
		u = DefaultMCPUpstream
	}
	return u
}

// handleMCP proxies JSON-RPC / streamable MCP traffic to Tavily MCP using a pool key.
//
// MVP behavior (hikari-aligned subset, no session rebalance):
//   - Acquire LRU active key
//   - Forward method + body
//   - Inject Authorization: Bearer <api_key> and strip client auth
//   - On HTTP 432 mark key exhausted
//   - Pass through status/body/content-type
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if s.client == nil {
		s.client = http.DefaultClient
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key, err := s.Pool.Acquire()
	if err != nil {
		writeJSON(w, 503, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	upURL := s.mcpUpstream()
	// preserve query from client except secrets
	if q := r.URL.RawQuery; q != "" {
		// drop client-supplied keys
		vals := r.URL.Query()
		vals.Del("tavilyApiKey")
		vals.Del("api_key")
		vals.Del("apiKey")
		// inject pool key as query for clients that expect it (Tavily MCP accepts both)
		vals.Set("tavilyApiKey", key.APIKey)
		if enc := vals.Encode(); enc != "" {
			upURL = upURL + "?" + enc
		}
	} else {
		upURL = upURL + "?tavilyApiKey=" + key.APIKey
	}

	var body io.Reader
	if r.Body != nil && r.Method != http.MethodGet {
		body = r.Body
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upURL, body)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// copy safe headers
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	} else if r.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if acc := r.Header.Get("Accept"); acc != "" {
		req.Header.Set("Accept", acc)
	} else {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	// session / protocol headers used by streamable HTTP MCP
	for _, h := range []string{"Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	// also send Bearer for upstreams that prefer header auth
	req.Header.Set("Authorization", "Bearer "+key.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		s.Pool.ReportFailure(key.ID, false)
		writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error(), "key_id": key.ID})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 432 {
		s.Pool.ReportFailure(key.ID, true)
		s.logf("mcp key %s exhausted (432)", key.ID)
	} else if resp.StatusCode >= 400 {
		s.Pool.ReportFailure(key.ID, false)
	} else {
		s.Pool.ReportSuccess(key.ID)
	}

	// pass through selected headers
	for _, h := range []string{"Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Cache-Control"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("X-Squirrel-Key-Id", key.ID)
	w.Header().Set("X-Squirrel-Plugin", "tavily-pool")
	w.Header().Set("X-Squirrel-Surface", "mcp")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 32<<20))
}
