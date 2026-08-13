package email

import (
	"encoding/json"
	"fmt"
)

// mailnest 移植自 grok-register-panel email_providers/mailnest.py（迈巢 Outlook）。
const mailnestBase = "https://mailnest.top"

func (p *Provider) mailnestCreate() (Handle, error) {
	req, err := p.postJSON(mailnestBase+"/api/v1/email/temporary/buy", map[string]any{
		"project_code": firstString(p.cfg.MailNestProjectCode, "x-ai001"),
		"count":        1,
	})
	if err != nil {
		return Handle{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.MailNestKey)
	body, status, err := p.doJSON(req)
	if err != nil {
		return Handle{}, err
	}
	var doc struct {
		Code string `json:"code"`
		Data []struct {
			Email string `json:"email"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &doc)
	if status >= 400 || doc.Code != "00000" || len(doc.Data) == 0 || doc.Data[0].Email == "" {
		return Handle{}, fmt.Errorf("mailnest buy failed status=%d code=%s body=%s", status, doc.Code, truncate(string(body), 120))
	}
	return Handle{Kind: "mailnest", Email: doc.Data[0].Email}, nil
}

func (p *Provider) fetchMailNest(h Handle) (string, error) {
	req, err := p.postJSON(mailnestBase+"/api/v1/email/receive", map[string]string{"email": h.Email})
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.MailNestKey)
	body, status, err := p.doJSON(req)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("mailnest receive status=%d", status)
	}
	var doc struct {
		Code string `json:"code"`
		Data []struct {
			Subject     string `json:"subject"`
			BodyPreview string `json:"body_preview"`
			Text        string `json:"text"`
			Body        string `json:"body"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &doc)
	var out string
	for _, m := range doc.Data {
		out += firstString(m.Subject, "") + "\n" + firstString(m.BodyPreview, m.Text, m.Body, "") + "\n"
	}
	return out, nil
}
