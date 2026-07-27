package notify

import (
	"path/filepath"
	"testing"
)

func TestCreateListUpdateDelete(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "notifications.json"))
	c, err := st.Create(Channel{
		Name:    "飞书告警",
		Kind:    KindFeishu,
		Enabled: true,
		Events:  []string{"register.finished"},
		Config: map[string]string{
			"webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/demo",
			"secret":      "s3cret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Config["secret"] != "••••" {
		t.Fatalf("mask fail: %+v", c)
	}
	list, err := st.List(true)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	raw, err := st.Get(c.ID, false)
	if err != nil || raw.Config["secret"] != "s3cret" {
		t.Fatalf("raw=%+v err=%v", raw, err)
	}
	// update with masked secret must keep original
	_, err = st.Update(c.ID, Channel{
		Name:    "飞书告警2",
		Kind:    KindFeishu,
		Enabled: false,
		Events:  []string{"system.test"},
		Config: map[string]string{
			"webhook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/demo",
			"secret":      "••••",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := st.Get(c.ID, false)
	if raw2.Config["secret"] != "s3cret" || raw2.Name != "飞书告警2" || raw2.Enabled {
		t.Fatalf("update bad: %+v", raw2)
	}
	if err := st.Delete(c.ID); err != nil {
		t.Fatal(err)
	}
	list, _ = st.List(false)
	if len(list) != 0 {
		t.Fatalf("not empty: %v", list)
	}
}

func TestValidateSMTP(t *testing.T) {
	st := New(filepath.Join(t.TempDir(), "n.json"))
	_, err := st.Create(Channel{Name: "m", Kind: KindSMTP, Config: map[string]string{}})
	if err == nil {
		t.Fatal("expected validation error")
	}
	_, err = st.Create(Channel{
		Name: "m",
		Kind: KindSMTP,
		Config: map[string]string{
			"host": "smtp.example.com",
			"from": "a@x.com",
			"to":   "b@x.com",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
