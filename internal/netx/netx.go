package netx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KasumiYuku/Aurorix/lib/requests"
)

type Client struct {
	http    *requests.Client
	headers map[string]string
}

func New(timeout int, headers map[string]string) *Client {
	return &Client{http: requests.Init(timeout), headers: headers}
}

func (c *Client) merge(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return c.headers
	}
	if len(c.headers) == 0 {
		return headers
	}
	merged := make(map[string]string, len(c.headers)+len(headers))
	for k, v := range c.headers {
		merged[k] = v
	}
	for k, v := range headers {
		merged[k] = v
	}
	return merged
}

func (c *Client) Bytes(method, rawURL string, body requests.Body, headers map[string]string) ([]byte, error) {
	return c.http.DoBytes(method, rawURL, body, c.merge(headers))
}

func (c *Client) Get(rawURL string, headers map[string]string) ([]byte, error) {
	return c.Bytes(http.MethodGet, rawURL, nil, headers)
}

func (c *Client) GetText(rawURL string, headers map[string]string) (string, error) {
	raw, err := c.Get(rawURL, headers)
	return strings.TrimSpace(string(raw)), err
}

func (c *Client) PostForm(rawURL string, form map[string][]string, headers map[string]string) ([]byte, error) {
	return c.Bytes(http.MethodPost, rawURL, requests.Form(form), headers)
}
func (c *Client) PostJSON(rawURL string, payload any, headers map[string]string) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return c.Bytes(http.MethodPost, rawURL, requests.Bytes(raw), headers)
}

func (c *Client) GetJSON(rawURL string, out any, headers map[string]string) error {
	raw, err := c.Get(rawURL, headers)
	if err != nil {
		return err
	}
	return Decode(raw, out)
}

func (c *Client) PostJSONDecode(rawURL string, payload, out any, headers map[string]string) error {
	raw, err := c.PostJSON(rawURL, payload, headers)
	if err != nil {
		return err
	}
	return Decode(raw, out)
}

func Decode(raw []byte, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("上游返回空正文")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("上游返回的不是 JSON: %w", err)
	}
	return nil
}

func DecodeJSONP(raw []byte, out any) error {
	text := strings.TrimSpace(string(raw))
	if i := strings.IndexByte(text, '('); i >= 0 {
		text = text[i+1:]
	}
	if i := strings.LastIndexByte(text, ')'); i >= 0 {
		text = text[:i]
	}
	return Decode([]byte(text), out)
}

func FinalURL(rawURL string, headers map[string]string) (string, error) {
	req, err := http.NewRequest(http.MethodHead, rawURL, nil)
	if err != nil {
		return "", err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	return resp.Request.URL.String(), nil
}

func Escaped(values map[string]string) string {
	parts := make([]string, 0, len(values))
	for k, v := range values {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "&")
}
