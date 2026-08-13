package email

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// yyds 移植自 grok-register-panel email_providers/yyds.py。
// 鉴权：jwt 优先走 Bearer，否则 api_key 走 X-API-Key。
const yydsBase = "https://maliapi.215.im/v1"

func (p *Provider) yydsAuth(req *http.Request) {
	if p.cfg.YYDSJWT != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.YYDSJWT)
	} else if p.cfg.YYDSKey != "" {
		req.Header.Set("X-API-Key", p.cfg.YYDSKey)
	}
	req.Header.Set("Accept", "application/json")
}

func (p *Provider) yydsCreate() (Handle, error) {
	domain := p.cfg.YYDSDomain
	if domain == "" {
		req, _ := http.NewRequest(http.MethodGet, yydsBase+"/domains", nil)
		p.yydsAuth(req)
		body, _, err := p.doJSON(req)
		if err != nil {
			return Handle{}, err
		}
		var doc struct {
			Success bool `json:"success"`
			Data    []struct {
				Domain     string `json:"domain"`
				IsVerified bool   `json:"isVerified"`
				IsPublic   bool   `json:"isPublic"`
			} `json:"data"`
		}
		_ = json.Unmarshal(body, &doc)
		for _, d := range doc.Data {
			if d.IsVerified && !d.IsPublic {
				domain = d.Domain
				break
			}
		}
		if domain == "" {
			for _, d := range doc.Data {
				if d.Domain != "" && !domainBanned(d.Domain) {
					domain = d.Domain
					break
				}
			}
		}
		if domain == "" {
			return Handle{}, fmt.Errorf("yyds no domain")
		}
	}

	name := humanUsername()
	email := name + "@" + domain
	payload := map[string]any{"localPart": name, "domain": domain, "autoDomainStrategy": "prefer_owned"}
	req, err := p.postJSON(yydsBase+"/accounts", payload)
	if err != nil {
		return Handle{}, err
	}
	p.yydsAuth(req)
	body, status, err := p.doJSON(req)
	if err != nil {
		return Handle{}, err
	}
	if status >= 400 {
		return Handle{}, fmt.Errorf("yyds accounts status=%d body=%s", status, truncate(string(body), 120))
	}
	var doc struct {
		Success bool `json:"success"`
		Data    struct {
			Address string `json:"address"`
			Token   string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &doc)
	if doc.Data.Address != "" {
		email = doc.Data.Address
	}
	token := doc.Data.Token
	if token == "" {
		// 无 token 则单独请求
		req2, _ := p.postJSON(yydsBase+"/token", map[string]string{"address": email})
		p.yydsAuth(req2)
		body2, _, err2 := p.doJSON(req2)
		if err2 == nil {
			var d2 struct {
				Success bool `json:"success"`
				Data    struct {
					Token string `json:"token"`
				} `json:"data"`
			}
			_ = json.Unmarshal(body2, &d2)
			token = d2.Data.Token
		}
	}
	if email == "" {
		return Handle{}, fmt.Errorf("yyds create failed body=%s", truncate(string(body), 120))
	}
	return Handle{Kind: "yyds", Email: email, Token: token, Base: yydsBase}, nil
}

func (p *Provider) fetchYYDS(h Handle) (string, error) {
	token := firstString(h.Token, p.cfg.YYDSJWT)
	u := yydsBase + "/messages?address=" + url.QueryEscape(h.Email)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	body, status, err := p.doJSON(req)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("yyds messages status=%d", status)
	}
	var doc struct {
		Success bool `json:"success"`
		Data    struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &doc)
	if len(doc.Data.Messages) == 0 {
		return "", nil
	}
	id := doc.Data.Messages[0].ID
	req2, _ := http.NewRequest(http.MethodGet, yydsBase+"/messages/"+id, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Accept", "application/json")
	body2, _, err2 := p.doJSON(req2)
	if err2 != nil {
		return "", err2
	}
	var d2 struct {
		Data struct {
			Text    string   `json:"text"`
			HTML    []string `json:"html"`
			Subject string   `json:"subject"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body2, &d2)
	return d2.Data.Subject + "\n" + d2.Data.Text + "\n" + strings.Join(d2.Data.HTML, "\n"), nil
}
