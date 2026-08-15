package tavilypool

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPoolInstancesShareFileLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	first := New(path)
	keyA, err := first.Add("tvly-lock-aaaaaaaa", "a")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := first.Add("tvly-lock-bbbbbbbb", "b")
	if err != nil {
		t.Fatal(err)
	}
	second := New(path)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errors <- first.SetStatus(keyA.ID, StatusDisabled)
	}()
	go func() {
		defer wg.Done()
		errors <- second.SetStatus(keyB.ID, StatusExhausted)
	}()
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	gotA, err := second.Get(keyA.ID, true)
	if err != nil || gotA.Status != StatusDisabled {
		t.Fatalf("key A lost update: key=%+v err=%v", gotA, err)
	}
	gotB, err := first.Get(keyB.ID, true)
	if err != nil || gotB.Status != StatusExhausted {
		t.Fatalf("key B lost update: key=%+v err=%v", gotB, err)
	}
}

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
	secret, err := p.Get(k1.ID, false)
	if err != nil || secret.APIKey == "" || strings.Contains(secret.APIKey, "…") {
		t.Fatalf("secret lookup failed: key=%+v err=%v", secret, err)
	}
	for _, k := range list {
		if strings.Contains(k.APIKey, "aaaabbbb") || strings.Contains(k.APIKey, "eeeeffff") {
			t.Fatalf("list leaked secret: %q", k.APIKey)
		}
		if !strings.Contains(k.APIKey, "…") && k.APIKey != "****" {
			t.Fatalf("expected masked key, got %q", k.APIKey)
		}
	}
	if err := p.Delete(k1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(k1.ID, false); err == nil {
		t.Fatal("expected deleted key to be missing")
	}
}
