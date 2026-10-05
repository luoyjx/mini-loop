package decisions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const JevURL = "https://api.typesafe.ai/v1/systemone"

// JevConfig is explicit. DefaultJevConfig supplies the source defaults. Client,
// if supplied, is borrowed; redirects are disabled on a private shallow copy.
type JevConfig struct {
	APIKey, Model string
	Timeout       time.Duration
	MaxRetries    int
	Client        *http.Client
}

func DefaultJevConfig(apiKey string) JevConfig {
	return JevConfig{APIKey: apiKey, Model: "jev-latest", Timeout: 20 * time.Second, MaxRetries: 2}
}

type JevProvider struct {
	apiKey, model string
	timeout       time.Duration
	retries       int
	client        *http.Client
}

func NewJev(cfg JevConfig) (*JevProvider, error) {
	if !nonempty(cfg.APIKey) || strings.ContainsAny(cfg.APIKey, "\r\n") {
		return nil, invalid("A valid TypeSafe API key is required.")
	}
	if !nonempty(cfg.Model) || utf8.RuneCountInString(cfg.Model) > 128 || !utf8.ValidString(cfg.Model) {
		return nil, invalid("A nonempty decision model ID is required.")
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 120*time.Second {
		return nil, invalid("Decision timeout must be finite and within (0, 120] seconds.")
	}
	if cfg.MaxRetries < 0 || cfg.MaxRetries > 3 {
		return nil, invalid("Decision retries must be between zero and three.")
	}
	p := &JevProvider{apiKey: cfg.APIKey, model: cfg.Model, timeout: cfg.Timeout, retries: cfg.MaxRetries}
	if cfg.Client != nil {
		client := *cfg.Client
		client.CheckRedirect = noRedirect
		p.client = &client
	}
	return p, nil
}
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
func (p *JevProvider) Model() string                  { return p.model }
func (p *JevProvider) Evaluate(ctx context.Context, request Request) (Result, error) {
	request, e := NewRequest(request.state, request.questions)
	if e != nil {
		return Result{}, e
	}
	obj := request.value().object
	obj["model"] = StringValue(p.model)
	body, e := encodeValue(ObjectValue(obj), MaxRequestBytes+256)
	if e != nil {
		return Result{}, e
	}
	owned := false
	client := p.client
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Transport: transport, CheckRedirect: noRedirect}
		owned = true
	}
	if owned {
		defer client.CloseIdleConnections()
	}
	bounded, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	for attempt := 0; attempt <= p.retries; attempt++ {
		req, e := http.NewRequestWithContext(bounded, http.MethodPost, JevURL, bytes.NewReader(body))
		if e != nil {
			return Result{}, failure("TypeSafe decision transport failed.")
		}
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, e := client.Do(req)
		if e != nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			return Result{}, transportFailure(ctx, bounded, e)
		}
		if (resp.StatusCode == 429 || resp.StatusCode == 529) && attempt < p.retries {
			delay := retryDelay(resp.Header.Get("Retry-After"), attempt)
			resp.Body.Close()
			timer := time.NewTimer(delay)
			select {
			case <-bounded.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return Result{}, transportFailure(ctx, bounded, bounded.Err())
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return Result{}, failure(fmt.Sprintf("TypeSafe decision request failed (HTTP %d).", resp.StatusCode))
		}
		data, e := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
		resp.Body.Close()
		if e != nil {
			return Result{}, transportFailure(ctx, bounded, e)
		}
		if e = bounded.Err(); e != nil {
			return Result{}, transportFailure(ctx, bounded, e)
		}
		if len(data) > MaxResponseBytes {
			return Result{}, failure("TypeSafe decision response exceeds the byte limit.")
		}
		payload, e := DecodeValue(data)
		if e != nil {
			return Result{}, failure("TypeSafe returned an invalid decision response.")
		}
		if payload.kind != Object {
			return Result{}, invalid("TypeSafe returned an invalid decision response.")
		}
		usage := payload.object["usage"]
		if usage.kind != Object || len(usage.object) != 2 {
			return Result{}, invalid("TypeSafe response requires token usage.")
		}
		if _, ok := usage.object["input_tokens"]; !ok {
			return Result{}, invalid("TypeSafe response requires token usage.")
		}
		if _, ok := usage.object["output_tokens"]; !ok {
			return Result{}, invalid("TypeSafe response requires token usage.")
		}
		return resultFromValue(request, payload, "typesafe", Jev)
	}
	return Result{}, failure("TypeSafe decision retry limit reached.")
}
func transportFailure(caller, total context.Context, err error) error {
	if errors.Is(caller.Err(), context.Canceled) {
		return context.Canceled
	}
	if total.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
		return failure("TypeSafe decision request timed out.")
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return failure("TypeSafe decision request timed out.")
	}
	return failure("TypeSafe decision transport failed.")
}

var retryNumber = regexp.MustCompile(`^[+-]?(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[eE][+-]?[0-9](?:_?[0-9])*)?$`)

func retryDelay(header string, attempt int) time.Duration {
	fallback := min(0.25*math.Pow(2, float64(attempt)), 2.0)
	text := strings.TrimFunc(header, pytext.Space)
	v, e := strconv.ParseFloat(strings.ReplaceAll(text, "_", ""), 64)
	if !retryNumber.MatchString(text) || e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		v = fallback
	}
	return time.Duration(min(v, 2.0) * float64(time.Second))
}

var _ Provider = (*JevProvider)(nil)
