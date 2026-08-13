package email

import (
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
)

// firstNames / lastNames 移植自 grok-register-panel common.generate_username，
// 用于生成更像真人的邮箱本地部分。
var firstNames = []string{
	"james", "john", "robert", "michael", "david", "william", "richard",
	"mary", "patricia", "jennifer", "linda", "emily", "sarah", "emma",
	"daniel", "matthew", "andrew", "joshua", "ryan", "justin", "brandon",
	"anna", "amy", "olivia", "hannah", "grace", "chloe", "lily", "noah",
}

var lastNames = []string{
	"smith", "johnson", "williams", "brown", "jones", "garcia", "miller",
	"davis", "wilson", "anderson", "thomas", "taylor", "moore", "jackson",
	"martin", "lee", "clark", "lewis", "walker", "hall", "young", "king",
	"wright", "scott", "green", "baker", "adams", "nelson", "carter",
}

// humanUsername 生成真人风邮箱本地部分，并追加 3 位随机后缀避免并发撞名。
func humanUsername() string {
	f := firstNames[rand.Intn(len(firstNames))]
	l := lastNames[rand.Intn(len(lastNames))]
	n := rand.Intn(100)
	patterns := []string{
		f + "." + l,
		f + l,
		f + "." + l + strconv.Itoa(n),
		f + strconv.Itoa(n),
		f + "_" + l,
		f[:1] + l + strconv.Itoa(n),
		f + l[:1] + strconv.Itoa(rand.Intn(90)+10),
	}
	name := patterns[rand.Intn(len(patterns))]
	if len(name) > 24 {
		name = name[:24]
	}
	return name + randStr(3)
}

// firstString 返回第一个非空字符串（用于配置默认值兜底）。
func firstString(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// pickList 从邮件 API 响应中解包列表（移植 common.pick_list_payload）。
// 兼容 hydra:member / member / results / items / data / messages / data.messages 等壳。
func pickList(doc map[string]any) []map[string]any {
	for _, key := range []string{"hydra:member", "member", "results", "items", "messages", "emails", "records", "rows", "list", "domains"} {
		if list, ok := doc[key].([]any); ok {
			return mapsOf(list)
		}
	}
	if data, ok := doc["data"].([]any); ok {
		return mapsOf(data)
	}
	if data, ok := doc["data"].(map[string]any); ok {
		if list, ok := data["messages"].([]any); ok {
			return mapsOf(list)
		}
		if list, ok := data["emails"].([]any); ok {
			return mapsOf(list)
		}
	}
	return nil
}

func mapsOf(list []any) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, it := range list {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// isFalse 判断 JSON 值为显式 false（用于 isActive/isVerified 等可选布尔字段）。
func isFalse(v any) bool {
	b, ok := v.(bool)
	return ok && !b
}

// doJSON 执行请求并读取响应体，返回 body / status / err。
func (p *Provider) doJSON(req *http.Request) ([]byte, int, error) {
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return body, resp.StatusCode, nil
}

// postJSON 构造一个 POST 请求（Content-Type: application/json）。
func (p *Provider) postJSON(url string, payload any) (*http.Request, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// unwrapPayload 解包邮件 API 响应壳（移植 _unwrap_payload）：
// 兼容裸对象 / {data} / {code:200,data} / {success:true,data}。
func unwrapPayload(doc map[string]any) map[string]any {
	if hasAny(doc, "id", "email", "messages", "address", "message") {
		return doc
	}
	if d, ok := doc["data"].(map[string]any); ok {
		return unwrapPayload(d)
	}
	return doc
}

func hasAny(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

// getStr 从 map 中按顺序取第一个非空字符串值。
func getStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
