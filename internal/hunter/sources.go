package hunter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ProviderRequest struct {
	Email string
	Key   string
	Query string
	Limit int
}

type Candidate struct {
	URL      string
	Host     string
	IP       string
	Source   string
	Query    string
	Product  string
	Title    string
	Banner   string
	Evidence []Evidence
	Metadata map[string]string
}

type PassiveProvider interface {
	Search(context.Context, ProviderRequest) ([]Candidate, error)
}

type FOFAClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c FOFAClient) Search(ctx context.Context, in ProviderRequest) ([]Candidate, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://fofa.info/api/v1/search/all"
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("email", in.Email)
	q.Set("key", in.Key)
	q.Set("qbase64", base64.StdEncoding.EncodeToString([]byte(in.Query)))
	q.Set("fields", "host,ip,port,protocol,title,server,banner")
	q.Set("size", strconv.Itoa(normalizeLimit(in.Limit)))
	u.RawQuery = q.Encode()
	var payload struct {
		Error   bool            `json:"error"`
		Message string          `json:"errmsg"`
		Results [][]interface{} `json:"results"`
	}
	if err := providerGET(ctx, c.HTTP, u.String(), &payload); err != nil {
		return nil, err
	}
	if payload.Error {
		return nil, fmt.Errorf("fofa: %s", sanitizeProviderError(payload.Message, u.String()))
	}
	out := make([]Candidate, 0, len(payload.Results))
	for _, row := range payload.Results {
		if len(row) < 4 {
			continue
		}
		hostURL := valueString(row[0])
		ip := valueString(row[1])
		port := valueString(row[2])
		protocol := valueString(row[3])
		title := valueAt(row, 4)
		server := valueAt(row, 5)
		banner := valueAt(row, 6)
		target := normalizeCandidateURL(hostURL, ip, port, protocol)
		if target == "" {
			continue
		}
		host := candidateHost(target)
		text := strings.Join([]string{title, server, banner}, " ")
		out = append(out, Candidate{URL: target, Host: host, IP: ip, Source: "fofa", Query: in.Query, Product: ClassifyProduct(text), Title: RedactText(title), Banner: RedactText(limitText(banner, 512)), Metadata: sanitizeMetadata(map[string]string{"server": server})})
	}
	return out, nil
}

type ShodanClient struct {
	BaseURL string
	HTTP    *http.Client
}

func (c ShodanClient) Search(ctx context.Context, in ProviderRequest) ([]Candidate, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.shodan.io/shodan/host/search"
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("key", in.Key)
	q.Set("query", in.Query)
	u.RawQuery = q.Encode()
	var payload struct {
		Error   string `json:"error"`
		Matches []struct {
			IP        string   `json:"ip_str"`
			Port      int      `json:"port"`
			Transport string   `json:"transport"`
			Product   string   `json:"product"`
			Data      string   `json:"data"`
			Hostnames []string `json:"hostnames"`
			HTTP      struct {
				Title string `json:"title"`
			} `json:"http"`
		} `json:"matches"`
	}
	if err := providerGET(ctx, c.HTTP, u.String(), &payload); err != nil {
		return nil, err
	}
	if payload.Error != "" {
		return nil, fmt.Errorf("shodan: %s", sanitizeProviderError(payload.Error, u.String()))
	}
	limit := normalizeLimit(in.Limit)
	out := make([]Candidate, 0, min(len(payload.Matches), limit))
	for _, m := range payload.Matches {
		if len(out) >= limit {
			break
		}
		host := m.IP
		if len(m.Hostnames) > 0 && strings.TrimSpace(m.Hostnames[0]) != "" {
			host = m.Hostnames[0]
		}
		scheme := "http"
		if m.Port == 443 || m.Port == 8443 {
			scheme = "https"
		}
		target := scheme + "://" + formatURLHost(host, m.Port, scheme)
		text := strings.Join([]string{m.Product, m.HTTP.Title, m.Data}, " ")
		out = append(out, Candidate{URL: target, Host: host, IP: m.IP, Source: "shodan", Query: in.Query, Product: ClassifyProduct(text), Title: RedactText(m.HTTP.Title), Banner: RedactText(limitText(m.Data, 512)), Metadata: sanitizeMetadata(map[string]string{"transport": m.Transport, "product": m.Product})})
	}
	return out, nil
}

func providerGET(ctx context.Context, client *http.Client, target string, out any) error {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("provider request failed: %v", urlErr.Err)
		}
		return fmt.Errorf("provider request failed")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("provider http %d: %s", res.StatusCode, sanitizeProviderError(string(body), target))
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}

func normalizeCandidateURL(hostURL, ip, port, protocol string) string {
	if u, err := url.Parse(hostURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" {
		u.Path, u.RawQuery, u.Fragment, u.User = "", "", "", nil
		return strings.TrimRight(u.String(), "/")
	}
	host := strings.TrimSpace(ip)
	if host == "" {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(protocol))
	if scheme != "https" {
		scheme = "http"
	}
	target := scheme + "://" + formatURLHost(host, parsePort(port), scheme)
	return target
}

func formatURLHost(host string, port int, scheme string) string {
	defaultPort := (scheme == "http" && port == 80) || (scheme == "https" && port == 443) || port == 0
	if defaultPort {
		if ip := net.ParseIP(host); ip != nil && strings.Contains(host, ":") {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func parsePort(raw string) int {
	port, _ := strconv.Atoi(raw)
	return port
}

func sanitizeProviderError(body, target string) string {
	body = RedactText(limitText(body, 512))
	if u, err := url.Parse(target); err == nil {
		for _, values := range u.Query() {
			for _, value := range values {
				if value != "" {
					body = strings.ReplaceAll(body, value, "[redacted]")
				}
			}
		}
	}
	if strings.TrimSpace(body) == "" {
		return "request rejected"
	}
	return body
}

func sanitizeMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = RedactText(limitText(value, 256))
	}
	return out
}

func candidateHost(raw string) string {
	u, _ := url.Parse(raw)
	return u.Hostname()
}

func valueString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	default:
		return fmt.Sprint(x)
	}
}

func valueAt(row []interface{}, idx int) string {
	if idx >= len(row) {
		return ""
	}
	return valueString(row[idx])
}

func normalizeLimit(v int) int {
	if v <= 0 {
		return 50
	}
	if v > 1000 {
		return 1000
	}
	return v
}

func limitText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
