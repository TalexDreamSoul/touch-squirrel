package hunter

import "testing"

func TestClassifyProjectAdapters(t *testing.T) {
	cases := map[string]string{
		`{"message":"CLI Proxy API Server","endpoints":["GET /v1/models"]}`: "cliproxyapi",
		`{"code":0,"data":{"needs_setup":false,"step":"completed"}}`:        "sub2api",
		"Sub2API 一站式开源中转服务":                                                 "sub2api",
	}
	for input, want := range cases {
		if got := ClassifyProduct(input); got != want {
			t.Errorf("ClassifyProduct(%q)=%q want %q", input, got, want)
		}
	}
}
