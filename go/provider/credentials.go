package provider

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

const OAuthAPIBeta = "oauth-2025-04-20"

// AccessToken owns a validated header value and an optional absolute expiry.
// Formatting never reveals its value. It contains no credential-file handle.
type AccessToken struct {
	value   string
	expiry  time.Time
	expires bool
}

func NewAccessToken(value string, expiry *time.Time) (AccessToken, error) {
	if value == "" || validateAuth(value, nil) != nil {
		return AccessToken{}, &CredentialError{Kind: CredentialInvalid}
	}
	token := AccessToken{value: value}
	if expiry != nil {
		token.expiry = *expiry
		token.expires = true
	}
	return token, nil
}
func (AccessToken) String() string     { return "<access token>" }
func (t AccessToken) GoString() string { return t.String() }

// CredentialProvider must honor context and serialize its own mutable files.
// Force refresh follows explicit invalidation or one token-endpoint 401 retry.
type CredentialProvider interface {
	FetchToken(context.Context, bool) (AccessToken, error)
}

type CredentialErrorKind string

const (
	CredentialExchange    CredentialErrorKind = "exchange"
	CredentialUnavailable CredentialErrorKind = "unavailable"
	CredentialTransport   CredentialErrorKind = "transport"
	CredentialInvalid     CredentialErrorKind = "invalid"
	CredentialCallback    CredentialErrorKind = "callback"
)

// CredentialError carries classification only, never an exchange body/secret.
type CredentialError struct {
	Kind   CredentialErrorKind
	Status int
}

func (*CredentialError) Error() string { return "provider credentials unavailable" }

type credentialCache struct {
	mu              sync.Mutex
	provider        CredentialProvider
	now             func() time.Time
	cached          AccessToken
	present         bool
	refreshing      chan struct{}
	force           bool
	advisoryFailure time.Time
}

func (cache *credentialCache) invalidate() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.present = false
	cache.force = true
}

func (cache *credentialCache) fetch(ctx context.Context, force bool) (token AccessToken, err error) {
	// Release mandatory-window waiters even if an injected provider panics.
	defer func() {
		if recover() != nil {
			token = AccessToken{}
			err = &CredentialError{Kind: CredentialCallback}
		}
	}()
	token, err = cache.provider.FetchToken(ctx, force)
	var failure *CredentialError
	if errors.As(err, &failure) && failure.Kind == CredentialExchange && failure.Status == 401 {
		if ctx.Err() != nil {
			return AccessToken{}, ctx.Err()
		}
		token, err = cache.provider.FetchToken(ctx, true)
	}
	if ctx.Err() != nil {
		return AccessToken{}, ctx.Err()
	}
	if err == nil && (token.value == "" || validateAuth(token.value, nil) != nil) {
		err = &CredentialError{Kind: CredentialInvalid}
	}
	return token, err
}

func (cache *credentialCache) get(ctx context.Context) (AccessToken, error) {
	for {
		if ctx.Err() != nil {
			return AccessToken{}, ctx.Err()
		}
		cache.mu.Lock()
		var fallback AccessToken
		advisory := false
		if cache.present {
			remaining := cache.cached.expiry.Sub(cache.now())
			if !cache.cached.expires || remaining > 120*time.Second {
				token := cache.cached
				cache.mu.Unlock()
				return token, nil
			}
			if remaining > 30*time.Second {
				if cache.refreshing != nil || cache.now().Sub(cache.advisoryFailure) < 5*time.Second {
					token := cache.cached
					cache.mu.Unlock()
					return token, nil
				}
				advisory = true
				fallback = cache.cached
			}
		}
		if wait := cache.refreshing; wait != nil {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return AccessToken{}, ctx.Err()
			case <-wait:
				continue
			}
		}
		released := make(chan struct{})
		cache.refreshing = released
		force := cache.force
		cache.mu.Unlock()
		token, err := cache.fetch(ctx, force)
		cache.mu.Lock()
		cache.refreshing = nil
		if err == nil {
			cache.cached = token
			cache.present = true
			cache.force = false
		}
		var failure *CredentialError
		fallbackAllowed := advisory && errors.As(err, &failure) && (failure.Kind == CredentialExchange || failure.Kind == CredentialUnavailable || failure.Kind == CredentialTransport)
		if fallbackAllowed {
			cache.advisoryFailure = cache.now()
		}
		close(released)
		cache.mu.Unlock()
		if fallbackAllowed {
			return fallback, nil
		}
		return token, err
	}
}

// InvalidateCredentials requests a forced fetch on the next dynamic call.
// It does not cancel a fetch in flight or fence provider-owned external effects.
// Model HTTP 401 does not automatically invalidate/replay a request.
func (c *Client) InvalidateCredentials() {
	if c.credentials != nil {
		c.credentials.invalidate()
	}
}
func (c *StreamingClient) InvalidateCredentials() { c.client.InvalidateCredentials() }

func (c *Client) authenticate(ctx context.Context, h http.Header) (string, error) {
	c.applyHeaders(h)
	if h.Get("Authorization") != "" || h.Get("X-Api-Key") != "" || c.credentials == nil {
		return "", nil
	}
	token, err := c.credentials.get(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		failure := &Failure{class: ClassCredentialProvider, Kind: FailureCredential, Message: "provider credentials unavailable"}
		var credentialError *CredentialError
		if errors.As(err, &credentialError) && credentialError.Kind == CredentialExchange {
			failure.class = ClassWorkloadIdentity
			failure.Status = credentialError.Status
		}
		return "", failure
	}
	h.Set("Authorization", "Bearer "+token.value)
	var flags []string
	for _, raw := range strings.Split(h.Get("Anthropic-Beta"), ",") {
		flag := strings.TrimSpace(raw)
		if flag == OAuthAPIBeta {
			return token.value, nil
		}
		if flag != "" {
			flags = append(flags, flag)
		}
	}
	h.Set("Anthropic-Beta", strings.Join(append(flags, OAuthAPIBeta), ", "))
	return token.value, nil
}
