package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grok-free-register/grok-reg/internal/artifact"
	"github.com/grok-free-register/grok-reg/internal/config"
	"github.com/grok-free-register/grok-reg/internal/home"
)

func newArtifactTestServer(t *testing.T) (*Server, *artifact.Store, home.Paths) {
	t.Helper()
	root := t.TempDir()
	paths := home.Paths{
		Root:         root,
		Config:       filepath.Join(root, "config.env"),
		Outputs:      filepath.Join(root, "outputs"),
		LocalPool:    filepath.Join(root, "local-pool"),
		TmpDir:       filepath.Join(root, "tmp"),
		ExportsDir:   filepath.Join(root, "exports"),
		PluginsDir:   filepath.Join(root, "plugins"),
		EnabledFile:  filepath.Join(root, "enabled.json"),
		ArtifactsDir: filepath.Join(root, "artifacts"),
		MarketCache:  filepath.Join(root, "market-cache"),
		AccountsDB:   filepath.Join(root, "accounts.db"),
		ClusterState: filepath.Join(root, "cluster-state.json"),
		StatusLayout: filepath.Join(root, "status-layout.json"),
		NotifyFile:   filepath.Join(root, "notifications.json"),
		HunterFile:   filepath.Join(root, "hunter.json"),
	}
	return New(Options{Paths: paths}), artifact.NewStore(paths.ArtifactsDir), paths
}

