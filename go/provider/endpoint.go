package provider

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Base has already passed New's endpoint admission. Preserve HTTPX's raw path:
// HTTPX removes literal dot segments, leaving percent-encoded dots opaque, and
// the SDK adds a slash only when absent before appending the relative endpoint.
type endpointTarget struct {
	url         string
	rawPath     string
	literal     bool
	invalidIDNA bool
}

func providerRequestTarget(base *url.URL, configured string) (endpointTarget, error) {
	host, err := providerRequestAuthority(base)
	if err != nil {
		return endpointTarget{}, err
	}
	// New has already validated the URL. Read original path bytes so escapes
	// introduced by net/url cannot erase HTTPX's literal backslash/caret/pipe.
	authority := strings.Index(configured, "://") + 3
	raw := configured[authority:]
	if slash := strings.IndexByte(raw, '/'); slash >= 0 {
		raw = raw[slash:]
	} else {
		raw = ""
	}
	if fragment := strings.IndexByte(raw, '#'); fragment >= 0 {
		raw = raw[:fragment]
	}
	parts := strings.Split(raw, "/")
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
	path = quoteProviderPath(path)
	// Original percent escapes passed parsing; newly quoted bytes are valid too.
	decoded, _ := url.PathUnescape(path)
	target := *base
	target.Host = host
	target.Path, target.RawPath = decoded, path
	literal := strings.ContainsAny(path, "\\^|")
	if literal {
		target.Opaque = "//" + target.Host + path
	}
	return endpointTarget{url: target.String(), rawPath: path, literal: literal,
		invalidIDNA: !providerIDNARequestValid(target.Hostname())}, nil
}

// HTTPX's PATH_SAFE excludes space, quotes, #<>?`{} and non-ASCII bytes.
// Existing percent escapes have already passed URL admission and stay opaque.
func quoteProviderPath(path string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c < 0x20 || c >= 0x7f || strings.ContainsRune(" \"#<>?`{}", rune(c)) {
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&15])
		} else {
			out.WriteByte(c)
		}
	}
	return out.String()
}

func (target endpointTarget) request(ctx context.Context, body []byte) (*http.Request, error) {
	if target.invalidIDNA {
		return nil, errors.New("invalid provider IDNA hostname")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.URL.RawPath = target.rawPath
	if target.literal {
		request.URL.Opaque = "//" + request.URL.Host + target.rawPath
	}
	return request, nil
}

// Normalize the accepted authority at the request boundary. IPv6 literals and
// zone identifiers retain their spelling; configured audit identity is separate.
func providerRequestAuthority(base *url.URL) (string, error) {
	host := base.Hostname()
	ipv6 := strings.Contains(host, ":")
	if !ipv6 {
		var err error
		host, err = providerEncodedHostname(host)
		if err != nil {
			return "", err
		}
	}
	port := strings.TrimLeft(base.Port(), "0")
	if base.Port() != "" && port == "" {
		port = "0"
	}
	if (base.Scheme == "https" && port == "443") || (base.Scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return net.JoinHostPort(host, port), nil
	}
	if ipv6 {
		return "[" + host + "]", nil
	}
	return host, nil
}
