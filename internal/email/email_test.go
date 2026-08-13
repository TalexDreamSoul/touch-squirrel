package email

import (
	"encoding/json"
	"testing"
)

func TestHumanUsername(t *testing.T) {
	for i := 0; i < 50; i++ {
		u := humanUsername()
		if len(u) < 3 || len(u) > 30 {
			t.Fatalf("humanUsername 长度异常: %q", u)
		}
		for _, c := range u {
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '.' && c != '_' && c != '-' {
				t.Fatalf("humanUsername 含非法字符: %q", u)
			}
		}
	}
}

func TestRandomSubdomain(t *testing.T) {
	for i := 0; i < 50; i++ {
		d := randomSubdomain("example.com")
		if d == "example.com" || d == "" {
			t.Fatalf("randomSubdomain 未生成子域: %q", d)
		}
	}
}

func TestPickList(t *testing.T) {
	cases := []string{
		`{"hydra:member":[{"id":"1"}]}`,
		`{"results":[{"id":"2"}]}`,
		`{"data":[{"id":"3"}]}`,
		`{"data":{"messages":[{"id":"4"}]}}`,
		`{"messages":[{"id":"5"}]}`,
	}
	for _, raw := range cases {
		var doc map[string]any
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			t.Fatal(err)
		}
		list := pickList(doc)
		if len(list) != 1 || list[0]["id"] == "" {
			t.Fatalf("pickList 解析失败: %s -> %v", raw, list)
		}
	}
}

func TestUnwrapPayload(t *testing.T) {
	raw := `{"code":200,"data":{"id":"x1","email":"a@b.c"}}`
	var doc map[string]any
	_ = json.Unmarshal([]byte(raw), &doc)
	inner := unwrapPayload(doc)
	if getStr(inner, "id") != "x1" || getStr(inner, "email") != "a@b.c" {
		t.Fatalf("unwrapPayload 失败: %v", inner)
	}
}

func TestExtractCode(t *testing.T) {
	if c := extractCode("您的验证码是 ABC-DEF"); c != "ABCDEF" {
		t.Fatalf("extractCode ABC-DEF -> %q", c)
	}
	if c := extractCode("code: 123456"); c != "123456" {
		t.Fatalf("extractCode 123456 -> %q", c)
	}
}
