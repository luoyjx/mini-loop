package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type credentialFunc func(context.Context, bool) (AccessToken, error)

func (f credentialFunc) FetchToken(ctx context.Context, force bool) (AccessToken, error) {
	return f(ctx, force)
}
func fixtureToken(t *testing.T, value string, expiry *time.Time) AccessToken {
	t.Helper()
	token, err := NewAccessToken(value, expiry)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestCredentialCacheSDKWindowsBackoffAndForcedRetry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	expiry := now.Add(300 * time.Second)
	calls := 0
	fail := false
	cache := credentialCache{now: func() time.Time { return now }, advisoryFailure: time.Unix(0, 0)}
	cache.provider = credentialFunc(func(_ context.Context, force bool) (AccessToken, error) {
		calls++
		if fail {
			return AccessToken{}, &CredentialError{Kind: CredentialTransport}
		}
		return fixtureToken(t, "cached-token", &expiry), nil
	})
	ctx := context.Background()
	if _, err := cache.get(ctx); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	now = now.Add(179 * time.Second) // 121s: fresh, no fetch.
	if _, err := cache.get(ctx); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	now = now.Add(time.Second) // 120s: advisory fetch, recoverable failure falls back.
	fail = true
	if _, err := cache.get(ctx); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	now = now.Add(4 * time.Second)
	if _, err := cache.get(ctx); err != nil || calls != 2 {
		t.Fatal(err, calls)
	}
	now = now.Add(time.Second)
	if _, err := cache.get(ctx); err != nil || calls != 3 {
		t.Fatal(err, calls)
	}
	now = expiry.Add(-30 * time.Second) // 30s: mandatory failure surfaces.
	if _, err := cache.get(ctx); err == nil || calls != 4 {
		t.Fatal(err, calls)
	}
	var forces []bool
	cache.provider = credentialFunc(func(_ context.Context, force bool) (AccessToken, error) {
		forces = append(forces, force)
		if len(forces) == 1 {
			return AccessToken{}, &CredentialError{Kind: CredentialExchange, Status: 401}
		}
		return fixtureToken(t, "refreshed-token", nil), nil
	})
	cache.invalidate()
	if _, err := cache.get(ctx); err != nil || len(forces) != 2 || !forces[0] || !forces[1] {
		t.Fatal(err, forces)
	}
}

func TestCredentialCacheSingleFlightCancellationAndPanicRelease(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	cache := credentialCache{now: time.Now, provider: credentialFunc(func(ctx context.Context, _ bool) (AccessToken, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return fixtureToken(t, "shared-token", nil), nil
		case <-ctx.Done():
			return AccessToken{}, ctx.Err()
		}
	})}
	done := make(chan error, 1)
	go func() { _, err := cache.get(context.Background()); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := cache.get(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := cache.get(context.Background()); err != nil || calls.Load() != 1 {
		t.Fatal(err, calls.Load())
	}
	cache.invalidate()
	cache.provider = credentialFunc(func(context.Context, bool) (AccessToken, error) { panic("callback secret") })
	if _, err := cache.get(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("panic leaked or disappeared", err)
	}
	cache.provider = credentialFunc(func(context.Context, bool) (AccessToken, error) { return fixtureToken(t, "after-panic", nil), nil })
	if _, err := cache.get(context.Background()); err != nil {
		t.Fatal("refresh waiter remained parked", err)
	}
}

func TestDynamicCredentialsDirectAndStreamAdmission(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var fetched, calls atomic.Int32
			source := credentialFunc(func(context.Context, bool) (AccessToken, error) {
				fetched.Add(1)
				return fixtureToken(t, "dynamic-token", nil), nil
			})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer dynamic-token" || r.Header.Get("Anthropic-Beta") != "oauth-2025-04-20b, oauth-2025-04-20" {
					t.Errorf("dynamic headers: %v", r.Header)
				}
				w.WriteHeader(401)
				io.WriteString(w, `{"error":{"message":"dynamic-token"}}`)
			}))
			defer upstream.Close()
			client, err := New(Config{BaseURL: upstream.URL, Credentials: source, Headers: []Header{{Name: "Anthropic-Beta", Value: " oauth-2025-04-20b , , "}}})
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				var failure error
				if stream {
					_, failure = (&StreamingClient{client: client}).CompleteStream(context.Background(), minimalRequest(), nil)
				} else {
					_, failure = client.Complete(context.Background(), minimalRequest())
				}
				var typed *Failure
				if !errors.As(failure, &typed) || typed.Kind != FailureStatus || strings.Contains(typed.Message, "dynamic-token") {
					t.Fatal("dynamic error leaked/classification changed", failure)
				}
			}
			if calls.Load() != 2 || fetched.Load() != 1 {
				t.Fatal("model 401 invalidated/replayed credentials", calls.Load(), fetched.Load())
			}
			client, err = New(Config{BaseURL: upstream.URL, APIKey: "explicit-key", Credentials: source})
			if err != nil {
				t.Fatal(err)
			}
			header := make(http.Header)
			if _, err := client.authenticate(context.Background(), header); err != nil || fetched.Load() != 1 || header.Get("Anthropic-Beta") != "" {
				t.Fatal("static credential did not bypass dynamic provider", err)
			}
		})
	}
}

func TestConcurrentRotatedTokenResponsesKeepTheirOwnRedaction(t *testing.T) {
	firstStarted, release := make(chan struct{}), make(chan struct{})
	var fetched atomic.Int32
	source := credentialFunc(func(context.Context, bool) (AccessToken, error) {
		value := "old-token"
		if fetched.Add(1) > 1 {
			value = "new-token"
		}
		return fixtureToken(t, value, nil), nil
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "old-token" {
			close(firstStarted)
			<-release
		}
		w.Header().Set("request-id", token)
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":{"message":%q}}`, token)
	}))
	defer upstream.Close()
	client, err := New(Config{BaseURL: upstream.URL, Credentials: source})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := client.Complete(context.Background(), minimalRequest()); done <- err }()
	<-firstStarted
	client.InvalidateCredentials()
	_, secondErr := client.Complete(context.Background(), minimalRequest())
	close(release)
	firstErr := <-done
	for _, err := range []error{firstErr, secondErr} {
		var f *Failure
		if !errors.As(err, &f) || strings.Contains(f.Message+f.RequestID, "old-token") || strings.Contains(f.Message+f.RequestID, "new-token") {
			t.Fatal("rotated token leaked", err)
		}
	}
}
