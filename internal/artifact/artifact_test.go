package artifact

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestPutList(t *testing.T) {
	root := t.TempDir()
	st := NewStore(filepath.Join(root, "artifacts"))
	a, err := st.PutJSON("xai-accounts", "oauth.token", StatusFresh, map[string]string{"email": "a@b.c"}, map[string]any{
		"access_token": "tok",
	}, "run1")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Plugin != "xai-accounts" {
		t.Fatalf("%+v", a)
	}
	list, err := st.List("xai-accounts", "oauth.token", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Labels["email"] != "a@b.c" {
		t.Fatalf("%+v", list)
	}
	got, err := st.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID {
		t.Fatalf("artifact=%+v", got)
	}
	var payload map[string]string
	if err := json.Unmarshal(got.Payload, &payload); err != nil || payload["access_token"] != "tok" {
		t.Fatalf("payload=%s err=%v", got.Payload, err)
	}
	if _, err := st.Get("missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
