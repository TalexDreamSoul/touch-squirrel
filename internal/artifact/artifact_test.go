package artifact

import (
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
}
