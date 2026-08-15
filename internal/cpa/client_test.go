package cpa

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthFileNameValidation(t *testing.T) {
	for _, name := range []string{"account.json", "xai-123.json"} {
		if !validAuthFileName(name) {
			t.Fatalf("valid name rejected: %q", name)
		}
	}
	for _, name := range []string{"../account.json", `..\\account.json`, "folder/account.json", "account.txt", "bad\nname.json"} {
		if validAuthFileName(name) {
			t.Fatalf("unsafe name accepted: %q", name)
		}
	}
}

func TestDownloadRejectsOversizedCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 1<<20)
		for range 17 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, "test-key", 10)
	if _, err := client.Download("large.json"); err == nil {
		t.Fatal("expected oversized download error")
	}
}
