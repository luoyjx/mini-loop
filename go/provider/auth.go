package provider

import (
	"errors"
	"net/http"
	"sort"
	"strings"
)

// Header is an explicit request header. New captures the ordered slice;
// duplicate names follow SDK last-value precedence. No environment is read.
type Header struct {
	Name  string
	Value string `json:"-"`
}

func validateAuth(token string, headers []Header) error {
	if len(token) > 4096 || strings.TrimSpace(token) != token || strings.IndexFunc(token, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return errors.New("provider auth token must be a single header value")
	}
	if len(headers) > 128 {
		return errors.New("provider custom headers exceed limit")
	}
	for _, h := range headers {
		if h.Name == "" || len(h.Name) > 256 || strings.IndexFunc(h.Name, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r))
		}) >= 0 || len(h.Value) > 8192 || strings.IndexFunc(h.Value, func(r rune) bool { return r < 32 && r != '\t' || r == 127 }) >= 0 {
			return errors.New("provider custom header is invalid")
		}
	}
	return nil
}

func (c *Client) applyHeaders(h http.Header) {
	if c.key != "" {
		h.Set("X-Api-Key", c.key)
	}
	if c.authToken != "" {
		h.Set("Authorization", "Bearer "+c.authToken)
	}
	for _, header := range c.headers {
		h.Set(header.Name, header.Value)
	}
}

func (c *Client) redactCredentials(text string) string {
	values := []string{c.key, c.authToken, c.activeToken}
	for _, h := range c.headers {
		if strings.EqualFold(h.Name, "X-Api-Key") || strings.EqualFold(h.Name, "Authorization") {
			values = append(values, h.Value)
			if strings.HasPrefix(h.Value, "Bearer ") {
				values = append(values, strings.TrimPrefix(h.Value, "Bearer "))
			}
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		if value != "" {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	return text
}