func artifactRequest(server *Server, method, target string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, body)
	request.RemoteAddr = "127.0.0.1:4321"
	request.Host = "127.0.0.1:8787"
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func TestArtifactsRequireTokenOutsideLoopback(t *testing.T) {
	server, store, _ := newArtifactTestServer(t)
	created, err := store.PutJSON("xai-accounts", "account.xai", artifact.StatusFresh, nil, map[string]any{"token": "secret"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/api/artifacts",
		"/api/artifacts/" + created.ID,
		"/api/artifacts/" + created.ID + "/download",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.RemoteAddr = "198.51.100.20:4321"
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("target=%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
	body := bytes.NewBufferString(`{"ids":["` + created.ID + `"]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/artifacts/download", body)
	request.RemoteAddr = "198.51.100.20:4321"
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("batch status=%d body=%s", response.Code, response.Body.String())
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP"} {
		request = httptest.NewRequest(http.MethodGet, "/api/artifacts/"+created.ID, nil)
		request.RemoteAddr = "127.0.0.1:4321"
		request.Host = "127.0.0.1:8787"
		request.Header.Set(header, "198.51.100.20")
		response = httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("header=%s status=%d body=%s", header, response.Code, response.Body.String())
		}
	}
	for name, mutate := range map[string]func(*http.Request){
		"host":   func(request *http.Request) { request.Host = "attacker.example" },
		"origin": func(request *http.Request) { request.Header.Set("Origin", "https://attacker.example") },
	} {
		request = httptest.NewRequest(http.MethodGet, "/api/artifacts/"+created.ID, nil)
		request.RemoteAddr = "127.0.0.1:4321"
		request.Host = "127.0.0.1:8787"
		mutate(request)
		response = httptest.NewRecorder()
		server.Handler().ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || response.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Fatalf("case=%s status=%d cors=%q body=%s", name, response.Code, response.Header().Get("Access-Control-Allow-Origin"), response.Body.String())
		}
	}
}

func TestLoopbackListenAddress(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"} {
		if !isLoopbackListenAddr(addr) {
			t.Fatalf("expected loopback: %s", addr)
		}
	}
	for _, addr := range []string{"", ":8787", "0.0.0.0:8787", "[::]:8787", "198.51.100.20:8787"} {
		if isLoopbackListenAddr(addr) {
			t.Fatalf("expected non-loopback: %s", addr)
		}
	}
	server, _, _ := newArtifactTestServer(t)
	server.opt.Addr = "0.0.0.0:0"
	if err := server.ListenAndServe(); err == nil || !strings.Contains(err.Error(), "PANEL_TOKEN") {
		t.Fatalf("expected token requirement, got %v", err)
	}
}

func TestLocalPoolImportMirrorsArtifact(t *testing.T) {
	server, _, paths := newArtifactTestServer(t)
	runID := "run-mirror"
	credentialDir := filepath.Join(paths.Outputs, runID, "CPA")
	if err := os.MkdirAll(credentialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credentialDir, "xai-mirror.json"), []byte(`{"email":"mirror@example.com","token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"run_id":"` + runID + `"}`)
	response := artifactRequest(server, http.MethodPost, "/api/local-pool/import", body)
	if response.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", response.Code, response.Body.String())
	}
	response = artifactRequest(server, http.MethodGet, "/api/artifacts?q=xai-mirror.json", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) || !strings.Contains(response.Body.String(), "mirror@example.com") {
		t.Fatalf("artifact status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAutoImportMirrorsArtifact(t *testing.T) {
	server, _, paths := newArtifactTestServer(t)
	cfg := config.Defaults()
	cfg.LocalPoolAutoImport = true
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	runID := "run-auto-mirror"
	credentialDir := filepath.Join(paths.Outputs, runID, "CPA")
	if err := os.MkdirAll(credentialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credentialDir, "xai-auto.json"), []byte(`{"email":"auto@example.com","token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server.autoImportLatestRun(runID)
	response := artifactRequest(server, http.MethodGet, "/api/artifacts?q=xai-auto.json", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) || !strings.Contains(response.Body.String(), "auto@example.com") {
		t.Fatalf("artifact status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestArtifactsListDetailAndDownload(t *testing.T) {
	server, store, _ := newArtifactTestServer(t)
	created, err := store.PutJSON("github-registrar", "session.cookie", artifact.StatusFresh, map[string]string{
		"email": "dev@example.com", "username": "octocat", "source_file": "octocat.json",
	}, map[string]any{"platform": "github", "cookie": "secret"}, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutJSON("tavily-pool", "key.tavily", artifact.StatusHealthy, map[string]string{
		"key_id": "key-1",
	}, map[string]any{"provider": "tavily", "masked": "tvly-***"}, ""); err != nil {
		t.Fatal(err)
	}

	response := artifactRequest(server, http.MethodGet, "/api/artifacts?q=dev%40example.com&kind=session.cookie&page=1&limit=10", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var listed struct {
		Artifacts []artifactSummary `json:"artifacts"`
		Total     int               `json:"total"`
		Facets    struct {
			Plugins []string `json:"plugins"`
			Kinds   []string `json:"kinds"`
		} `json:"facets"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 || len(listed.Artifacts) != 1 {
		t.Fatalf("unexpected list: %+v", listed)
	}
	row := listed.Artifacts[0]
	if row.Email != "dev@example.com" || row.Account != "octocat" || row.Channel != "github" || len(row.Payload) != 0 {
		t.Fatalf("unexpected summary: %+v", row)
	}
	if len(listed.Facets.Plugins) != 2 || len(listed.Facets.Kinds) != 2 {
		t.Fatalf("unexpected facets: %+v", listed.Facets)
	}

	response = artifactRequest(server, http.MethodGet, "/api/artifacts?page=9223372036854775807&limit=200", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("large page status=%d body=%s", response.Code, response.Body.String())
	}
	var paged struct {
		Page      int               `json:"page"`
		Artifacts []artifactSummary `json:"artifacts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &paged); err != nil {
		t.Fatal(err)
	}
	if paged.Page != 1 || len(paged.Artifacts) != 2 {
		t.Fatalf("large page response: %+v", paged)
	}

	response = artifactRequest(server, http.MethodGet, "/api/artifacts?channel=tavily&status=healthy", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("filtered status=%d body=%s", response.Code, response.Body.String())
	}

	response = artifactRequest(server, http.MethodGet, "/api/artifacts/"+created.ID, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"cookie":"secret"`) {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}

	response = artifactRequest(server, http.MethodGet, "/api/artifacts/"+created.ID+"/download", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("download status=%d body=%s", response.Code, response.Body.String())
	}
	if disposition := response.Header().Get("Content-Disposition"); !strings.Contains(disposition, "octocat.json") {
		t.Fatalf("disposition=%q", disposition)
	}
	if !strings.Contains(response.Body.String(), `"cookie": "secret"`) {
		t.Fatalf("download body=%s", response.Body.String())
	}

	server.opt.Token = "panel-token"
	response = artifactRequest(server, http.MethodGet, "/api/artifacts/"+created.ID, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated detail status=%d", response.Code)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/artifacts/"+created.ID+"/download", nil)
	request.Header.Set("X-Panel-Token", "panel-token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated download status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestArtifactsBatchDownload(t *testing.T) {
	server, store, _ := newArtifactTestServer(t)
	first, err := store.PutJSON("xai-accounts", "account.xai", artifact.StatusFresh, map[string]string{
		"email": "one@example.com", "source_file": "one.json",
	}, map[string]any{"token": "one"}, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PutJSON("xai-accounts", "account.xai", artifact.StatusFresh, map[string]string{
		"email": "two@example.com", "source_file": "two.json",
	}, map[string]any{"token": "two"}, "")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"ids": []string{first.ID, second.ID}})
	response := artifactRequest(server, http.MethodPost, "/api/artifacts/download", bytes.NewReader(body))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 2 {
		t.Fatalf("zip entries=%d", len(archive.File))
	}
	for _, file := range archive.File {
		if !strings.HasSuffix(file.Name, ".json") || strings.Contains(file.Name, "/") {
			t.Fatalf("unsafe zip entry %q", file.Name)
		}
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(entry)
		_ = entry.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(payload, []byte(`"token"`)) {
			t.Fatalf("unexpected payload in %q: %s", file.Name, payload)
		}
	}

	tooMany := make([]string, maxArtifactBatchItems+1)
	for index := range tooMany {
		tooMany[index] = first.ID
	}
	body, _ = json.Marshal(map[string]any{"ids": tooMany})
	response = artifactRequest(server, http.MethodPost, "/api/artifacts/download", bytes.NewReader(body))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized selection status=%d body=%s", response.Code, response.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"ids": []string{"missing"}})
	response = artifactRequest(server, http.MethodPost, "/api/artifacts/download", bytes.NewReader(body))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing selection status=%d body=%s", response.Code, response.Body.String())
	}
}
