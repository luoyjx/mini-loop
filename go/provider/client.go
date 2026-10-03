// Package provider adapts the Anthropic-compatible HTTP boundary to concrete
// protocol requests/replies. It owns SDK-style HTTP retries, not agent recovery.
package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const DefaultEndpoint = "https://api.anthropic.com"
const APIVersion = "2023-06-01"
const DefaultRetries = 2
const DefaultTimeout = 10 * time.Minute
const DefaultWireLimit = 8 * 1024 * 1024
const MaxWireLimit = 64 * 1024 * 1024

type FailureKind string

const (
	FailureStatus            FailureKind = "status"
	FailureConnection        FailureKind = "connection"
	FailureTimeout           FailureKind = "timeout"
	FailureProtocol          FailureKind = "protocol"
	FailureLimit             FailureKind = "wire_limit"
	FailureStreamingRequired FailureKind = "streaming_required"
)

// Failure contains bounded, credential-scrubbed diagnostic text, never raw wire
// bytes or a response handle. Status and retry headers retain their own meanings.
type Failure struct {
	Kind              FailureKind
	Status            int
	Message           string
	RequestID         string
	RetryAfter        *time.Duration
	RetryAfterSeconds *float64
}

func (e *Failure) Error() string { return e.Message }

type RetryWaiter interface {
	Wait(context.Context, time.Duration) error
}
type timerWaiter struct{}

func (timerWaiter) Wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Config struct {
	BaseURL                           string
	APIKey                            string       `json:"-"`
	HTTPClient                        *http.Client `json:"-"`
	Timeout                           time.Duration
	MaxRequestBytes, MaxResponseBytes int64
	MaxRetries                        *int
	Waiter                            RetryWaiter      `json:"-"`
	Jitter                            func() float64   `json:"-"`
	Now                               func() time.Time `json:"-"`
}
type Description struct {
	Name       string `json:"name"`
	Wire       string `json:"wire"`
	Endpoint   string `json:"endpoint"`
	Credential string `json:"credential"`
}
type Client struct {
	endpoint, requestURL, key   string
	compatible                  bool
	defaultTimeout              bool
	http                        *http.Client
	timeout                     time.Duration
	requestLimit, responseLimit int64
	retries                     int
	wait                        RetryWaiter
	jitter                      func() float64
	now                         func() time.Time
}

