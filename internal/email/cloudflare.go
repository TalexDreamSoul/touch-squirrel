package email

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
)

// cloudflare 移植自 grok-register-panel email_providers/cloudflare.py（Cloudflare Worker 临时邮箱）。
const cloudflareAccountsPath = "/api/new_address"
const cloudflareMessagesPath = "/api/mails"

func (p *Provider) cloudflareBase() string {
	return strings.TrimRight(p.cfg.CloudflareBase, "/")
}

func (p *Provider) cloudflareAuth(req *http.Request) {
	switch strings.ToLower(strings.TrimSpace(p.cfg.CloudflareAuthMode)) {
	case "", "none":
		// 无鉴权
	case "x-api-key":
		req.Header.Set("X-API-Key", p.cfg.CloudflareKey)
	case "x-admin-auth":
		req.Header.Set("x-admin-auth", p.cfg.CloudflareKey)
	case "query-key":
		req.Header.Set("X-API-Key", p.cfg.CloudflareKey)
	default: // bearer
		req.Header.Set("Authorization", "Bearer "+p.cfg.CloudflareKey)
	}
	if p.cfg.CloudflareCustomAuth != "" {
		req.Header.Set("x-custom-auth", p.cfg.CloudflareCustomAuth)
	}
}

func (p *Provider) cloudflareDomain() string {
	domain := strings.TrimSpace(strings.Split(p.cfg.DefaultDomains, ",")[0])
	if domain == "" {
		domain = p.cfg.Domain
	}
	if p.cfg.CloudflareRandomize && domain != "" {
		domain = randomSubdomain(domain)
	}
	return domain
}

func (p *Provider) cloudflareCreate() (Handle, error) {
	base := p.cloudflareBase()
	domain := p.cloudflareDomain()
	name := humanUsername()
	var last error
	for attempt := 0; attempt < 4; attempt++ {
		payload := map[string]any{"name": name, "enablePrefix": false}
		if domain != "" {
			payload["domain"] = domain
		}
		req, err := p.postJSON(base+cloudflareAccountsPath, payload)
		if err != nil {
			return Handle{}, err
		}
		p.cloudflareAuth(req)
		body, status, err := p.doJSON(req)
		if err != nil {
			last = err
			continue
		}
		lower := strings.ToLower(string(body))
		if (status == 400 || status == 409) && strings.Contains(lower, "exists") {
			name = humanUsername() // 地址冲突换名重试
			continue
		}
		if status >= 400 {
			return Handle{}, fmt.Errorf("cloudflare new_address status=%d body=%s", status, truncate(string(body), 120))
		}
		var doc map[string]any
		_ = json.Unmarshal(body, &doc)
		doc = unwrapPayload(doc)
		address := getStr(doc, "address", "email")
		if address == "" {
			return Handle{}, fmt.Errorf("cloudflare no address body=%s", truncate(string(body), 120))
		}
		token := getStr(doc, "jwt", "token")
		return Handle{Kind: "cloudflare", Email: address, Token: token, Base: base}, nil
	}
	if last == nil {
		last = fmt.Errorf("cloudflare create failed")
	}
	return Handle{}, last
}

func (p *Provider) fetchCloudflare(h Handle) (string, error) {
	base := h.Base
	req, _ := http.NewRequest(http.MethodGet, base+cloudflareMessagesPath, nil)
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("Accept", "application/json")
	if p.cfg.CloudflareCustomAuth != "" {
		req.Header.Set("x-custom-auth", p.cfg.CloudflareCustomAuth)
	}
	body, status, err := p.doJSON(req)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("cloudflare messages status=%d", status)
	}
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	doc = unwrapPayload(doc)
	rows := pickList(doc)
	if len(rows) == 0 {
		return "", nil
	}
	id := getStr(rows[0], "id", "message_id")
	if id == "" {
		return "", nil
	}
	// 详情：先试 /api/mail/{id}，再试 /api/mails/{id}
	for _, path := range []string{"/api/mail/" + id, cloudflareMessagesPath + "/" + id} {
		req2, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req2.Header.Set("Authorization", "Bearer "+h.Token)
		if p.cfg.CloudflareCustomAuth != "" {
			req2.Header.Set("x-custom-auth", p.cfg.CloudflareCustomAuth)
		}
		body2, status2, err2 := p.doJSON(req2)
		if err2 != nil {
			continue
		}
		if status2 >= 400 {
			continue
		}
		var d2 map[string]any
		_ = json.Unmarshal(body2, &d2)
		d2 = unwrapPayload(d2)
		if inner, ok := d2["data"].(map[string]any); ok {
			d2 = inner
		}
		out := getStr(d2, "subject") + "\n" +
			getStr(d2, "text", "raw", "content", "intro", "body", "snippet") + "\n" +
			getStr(d2, "html") + "\n"
		if out != "\n\n" {
			return out, nil
		}
	}
	return "", nil
}

// randomSubdomain 在 apex 下生成一次性子域名（移植 common.random_subdomain_domain）。
func randomSubdomain(apex string) string {
	apex = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(apex)), "@")
	if !strings.Contains(apex, ".") {
		return apex
	}
	label := ""
	if rand.Float64() < 0.35 {
		words := []string{"mail", "inbox", "box", "get", "app", "go", "my", "use", "fast", "safe", "home", "note", "post", "send", "hub", "net", "lab", "pro"}
		label = words[rand.Intn(len(words))] + strconv.Itoa(rand.Intn(90)+10)
	} else {
		label = randStr(6 + rand.Intn(5))
	}
	parts := strings.Split(apex, ".")
	if len(parts) >= 3 {
		parts[0] = label
		return strings.Join(parts, ".")
	}
	return label + "." + apex
}
