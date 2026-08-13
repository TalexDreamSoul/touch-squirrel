package hunter

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFOFAClientParsesPassiveResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("email") != "a@example.com" || r.URL.Query().Get("key") != "secret" {
			t.Fatal("missing credentials")
		}
		_, _ = w.Write([]byte(`{"error":false,"results":[["https://ai.example.com","203.0.113.8",443,"https","AI Gateway","nginx","OpenAI API"]]}`))
	}))
	defer srv.Close()
	c := FOFAClient{BaseURL: srv.URL, HTTP: srv.Client()}
	items, err := c.Search(context.Background(), ProviderRequest{Email: "a@example.com", Key: "secret", Query: `title="AI"`, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != "https://ai.example.com" || items[0].Product != "openai-compatible" {
		t.Fatalf("items=%+v", items)
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("dial failed")
}

func TestFOFAApplicationErrorRedactsKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":true,"errmsg":"invalid key top-secret"}`))
	}))
	defer srv.Close()
	c := FOFAClient{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.Search(context.Background(), ProviderRequest{Email: "a@example.com", Key: "top-secret", Query: "ollama", Limit: 10})
	if err == nil || strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("application error leaked key: %v", err)
	}
}

func TestProviderErrorDoesNotLeakCredentialURL(t *testing.T) {
	var out map[string]interface{}
	err := providerGET(context.Background(), &http.Client{Transport: failingTransport{}}, "https://example.com/search?key=top-secret", &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), "example.com") {
		t.Fatalf("credential URL leaked in error: %v", err)
	}
}

func TestProviderHTTPErrorRedactsQuerySecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("request " + r.URL.String() + " rejected"))
	}))
	defer srv.Close()
	var out map[string]interface{}
	err := providerGET(context.Background(), srv.Client(), srv.URL+"?key=top-secret", &out)
	if err == nil || strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestShodanClientParsesPassiveResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"matches":[{"ip_str":"203.0.113.9","port":11434,"transport":"tcp","product":"Ollama","data":"Ollama is running","hostnames":["ollama.example.com"],"http":{"title":"Ollama"}}]}`))
	}))
	defer srv.Close()
	c := ShodanClient{BaseURL: srv.URL, HTTP: srv.Client()}
	items, err := c.Search(context.Background(), ProviderRequest{Key: "secret", Query: "Ollama", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].URL != "http://ollama.example.com:11434" || items[0].Product != "ollama" {
		t.Fatalf("items=%+v", items)
	}
}

func TestMetadataValuesAreRedacted(t *testing.T) {
	secret := "sk-proj-abcdefghijklmnopqrstuvxyz0123456789"
	got := sanitizeMetadata(map[string]string{"server": "token=" + secret})
	if strings.Contains(got["server"], "abcdefghijkl") {
		t.Fatalf("metadata leaked secret: %q", got["server"])
	}
}

func TestIPv6CandidateURLUsesBrackets(t *testing.T) {
	got := normalizeCandidateURL("", "2001:4860:4860::8888", "11434", "http")
	if got != "http://[2001:4860:4860::8888]:11434" {
		t.Fatalf("url=%q", got)
	}
}
