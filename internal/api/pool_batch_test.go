package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grok-free-register/grok-reg/internal/config"
)

func waitForUploadCount(t *testing.T, uploads *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if uploads.Load() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("uploads=%d want at least %d", uploads.Load(), want)
}

func TestPoolBatchAccountLifecycle(t *testing.T) {
	server, _, paths := newArtifactTestServer(t)
	runDir := filepath.Join(paths.Outputs, "run-batch")
	credentialDir := filepath.Join(runDir, "CPA")
	if err := os.MkdirAll(credentialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	credentialName := "batch-account.json"
	credentialRaw := []byte(`{"email":"batch@example.com","token":"secret"}`)
	if err := os.WriteFile(filepath.Join(credentialDir, credentialName), credentialRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, entries, err := server.localPool.ImportRun(runDir)
	if err != nil {
		t.Fatal(err)
	}
	server.indexLocalEntries(entries)

	response := artifactRequest(server, http.MethodGet, "/api/pool/list?source=accounts&page=1&limit=50&q=batch-account.json", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Items []AccountItemForTest `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("unexpected accounts: %+v", list.Items)
	}
	accountID := list.Items[0].ID

	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	response = artifactRequest(server, http.MethodGet, "/api/pool/list?source=accounts&time_field=created_at&from="+future, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":0`) {
		t.Fatalf("time filter status=%d body=%s", response.Code, response.Body.String())
	}
	response = artifactRequest(server, http.MethodGet, "/api/pool/list?source=accounts&time_field=invalid", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid time field status=%d body=%s", response.Code, response.Body.String())
	}

	batchBody := func(action string, ids []string) *bytes.Buffer {
		raw, _ := json.Marshal(map[string]any{"source": "accounts", "action": action, "ids": ids})
		return bytes.NewBuffer(raw)
	}
	response = artifactRequest(server, http.MethodPost, "/api/pool/batch", batchBody("disable", []string{accountID}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"succeeded":1`) {
		t.Fatalf("disable status=%d body=%s", response.Code, response.Body.String())
	}
	response = artifactRequest(server, http.MethodGet, "/api/pool/list?source=accounts&status=disabled", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), accountID) {
		t.Fatalf("disabled list status=%d body=%s", response.Code, response.Body.String())
	}

	downloadRaw, _ := json.Marshal(map[string]any{"source": "accounts", "ids": []string{accountID}})
	response = artifactRequest(server, http.MethodPost, "/api/pool/batch/download", bytes.NewReader(downloadRaw))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("download status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != credentialName {
		t.Fatalf("unexpected archive: %+v", archive.File)
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(entry)
	_ = entry.Close()
	if string(got) != string(credentialRaw) {
		t.Fatalf("unexpected credential: %s", got)
	}
	partialRaw, _ := json.Marshal(map[string]any{"source": "accounts", "ids": []string{accountID, "missing"}})
	response = artifactRequest(server, http.MethodPost, "/api/pool/batch/download", bytes.NewReader(partialRaw))
	if response.Code != http.StatusUnprocessableEntity || response.Header().Get("Content-Type") == "application/zip" || !strings.Contains(response.Body.String(), `"failed":1`) {
		t.Fatalf("partial download status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}

	var uploads atomic.Int32
	cpaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			uploads.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer cpaServer.Close()
	cfg := config.Defaults()
	cfg.CPAManagementBase = cpaServer.URL
	cfg.CPAManagementKey = "test-key"
	cfg.CPAUploadVerify = false
	cfg.CPAUploadMode = "json"
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	appendTestConfigSecret(t, paths.Config, "CPA_MANAGEMENT_KEY=test-key\n")
	response = artifactRequest(server, http.MethodPost, "/api/pool/batch", batchBody("upload_cpa", []string{accountID}))
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"job_id":`) || !strings.Contains(response.Body.String(), `"queued":1`) {
		t.Fatalf("upload queue status=%d body=%s", response.Code, response.Body.String())
	}
	waitForUploadCount(t, &uploads, 1)

	response = artifactRequest(server, http.MethodPost, "/api/pool/batch", batchBody("delete", []string{accountID}))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"succeeded":1`) {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err := server.localPool.PathFor(credentialName); !os.IsNotExist(err) {
		t.Fatalf("credential was not deleted: %v", err)
	}
	response = artifactRequest(server, http.MethodGet, "/api/pool/list?source=accounts&q=batch-account.json", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":0`) {
		t.Fatalf("deleted account returned status=%d body=%s", response.Code, response.Body.String())
	}
}

type AccountItemForTest struct {
	ID string `json:"id"`
}

func TestPoolBatchCloudAndFederation(t *testing.T) {
	server, _, paths := newArtifactTestServer(t)
	var uploads atomic.Int32
	var deletes atomic.Int32
	cpaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/download"):
			_, _ = w.Write([]byte(`{"email":"cloud@example.com","token":"secret"}`))
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"name":"cloud.json","provider":"xai","status":"active"}]}`))
		case r.Method == http.MethodPost:
			uploads.Add(1)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodDelete:
			deletes.Add(1)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer cpaServer.Close()

	cfg := config.Defaults()
	cfg.CPAManagementBase = cpaServer.URL
	cfg.CPAManagementKey = "test-key"
	cfg.CPAUploadVerify = false
	cfg.CPAUploadMode = "json"
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	appendTestConfigSecret(t, paths.Config, "CPA_MANAGEMENT_KEY=test-key\n")

	response := artifactRequest(server, http.MethodGet, "/api/pool/list?source=cloud&page=1&limit=50", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "cloud.json") || !strings.Contains(response.Body.String(), `"delete":true`) {
		t.Fatalf("cloud list status=%d body=%s", response.Code, response.Body.String())
	}
	downloadBody := []byte(`{"source":"cloud","ids":["cloud.json"]}`)
	response = artifactRequest(server, http.MethodPost, "/api/pool/batch/download", bytes.NewReader(downloadBody))
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("cloud download status=%d body=%s", response.Code, response.Body.String())
	}
	deleteBody := []byte(`{"source":"cloud","action":"delete","ids":["cloud.json"]}`)
	response = artifactRequest(server, http.MethodPost, "/api/pool/batch", bytes.NewReader(deleteBody))
	if response.Code != http.StatusOK || deletes.Load() != 1 {
		t.Fatalf("cloud delete status=%d deletes=%d body=%s", response.Code, deletes.Load(), response.Body.String())
	}

	master := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Cluster-Token") != "per-master-token" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "bad cluster token"})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/pull") {
			_, _ = w.Write([]byte(`{"email":"federation@example.com","token":"remote-secret"}`))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "source": "federation", "total": 1, "total_pages": 1,
			"share_pool_list": true, "share_pool_pull": true,
			"capabilities": poolCapabilities("federation", true),
			"files":        []map[string]any{{"name": "federation.json", "provider": "xai", "status": "active"}},
		})
	}))
	defer master.Close()
	cfg.ClusterMasterURLs = fmt.Sprintf(`[{"url":%q,"token":"per-master-token"}]`, master.URL)
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	appendTestConfigSecret(t, paths.Config, "CPA_MANAGEMENT_KEY=test-key\n")
	response = artifactRequest(server, http.MethodGet, "/api/pool/list?source=federation&master="+master.URL+"&page=1&limit=50", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "federation.json") {
		t.Fatalf("federation list status=%d body=%s", response.Code, response.Body.String())
	}
	uploadBody, _ := json.Marshal(map[string]any{
		"source": "federation", "action": "upload_cpa", "ids": []string{"federation.json"}, "master": master.URL,
	})
	response = artifactRequest(server, http.MethodPost, "/api/pool/batch", bytes.NewReader(uploadBody))
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"job_id":`) || !strings.Contains(response.Body.String(), `"queued":1`) {
		t.Fatalf("federation upload queue status=%d body=%s", response.Code, response.Body.String())
	}
	waitForUploadCount(t, &uploads, 1)

	var attackerHits atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()
	response = artifactRequest(server, http.MethodGet, "/api/pool/list?source=federation&master="+attacker.URL, nil)
	if response.Code != http.StatusBadRequest || attackerHits.Load() != 0 {
		t.Fatalf("unconfigured master status=%d hits=%d body=%s", response.Code, attackerHits.Load(), response.Body.String())
	}
}

