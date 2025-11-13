package httputil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NewHTTPClient returns an http.Client with sane defaults.
func NewHTTPClient(timeoutSeconds int) *http.Client {
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// NormalizeEndpoint chooses the effective endpoint or fallback.
func NormalizeEndpoint(base, fallback string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = fallback
	}
	return strings.TrimRight(base, "/")
}

// DecodeContent converts a raw JSON content field into text.
func DecodeContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if strings.EqualFold(part.Type, "text") {
				b.WriteString(part.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	var obj struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Text != "" {
		return obj.Text
	}
	return string(raw)
}

// ReadBodyError formats an HTTP error from the response body.
func ReadBodyError(resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("http error: no response")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	body := strings.TrimSpace(string(data))
	if body == "" {
		body = resp.Status
	}
	return fmt.Errorf("http %d: %s", resp.StatusCode, body)
}
