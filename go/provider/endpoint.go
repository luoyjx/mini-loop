package provider

import (
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
	target.Path, target.RawPath = decoded, path
	return target.String()
}