func appendTestConfigSecret(t *testing.T, path, value string) {
	t.Helper()
	configFile, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configFile.WriteString(value); err != nil {
		_ = configFile.Close()
		t.Fatal(err)
	}
	if err := configFile.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSafeCredentialName(t *testing.T) {
	for _, input := range []string{`..\\..\\target.json`, "../../target.json", `C:\\secret\\key.json`, "bad\nname.json"} {
		name := safeCredentialName(input)
		if strings.ContainsAny(name, `/\\:`) || strings.Contains(name, "..") || !strings.HasSuffix(name, ".json") {
			t.Fatalf("unsafe archive name %q from %q", name, input)
		}
	}
}

func TestPoolBatchValidationAndCapabilities(t *testing.T) {
	server, _, _ := newArtifactTestServer(t)
	request := func(source, action string, ids []string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(map[string]any{"source": source, "action": action, "ids": ids})
		return artifactRequest(server, http.MethodPost, "/api/pool/batch", bytes.NewReader(raw))
	}
	response := request("local", "disable", []string{"account.json"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unsupported capability status=%d body=%s", response.Code, response.Body.String())
	}
	response = request("accounts", "disable", []string{"dup", "dup"})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate status=%d body=%s", response.Code, response.Body.String())
	}
	ids := make([]string, maxPoolBatchItems+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("id-%d", i)
	}
	response = request("accounts", "disable", ids)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "500") {
		t.Fatalf("limit status=%d body=%s", response.Code, response.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pool/batch", strings.NewReader(`{"source":"accounts","action":"disable","ids":["id"]}`))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Host = "attacker.example"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden || recorder.Header().Get("Access-Control-Allow-Origin") == "*" {
		t.Fatalf("sensitive request status=%d cors=%q body=%s", recorder.Code, recorder.Header().Get("Access-Control-Allow-Origin"), recorder.Body.String())
	}

	for _, target := range []string{
		"/api/pool/list?source=accounts",
		"/api/pool/pull?source=cloud&name=credential.json",
	} {
		req = httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Host = "127.0.0.1:8787"
		req.Header.Set("Origin", "https://attacker.example")
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden || recorder.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Fatalf("target=%s status=%d cors=%q body=%s", target, recorder.Code, recorder.Header().Get("Access-Control-Allow-Origin"), recorder.Body.String())
		}
	}
}