func New(cfg Config) (*Client, error) {
	compatible := cfg.BaseURL != ""
	defaultTimeout := cfg.Timeout == 0
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultEndpoint
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("provider endpoint must be an HTTP(S) base URL without credentials, query or fragment")
	}
	if len(cfg.APIKey) > 4096 || strings.TrimSpace(cfg.APIKey) == "" || strings.IndexFunc(cfg.APIKey, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
		return nil, errors.New("provider API key is required and must be a single header value")
	}
	retries := DefaultRetries
	if cfg.MaxRetries != nil {
		retries = *cfg.MaxRetries
	}
	if retries < 0 || retries > 10 {
		return nil, errors.New("provider HTTP retries must be between 0 and 10")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Timeout < 0 || cfg.Timeout > 24*time.Hour {
		return nil, errors.New("provider timeout must be positive and at most 24 hours")
	}
	if cfg.MaxRequestBytes == 0 {
		cfg.MaxRequestBytes = DefaultWireLimit
	}
	if cfg.MaxResponseBytes == 0 {
		cfg.MaxResponseBytes = DefaultWireLimit
	}
	if cfg.MaxRequestBytes < 1 || cfg.MaxRequestBytes > MaxWireLimit || cfg.MaxResponseBytes < 1 || cfg.MaxResponseBytes > MaxWireLimit {
		return nil, errors.New("provider wire limits must be between 1 byte and 64 MiB")
	}
	if cfg.Waiter == nil {
		cfg.Waiter = timerWaiter{}
	}
	if cfg.Jitter == nil {
		cfg.Jitter = rand.Float64
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	base := http.DefaultClient
	if cfg.HTTPClient != nil {
		base = cfg.HTTPClient
	}
	client := *base
	// x-api-key is not a standard sensitive header stripped by net/http on a
	// cross-host redirect. Refuse redirects instead of forwarding a credential.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{cfg.BaseURL, strings.TrimRight(u.String(), "/") + "/v1/messages", cfg.APIKey, compatible, defaultTimeout, &client, cfg.Timeout, cfg.MaxRequestBytes, cfg.MaxResponseBytes, retries, cfg.Waiter, cfg.Jitter, cfg.Now}, nil
}
func (c *Client) Describe() Description {
	name := "anthropic"
	if c.compatible {
		name = "anthropic-compatible"
	}
	return Description{name, "anthropic", strings.ReplaceAll(c.endpoint, c.key, "[REDACTED]"), "<set>"}
}
func (c *Client) String() string {
	v := c.Describe()
	return fmt.Sprintf("%s (%s; credential %s)", v.Name, v.Endpoint, v.Credential)
}
func (c *Client) GoString() string { return c.String() }
func (c *Client) Complete(ctx context.Context, input protocol.ModelRequest) (protocol.ModelReply, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	if err := input.Validate(); err != nil {
		return protocol.ModelReply{}, &Failure{Kind: FailureProtocol, Message: "invalid provider request"}
	}
	if c.defaultTimeout && input.MaxTokens > DirectTokenCeiling(input.Model) {
		return protocol.ModelReply{}, &Failure{Kind: FailureStreamingRequired, Message: "Streaming is required for operations that may take longer than 10 minutes."}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return protocol.ModelReply{}, &Failure{Kind: FailureProtocol, Message: "cannot encode provider request"}
	}
	if int64(len(body)) > c.requestLimit {
		return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Message: "provider request exceeds wire limit"}
	}
	for attempt := 0; ; attempt++ {
		reply, failure, retry, delay, err := c.attempt(ctx, body, attempt)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		if failure == nil {
			return reply, nil
		}
		if !retry || attempt >= c.retries {
			return protocol.ModelReply{}, failure
		}
		if err := c.wait.Wait(ctx, delay); err != nil {
			return protocol.ModelReply{}, err
		}
	}
}
func (c *Client) attempt(ctx context.Context, body []byte, attempt int) (protocol.ModelReply, *Failure, bool, time.Duration, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.requestURL, bytes.NewReader(body))
	if err != nil {
		return protocol.ModelReply{}, nil, false, 0, errors.New("cannot construct provider request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Api-Key", c.key)
	req.Header.Set("Anthropic-Version", APIVersion)
	req.Header.Set("X-Stainless-Retry-Count", strconv.Itoa(attempt))
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return protocol.ModelReply{}, nil, false, 0, ctx.Err()
		}
		kind, msg := FailureConnection, "Connection error."
		var timed interface{ Timeout() bool }
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) || (errors.As(err, &timed) && timed.Timeout()) {
			kind, msg = FailureTimeout, "Request timed out."
		}
		return protocol.ModelReply{}, &Failure{Kind: kind, Message: msg}, true, c.backoff(attempt, nil), nil
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, c.responseLimit+1))
	if ctx.Err() != nil {
		return protocol.ModelReply{}, nil, false, 0, ctx.Err()
	}
	if int64(len(data)) > c.responseLimit {
		return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Status: response.StatusCode, Message: "provider response exceeds wire limit"}, false, 0, nil
	}
	if readErr != nil {
		kind, msg := FailureConnection, "Connection error while reading response."
		if callCtx.Err() != nil {
			kind, msg = FailureTimeout, "Request timed out while reading response."
		}
		// Non-streaming httpx.send reads the body inside the SDK retry block.
		return protocol.ModelReply{}, &Failure{Kind: kind, Message: msg}, true, c.backoff(attempt, nil), nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryAfter := parseRetryAfter(response.Header, c.now())
		failure := &Failure{Kind: FailureStatus, Status: response.StatusCode, Message: c.statusMessage(response.StatusCode, data), RequestID: c.clean(response.Header.Get("request-id"), 200), RetryAfter: retryAfter, RetryAfterSeconds: retryAfterSeconds(response.Header.Get("retry-after"))}
		return protocol.ModelReply{}, failure, shouldRetry(response.StatusCode, response.Header), c.backoff(attempt, retryAfter), nil
	}
	reply, err := decodeReply(data)
	if err != nil {
		return protocol.ModelReply{}, &Failure{Kind: FailureProtocol, Status: response.StatusCode, Message: "invalid provider response: " + c.clean(err.Error(), 500)}, false, 0, nil
	}
	return reply, nil, false, 0, nil
}
func (c *Client) clean(s string, limit int) string {
	s = strings.ReplaceAll(s, c.key, "[REDACTED]")
	runes := []rune(s)
	if len(runes) > limit {
		s = string(runes[:limit]) + " [truncated]"
	}
	return s
}
func (c *Client) statusMessage(status int, data []byte) string {
	var wire struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &wire) == nil && wire.Error.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", status, c.clean(wire.Error.Message, 2000))
	}
	return fmt.Sprintf("HTTP %d: provider request failed", status)
}
func shouldRetry(status int, h http.Header) bool {
	switch h.Get("x-should-retry") {
	case "true":
		return true
	case "false":
		return false
	}
	return status == 408 || status == 409 || status == 429 || status >= 500
}
func parseRetryAfter(h http.Header, now time.Time) *time.Duration {
	// Milliseconds take precedence, as in the Python SDK. Invalid ms falls back
	// to seconds/date; nan/inf never becomes an overflowing duration.
	for _, item := range []struct {
		key    string
		factor float64
	}{{"retry-after-ms", .001}, {"retry-after", 1}} {
		if value := h.Get(item.key); value != "" {
			seconds, err := strconv.ParseFloat(value, 64)
			if err == nil {
				if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
					return nil
				}
				seconds *= item.factor
				if seconds > 0 && seconds <= 60 {
					v := time.Duration(seconds * float64(time.Second))
					return &v
				}
				return nil
			} else if item.key == "retry-after" {
				if date, err := http.ParseTime(value); err == nil {
					d := date.Sub(now)
					if d > 0 && d <= time.Minute {
						return &d
					}
					return nil
				}
			}
		}
	}
	return nil
}
func (c *Client) backoff(attempt int, retryAfter *time.Duration) time.Duration {
	if retryAfter != nil {
		return *retryAfter
	}
	if attempt > 4 {
		attempt = 4
	}
	seconds := math.Min(.5*math.Exp2(float64(attempt)), 8)
	jitter := c.jitter()
	if math.IsNaN(jitter) || math.IsInf(jitter, 0) || jitter < 0 || jitter > 1 {
		jitter = 0
	}
	return time.Duration(seconds * (1 - .25*jitter) * float64(time.Second))
}
