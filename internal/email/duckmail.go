package email

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// duckmail 移植自 grok-register-panel email_providers/duckmail.py：
// DuckMail / Mail.tm 兼容临时邮箱，支持自定义 base 与 api_key 鉴权。
const duckmailDefaultBase = "https://api.duckmail.sbs"

func (p *Provider) duckmailBase() string {
	return strings.TrimRight(firstString(p.cfg.DuckMailBase, duckmailDefaultBase), "/")
}

// duckmailAuth 给请求加 DuckMail 鉴权头。mail.tm 公共域不带 key；
// DuckMail 私有域（配置了 api_key）走 Authorization: Bearer <key>。
func (p *Provider) duckmailAuth(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if p.cfg.DuckMailKey != "" && !strings.Contains(p.duckmailBase(), "mail.tm") {
		req.Header.Set("Authorization", "Bearer "+p.cfg.DuckMailKey)
	}
}

func (p *Provider) duckmailCreate() (Handle, error) {
	base := p.duckmailBase()
	password := randStr(15)
	domains, err := p.duckmailDomains(base)
	if err != nil {
		return Handle{}, err
	}
	name := humanUsername()
	var last error
	for _, dom := range domains {
		email := name + "@" + dom
		// DuckMail 私有域带 expiresIn；返回 400 时去掉 expiresIn 重发（兼容 mail.tm）。
		if err := p.duckmailCreateAccount(base, email, password); err != nil {
			last = err
			continue
		}
		payload, _ := json.Marshal(map[string]string{"address": email, "password": password})
		req, _ := http.NewRequest(http.MethodPost, base+"/token", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		p.duckmailAuth(req)
		resp, err := p.cfg.HTTPClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		tb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		var tokDoc map[string]any
		_ = json.Unmarshal(tb, &tokDoc)
		tok, _ := tokDoc["token"].(string)
		if tok == "" {
			last = fmt.Errorf("duckmail no token")
			continue
		}
		return Handle{Kind: "duckmail", Email: email, Password: password, Token: tok, Base: base}, nil
	}
	if last == nil {
		last = fmt.Errorf("duckmail create failed")
	}
	return Handle{}, last
}

func (p *Provider) duckmailCreateAccount(base, email, password string) error {
	withExpiry, _ := json.Marshal(map[string]any{"address": email, "password": password, "expiresIn": 3600})
	req, _ := http.NewRequest(http.MethodPost, base+"/accounts", strings.NewReader(string(withExpiry)))
	req.Header.Set("Content-Type", "application/json")
	p.duckmailAuth(req)
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		// 去掉 expiresIn 重试
		plain, _ := json.Marshal(map[string]string{"address": email, "password": password})
		req2, _ := http.NewRequest(http.MethodPost, base+"/accounts", strings.NewReader(string(plain)))
		req2.Header.Set("Content-Type", "application/json")
		p.duckmailAuth(req2)
		resp2, err := p.cfg.HTTPClient.Do(req2)
		if err != nil {
			return err
		}
		resp2.Body.Close()
		if resp2.StatusCode >= 400 {
			return fmt.Errorf("duckmail accounts status=%d", resp2.StatusCode)
		}
	}
	return nil
}

func (p *Provider) duckmailDomains(base string) ([]string, error) {
	req, _ := http.NewRequest(http.MethodGet, base+"/domains", nil)
	p.duckmailAuth(req)
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	members := pickList(doc)
	var doms []string
	for _, m := range members {
		d, _ := m["domain"].(string)
		if d == "" || domainBanned(d) {
			continue
		}
		if isFalse(m["isActive"]) {
			continue
		}
		if priv, _ := m["isPrivate"].(bool); priv {
			continue
		}
		doms = append(doms, d)
	}
	if len(doms) == 0 {
		return nil, fmt.Errorf("no domain from %s", base)
	}
	return doms, nil
}

// fetchDuckMail 与 mail.tm 取信路径一致（messages → messages/{id}），复用 fetchMailTM。
func (p *Provider) fetchDuckMail(h Handle) (string, error) {
	return p.fetchMailTM(h)
}
