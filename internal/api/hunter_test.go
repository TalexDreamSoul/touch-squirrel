package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/home"
)

func TestHunterProbeCannotAuthorizePrivateAddress(t *testing.T) {
	dir := t.TempDir()
	paths := home.Paths{
		Root:         dir,
		Config:       filepath.Join(dir, "config.env"),
		PatrolState:  filepath.Join(dir, "patrol.json"),
		ClusterState: filepath.Join(dir, "cluster.json"),
		StatusLayout: filepath.Join(dir, "status.json"),
		LocalPool:    filepath.Join(dir, "pool"),
		ExportsDir:   filepath.Join(dir, "exports"),
		TmpDir:       filepath.Join(dir, "tmp"),
		UploadCache:  filepath.Join(dir, "upload.json"),
		AccountsDB:   filepath.Join(dir, "accounts.db"),
		HunterFile:   filepath.Join(dir, "hunter.json"),
		NotifyFile:   filepath.Join(dir, "notifications.json"),
	}
	server := New(Options{Paths: paths})
	remoteReq := httptest.NewRequest(http.MethodGet, "/api/hunter", nil)
	remoteReq.RemoteAddr = "198.51.100.20:4321"
	remoteRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(remoteRes, remoteReq)
	if remoteRes.Code != http.StatusForbidden {
		t.Fatalf("remote hunter status=%d", remoteRes.Code)
	}

	srv := httptest.NewServer(server.Handler())
	defer srv.Close()

	requestJSON(t, http.MethodPut, srv.URL+"/api/hunter/config", map[string]interface{}{
		"scopes":          []string{"localhost"},
		"probe_enabled":   true,
		"max_results":     10,
		"rate_per_minute": 6,
	}, http.StatusOK)
	var imported struct {
		Imported int `json:"imported"`
	}
	requestJSONInto(t, http.MethodPost, srv.URL+"/api/hunter/import", map[string]interface{}{
		"items": []map[string]string{{"url": "http://localhost:8080", "product": "ollama"}},
	}, http.StatusOK, &imported)
	if imported.Imported != 1 {
		t.Fatalf("imported=%d", imported.Imported)
	}
	var snap struct {
		Snapshot struct {
			Findings []struct {
				ID      string `json:"id"`
				InScope bool   `json:"in_scope"`
			} `json:"findings"`
		} `json:"snapshot"`
	}
	requestJSONInto(t, http.MethodGet, srv.URL+"/api/hunter", nil, http.StatusOK, &snap)
	if len(snap.Snapshot.Findings) != 1 || !snap.Snapshot.Findings[0].InScope {
		t.Fatalf("findings=%+v", snap.Snapshot.Findings)
	}
	requestJSON(t, http.MethodPost, srv.URL+"/api/hunter/findings/"+snap.Snapshot.Findings[0].ID+"/probe", map[string]interface{}{}, http.StatusConflict)
}

func requestJSON(t *testing.T, method, target string, body interface{}, want int) {
	t.Helper()
	requestJSONInto(t, method, target, body, want, nil)
}

func requestJSONInto(t *testing.T, method, target string, body interface{}, want int, out interface{}) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, target, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		t.Fatalf("%s %s status=%d want=%d", method, target, res.StatusCode, want)
	}
	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}
