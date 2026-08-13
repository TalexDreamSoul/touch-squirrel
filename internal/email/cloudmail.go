package email

import (
	"encoding/json"
	"fmt"
	"strings"
)

// cloudmail 移植自 grok-register-panel email_providers/cloudmail.py（maillab/cloud-mail）。
// 统一响应壳 {"code":200,"data":...}；token 用裸值（无 Bearer 前缀）；catch-all 收信。
func (p *Provider) cloudmailURL() string {
	return strings.TrimRight(p.cfg.CloudMailURL, "/")
}

func (p *Provider) cloudmailLogin() (string, error) {
	req, err := p.postJSON(p.cloudmailURL()+"/api/login", map[string]string{
		"email":    p.cfg.CloudMailAdminEmail,
		"password": p.cfg.CloudMailPassword,
	})
	if err != nil {
		return "", err
	}
	body, status, err := p.doJSON(req)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("cloudmail login status=%d", status)
	}
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	tok := getStr(unwrapPayload(doc), "token")
	if tok == "" {
		return "", fmt.Errorf("cloudmail login no token body=%s", truncate(string(body), 120))
	}
	return tok, nil
}

func (p *Provider) cloudmailCreate() (Handle, error) {
	token, err := p.cloudmailLogin()
	if err != nil {
		return Handle{}, err
	}
	address := humanUsername() + "@" + firstString(p.cfg.Domain, strings.TrimSpace(strings.Split(p.cfg.DefaultDomains, ",")[0]), "example.com")
	req, err := p.postJSON(p.cloudmailURL()+"/api/account/add", map[string]string{
		"email": address,
		"token": "",
	})
	if err != nil {
		return Handle{}, err
	}
	req.Header.Set("Authorization", token) // 裸 token，无 Bearer
	body, status, err := p.doJSON(req)
	if err != nil {
		return Handle{}, err
	}
	if status >= 400 {
		return Handle{}, fmt.Errorf("cloudmail account/add status=%d body=%s", status, truncate(string(body), 120))
	}
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	accountID := getStr(unwrapPayload(doc), "accountId", "account_id", "id")
	// catch-all：无独立 token，靠 admin 公共 token + toEmail 过滤收信。
	return Handle{Kind: "cloudmail", Email: address, Password: p.cfg.CloudMailPassword, ID: accountID, Base: p.cloudmailURL()}, nil
}

func (p *Provider) fetchCloudMail(h Handle) (string, error) {
	// 公共 token（genToken），按 toEmail 过滤取信。
	req, err := p.postJSON(h.Base+"/api/public/genToken", map[string]string{
		"email":    h.Email,
		"password": h.Password,
	})
	if err != nil {
		return "", err
	}
	body, status, err := p.doJSON(req)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("cloudmail genToken status=%d", status)
	}
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	pubToken := getStr(unwrapPayload(doc), "token")
	if pubToken == "" {
		return "", fmt.Errorf("cloudmail genToken no token")
	}

	req2, err := p.postJSON(h.Base+"/api/public/emailList", map[string]any{
		"size":    20,
		"toEmail": h.Email,
	})
	if err != nil {
		return "", err
	}
	req2.Header.Set("Authorization", pubToken) // 裸 token
	body2, status2, err := p.doJSON(req2)
	if err != nil {
		return "", err
	}
	if status2 >= 400 {
		return "", fmt.Errorf("cloudmail emailList status=%d", status2)
	}
	var d2 map[string]any
	_ = json.Unmarshal(body2, &d2)
	rows := pickList(unwrapPayload(d2))
	var out string
	for _, m := range rows {
		out += getStr(m, "subject") + "\n" +
			getStr(m, "content", "text", "textContent", "text_content", "body", "snippet", "intro") + "\n" +
			getStr(m, "html", "htmlContent") + "\n"
	}
	return out, nil
}
