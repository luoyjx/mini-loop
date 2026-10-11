package provider

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// The owned clone preserves caller TLS/dial/pool configuration without mutating
// its transport. Custom RoundTrippers receive Source URL/RawPath observations
// and retain ownership of their encoding and routing.
type targetTransport struct {
	base  *http.Transport
	proxy func(*http.Request) (*url.URL, error)
}
type proxyRouteKey struct{}
type resolvedProxy struct{ url *url.URL }

func providerTargetTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	standard, ok := base.(*http.Transport)
	if !ok {
		return base
	}
	owned := standard.Clone()
	transport := &targetTransport{base: owned, proxy: standard.Proxy}
	owned.Proxy = func(request *http.Request) (*url.URL, error) {
		route, ok := request.Context().Value(proxyRouteKey{}).(resolvedProxy)
		if !ok {
			return nil, errors.New("provider proxy route not resolved")
		}
		return route.url, nil
	}
	return transport
}

func (transport *targetTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// Resolve policy once per attempt, before changing the wire-only URL clone.
	var proxy *url.URL
	var err error
	if transport.proxy != nil {
		proxy, err = transport.proxy(request)
		if err != nil {
			return nil, err
		}
	}
	route := resolvedProxy{url: proxy}
	wire := request.Clone(context.WithValue(request.Context(), proxyRouteKey{}, route))
	target := request.URL.RawPath
	wire.URL.Opaque = target
	if request.URL.Scheme == "http" && proxy != nil && (proxy.Scheme == "http" || proxy.Scheme == "https") {
		// net/http owns IDNA/Host/IPv6-zone encoding. Serialize a body-free probe
		// through its WriteProxy codec to retain the existing authority behavior.
		authority := *request.URL
		authority.Path, authority.RawPath, authority.Opaque = "/", "", ""
		probe := &http.Request{Method: http.MethodGet, URL: &authority, Host: request.Host, Header: make(http.Header)}
		var encoded bytes.Buffer
		if err = probe.WriteProxy(&encoded); err != nil {
			return nil, err
		}
		fields := strings.SplitN(encoded.String(), " ", 3)
		if len(fields) != 3 || !strings.HasSuffix(fields[1], "/") {
			return nil, errors.New("invalid provider proxy authority")
		}
		wire.URL.Opaque = strings.TrimSuffix(fields[1], "/") + target
	}
	response, err := transport.base.RoundTrip(wire)
	if response != nil {
		response.Request = request
	}
	return response, err
}
func (transport *targetTransport) CloseIdleConnections() { transport.base.CloseIdleConnections() }
