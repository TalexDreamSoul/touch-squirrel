package tavilypool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Server is a minimal hikari-like HTTP façade over the key pool.
//
// Routes:
//
//	GET  /health
//	GET  /api/keys          (redacted list)
//	POST /api/keys          {"api_key":"...","note":""}
//	POST /api/tavily/search|extract|crawl|map|research
//	ANY  /mcp               Tavily MCP proxy (pool key injected)
//
// Upstream default: https://api.tavily.com{path}
// MCP upstream default: https://mcp.tavily.com/mcp
type Server struct {
	Pool        *Pool
	Upstream    string // e.g. https://api.tavily.com
	MCPUpstream string // e.g. https://mcp.tavily.com/mcp
	Addr        string
	Logf        func(string, ...any)
	client      *http.Client
}

func (s *Server) logf(f string, a ...any) {
	if s.Logf != nil {
		s.Logf(f, a...)
	}
}

func (s *Server) upstreamBase() string {
	u := strings.TrimRight(s.Upstream, "/")
	if u == "" {
		u = "https://api.tavily.com"
	}
	return u
}

// Handler returns the HTTP handler (for tests / custom listeners).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /api/keys", s.handleKeysList)
	mux.HandleFunc("POST /api/keys", s.handleKeysAdd)
	mux.HandleFunc("POST /api/tavily/search", s.proxy("search"))
	mux.HandleFunc("POST /api/tavily/extract", s.proxy("extract"))
	mux.HandleFunc("POST /api/tavily/crawl", s.proxy("crawl"))
	mux.HandleFunc("POST /api/tavily/map", s.proxy("map"))
	mux.HandleFunc("POST /api/tavily/research", s.proxy("research"))
	// bare /search for thin clients
	mux.HandleFunc("POST /search", s.proxy("search"))
	// MCP surface (streamable HTTP / JSON-RPC)
	mux.HandleFunc("POST /mcp", s.handleMCP)
	mux.HandleFunc("GET /mcp", s.handleMCP)
	mux.HandleFunc("DELETE /mcp", s.handleMCP)
	mux.HandleFunc("POST /mcp/", s.handleMCP)
	mux.HandleFunc("GET /mcp/", s.handleMCP)
	mux.HandleFunc("DELETE /mcp/", s.handleMCP)
	return mux
}

// ListenAndServe binds Addr and serves until ctx cancel or error.
func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.client == nil {
		s.client = &http.Client{Timeout: 120 * time.Second}
	}
	addr := s.Addr
	if addr == "" {
		addr = "127.0.0.1:8791"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler()}
	s.logf("tavily-pool listening on http://%s (upstream %s)", ln.Addr().String(), s.upstreamBase())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	keys, _ := s.Pool.List(true)
	active := 0
	for _, k := range keys {
		if k.Status == StatusActive {
			active++
		}
	}
	writeJSON(w, 200, map[string]any{
		"ok":           true,
		"plugin":       "tavily-pool",
		"keys_total":   len(keys),
		"keys_active":  active,
		"upstream":     s.upstreamBase(),
		"mcp_upstream": s.mcpUpstream(),
		"surfaces":     []string{"http", "mcp"},
	})
}

func (s *Server) handleKeysList(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Pool.List(true)
	if err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "keys": keys})
}

func (s *Server) handleKeysAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"api_key"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	k, err := s.Pool.Add(body.APIKey, body.Note)
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "key": k})
}

func (s *Server) proxy(endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.client == nil {
			s.client = &http.Client{Timeout: 120 * time.Second}
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var payload map[string]any
		if len(bytes.TrimSpace(raw)) == 0 {
			payload = map[string]any{}
		} else if err := json.Unmarshal(raw, &payload); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "body must be json object"})
			return
		}
		// strip client-supplied upstream secrets
		delete(payload, "api_key")
		delete(payload, "apiKey")

		key, err := s.Pool.Acquire()
		if err != nil {
			writeJSON(w, 503, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		payload["api_key"] = key.APIKey
		body, _ := json.Marshal(payload)

		upURL := s.upstreamBase() + "/" + endpoint
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upURL, bytes.NewReader(body))
		if err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := s.client.Do(req)
		if err != nil {
			s.Pool.ReportFailure(key.ID, false)
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error(), "key_id": key.ID})
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))

		// hikari: 432 => exhausted until next UTC month
		if resp.StatusCode == 432 {
			s.Pool.ReportFailure(key.ID, true)
			s.logf("key %s exhausted (432)", key.ID)
		} else if resp.StatusCode >= 400 {
			s.Pool.ReportFailure(key.ID, false)
		} else {
			s.Pool.ReportSuccess(key.ID)
		}

		// pass through status + body; add routing metadata headers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Squirrel-Key-Id", key.ID)
		w.Header().Set("X-Squirrel-Plugin", "tavily-pool")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ListenAddr validates bind string.
func ListenAddr(addr string) (string, error) {
	if strings.TrimSpace(addr) == "" {
		return "127.0.0.1:8791", nil
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		// allow :8791
		if strings.HasPrefix(addr, ":") {
			return "0.0.0.0" + addr, nil
		}
		return "", fmt.Errorf("invalid addr %q: %w", addr, err)
	}
	return addr, nil
}
