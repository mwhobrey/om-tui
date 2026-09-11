package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	utilcurl "go.mau.fi/util/curl"
)

// ParseGoogleCookiesInput accepts the same paste shapes as `om-tui pair --google`:
// a JSON object, a copied cURL command with a Cookie header, or a raw Cookie
// header / name=value list. Cookie values are never logged.
func ParseGoogleCookiesInput(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("no cookies provided")
	}

	var cookieMap map[string]string
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal([]byte(trimmed), &cookieMap); err == nil && len(cookieMap) > 0 {
			return cookieMap, nil
		}
	}

	if strings.HasPrefix(trimmed, "curl ") {
		parsed, err := utilcurl.Parse(trimmed)
		if err != nil {
			return nil, fmt.Errorf("parse cURL command: %w", err)
		}
		if cookies, err := parseCookieHeader(parsed.Header.Get("Cookie")); err == nil {
			return cookies, nil
		}
		return nil, fmt.Errorf("cURL command did not include a Cookie header")
	}

	return parseCookieHeader(trimmed)
}

func parseCookieHeader(raw string) (map[string]string, error) {
	header := strings.TrimSpace(raw)
	if header == "" {
		return nil, fmt.Errorf("no cookie header provided")
	}
	if strings.HasPrefix(strings.ToLower(header), "cookie:") {
		header = strings.TrimSpace(header[len("cookie:"):])
	}
	req := &http.Request{Header: make(http.Header)}
	req.Header.Set("Cookie", header)
	parsed := map[string]string{}
	for _, cookie := range req.Cookies() {
		name := strings.TrimSpace(cookie.Name)
		if name == "" {
			continue
		}
		parsed[name] = cookie.Value
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("no cookies found")
	}
	return parsed, nil
}
