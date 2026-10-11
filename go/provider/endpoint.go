package provider

import (
	"net"
	"net/url"
	"strings"
)

// Base has already passed New's endpoint admission. Work on its escaped path:
// HTTPX removes literal dot segments, leaving percent-encoded dots opaque, and
// the SDK adds a slash only when absent before appending the relative endpoint.
func providerRequestURL(base *url.URL) string {
	parts := strings.Split(base.EscapedPath(), "/")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case ".":
		case "..":
			if len(normalized) > 1 || (len(normalized) == 1 && normalized[0] != "") {
				normalized = normalized[:len(normalized)-1]
			}
		default:
			normalized = append(normalized, part)
		}
	}
	path := strings.Join(normalized, "/")
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	path += "v1/messages"
	// EscapedPath comes from a parsed URL; normalization removes only literal
	// segments and preserves every percent escape, so unescaping cannot fail.
	decoded, _ := url.PathUnescape(path)
	target := *base
	target.Host = providerRequestAuthority(base)
	target.Path, target.RawPath = decoded, path
	return target.String()
}

// Normalize the accepted authority at the request boundary. IPv6 literals and
// zone identifiers retain their spelling; configured audit identity is separate.
func providerRequestAuthority(base *url.URL) string {
	host := base.Hostname()
	ipv6 := strings.Contains(host, ":")
	ascii := true
	for i := 0; i < len(host); i++ {
		if host[i] >= 0x80 {
			ascii = false
			break
		}
	}
	// Unicode lowercase mapping is contextual in Python (and can expand a
	// character). Preserve it for transport IDNA instead of Go's rune mapping.
	if !ipv6 && ascii {
		host = strings.ToLower(host)
	}
	port := strings.TrimLeft(base.Port(), "0")
	if base.Port() != "" && port == "" {
		port = "0"
	}
	if (base.Scheme == "https" && port == "443") || (base.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return net.JoinHostPort(host, port)
	}
	if ipv6 {
		return "[" + host + "]"
	}
	return host
}