func TestTavilyRefreshAndDeleteShareServerLock(t *testing.T) {
	server, _, _ := newArtifactTestServer(t)
	pool := server.tavilyPool()
	key, err := pool.Add("tvly-batch-lock-aaaaaaaa", "lock test")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.accounts.UpsertTavilyKey(pool.Path, key); err != nil {
		t.Fatal(err)
	}
	account, err := server.accounts.GetByExternal("tavily", key.ID)
	if err != nil {
		t.Fatal(err)
	}

	server.tavilyMu.Lock()
	refreshStarted := make(chan struct{})
	refreshDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		close(refreshStarted)
		refreshDone <- artifactRequest(server, http.MethodGet, "/api/pool/list?source=accounts", nil)
	}()
	<-refreshStarted
	select {
	case <-refreshDone:
		server.tavilyMu.Unlock()
		t.Fatal("Tavily refresh bypassed the server lock")
	case <-time.After(25 * time.Millisecond):
	}
	server.tavilyMu.Unlock()
	response := <-refreshDone
	if response.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", response.Code, response.Body.String())
	}

	server.tavilyMu.Lock()
	deleteStarted := make(chan struct{})
	deleteDone := make(chan error, 1)
	go func() {
		close(deleteStarted)
		deleteDone <- server.applyPoolBatchItem(
			poolBatchRequest{Source: "accounts", Action: "delete"},
			account.ID,
			account,
			nil,
			nil,
			config.Config{},
		)
	}()
	<-deleteStarted
	select {
	case err := <-deleteDone:
		server.tavilyMu.Unlock()
		t.Fatalf("Tavily delete bypassed the server lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	server.tavilyMu.Unlock()
	if err := <-deleteDone; err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Get(key.ID, false); err == nil {
		t.Fatal("deleted Tavily source still exists")
	}
	accounts, err := server.accounts.GetMany([]string{account.ID})
	if err != nil || len(accounts) != 0 {
		t.Fatalf("deleted Tavily index remains: accounts=%v err=%v", accounts, err)
	}
}
