package email

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// moemail 移植自 grok-register-panel email_providers/moemail.py。
// 鉴权统一 X-API-Key；收信靠 email_id（非 token）。
func (p *Provider) moemailBase() string {
	base := strings.TrimRight(p.cfg.MoeMailBase, "/")
	for _, suf := range []string{"/api/v1", "/api"} {
		if strings.HasSuffix(base, suf) {
			base = strings.TrimSuffix(base, suf)
			break
		}
	}
	return base
}

func (p *Provider) moemailAuth(req *http.Request) {
	req.Header.Set("X-API-Key", p.cfg.MoeMailKey)
	req.Header.Set("Accept", "application/json")
}

func (p *Provider) moemailCreate() (Handle, error) {
	base := p.moemailBase()
	domain := p.cfg.MoeMailDomain
	if domain == "" {
		req, _ := http.NewRequest(http.MethodGet, base+"/api/config", nil)
		p.moemailAuth(req)
		body, _, err := p.doJSON(req)
		if err != nil {
			return Handle{}, err
		}
		var cfgDoc map[string]any
		_ = json.Unmarshal(body, &cfgDoc)
		cfgDoc = unwrapPayload(cfgDoc)
		if doms := getStr(cfgDoc, "emailDomains"); doms != "" {
			domain = strings.TrimSpace(strings.Split(doms, ",")[0])
		}
	}

	expiry := p.cfg.MoeMailExpiryMS
	if expiry == 0 {
		expiry = 3600000
	}
	req, err := p.postJSON(base+"/api/emails/generate", map[string]any{
		"name":       humanUsername(),
		"expiryTime": expiry,
		"domain":     domain,
	})
	if err != nil {
		return Handle{}, err
	}
	p.moemailAuth(req)
	body, status, err := p.doJSON(req)
	if err != nil {
		return Handle{}, err
	}
	if status >= 400 {
		return Handle{}, fmt.Errorf("moemail generate status=%d body=%s", status, truncate(string(body), 120))
	}
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	doc = unwrapPayload(doc)
	email := getStr(doc, "email", "address")
	id := getStr(doc, "id", "emailId")
	if email == "" || id == "" {
		return Handle{}, fmt.Errorf("moemail generate no email/id body=%s", truncate(string(body), 160))
	}
	return Handle{Kind: "moemail", Email: email, ID: id, Base: base}, nil
}

func (p *Provider) fetchMoeMail(h Handle) (string, error) {
	base := h.Base
	req, _ := http.NewRequest(http.MethodGet, base+"/api/emails/"+h.ID+"?cursor=", nil)
	p.moemailAuth(req)
	body, status, err := p.doJSON(req)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("moemail messages status=%d", status)
	}
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	doc = unwrapPayload(doc)
	messages := pickList(doc)
	if len(messages) == 0 {
		return "", nil
	}
	msgID := getStr(messages[0], "id", "message_id")
	if msgID == "" {
		return "", nil
	}
	req2, _ := http.NewRequest(http.MethodGet, base+"/api/emails/"+h.ID+"/"+msgID, nil)
	p.moemailAuth(req2)
	body2, _, err2 := p.doJSON(req2)
	if err2 != nil {
		return "", err2
	}
	var d2 map[string]any
	_ = json.Unmarshal(body2, &d2)
	d2 = unwrapPayload(d2)
	if m, ok := d2["message"].(map[string]any); ok {
		d2 = m
	}
	return getStr(d2, "subject") + "\n" + getStr(d2, "content", "text", "body") + "\n" + getStr(d2, "html"), nil
}
