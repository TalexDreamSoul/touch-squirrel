package tavilypool

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddAcquireLRU(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "keys.json"))
	if _, err := p.Add("tvly-aaaabbbbccccdddd", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Add("tvly-eeeeffffgggghhhh", "b"); err != nil {
		t.Fatal(err)
	}
	k1, err := p.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	k2, err := p.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if k1.ID == k2.ID {
		t.Fatalf("expected different keys on LRU rotate, got %s twice", k1.ID)
	}
	p.ReportFailure(k1.ID, true)
	for range 5 {
		k, err := p.Acquire()
		if err != nil {
			t.Fatal(err)
		}
		if k.ID == k1.ID {
			t.Fatalf("acquired exhausted key %s", k1.ID)
		}
	}
	list, err := p.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("list=%+v", list)
	}
	for _, k := range list {
		if strings.Contains(k.APIKey, "aaaabbbb") || strings.Contains(k.APIKey, "eeeeffff") {
			t.Fatalf("list leaked secret: %q", k.APIKey)
		}
		if !strings.Contains(k.APIKey, "…") && k.APIKey != "****" {
			t.Fatalf("expected masked key, got %q", k.APIKey)
		}
	}
}
