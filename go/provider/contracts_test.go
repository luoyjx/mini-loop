package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type captureWaiter struct{ waits []float64 }

func (w *captureWaiter) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.waits = append(w.waits, d.Seconds())
	return nil
}

type frame struct {
	Path, Method string
	Headers      map[string]string
	Body         json.RawMessage
}
type providerCase struct {
	Name          string
	Statuses      []json.RawMessage
	Headers       map[string]string
	Model         string
	MaxTokens     int  `json:"max_tokens"`
	CustomTimeout bool `json:"custom_timeout"`
	Frames        []frame
	Waits         []float64
	Reply         *protocol.ModelReply
	ErrorClass    *ErrorClass `json:"error_class"`
	Status        int
}
type providerFixture struct {
	SDKVersion   string         `json:"sdk_version"`
	SDKRetries   int            `json:"sdk_max_retries"`
	Ceilings     map[string]int `json:"sdk_model_ceilings"`
	Request      json.RawMessage
	Descriptions []Description
	Cases        []providerCase
}

func fixture(t *testing.T) providerFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-provider.json")
	if err != nil {
		t.Fatal(err)
	}
	var v providerFixture
	if err = json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func sourceRequest(t *testing.T, data []byte) protocol.ModelRequest {
	t.Helper()
	var v struct {
		Model     string
		MaxTokens int `json:"max_tokens"`
		System    []struct {
			Text  string
			Cache protocol.CacheControl `json:"cache_control"`
		}
		Messages []struct {
			Role    protocol.Role
			Content []struct {
				Text  string
				Cache protocol.CacheControl `json:"cache_control"`
			}
		}
		Tools []protocol.ToolSchema
	}
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	system := v.System[0].Text
	r := protocol.ModelRequest{Model: v.Model, MaxTokens: v.MaxTokens, Tools: v.Tools, System: &system, Purpose: protocol.PurposeAgentTurn, Cache: protocol.CacheAnnotations{System: &v.System[0].Cache}}
	for i, message := range v.Messages {
		var blocks []protocol.Block
		for j, b := range message.Content {
			blocks = append(blocks, protocol.NewTextBlock(b.Text))
			r.Cache.Messages = append(r.Cache.Messages, protocol.CacheBreakpoint{MessageIndex: i, BlockIndex: j, Control: b.Cache})
		}
		r.Messages = append(r.Messages, protocol.Message{Role: message.Role, Content: protocol.BlockContent(blocks...)})
	}
	return r
}
func equalJSON(t *testing.T, a, b []byte) {
	t.Helper()
	var x, y interface{}
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("JSON differs: %s != %s", a, b)
	}
}

type timedError struct{}

func (timedError) Error() string { return "timed out" }
func (timedError) Timeout() bool { return true }

type brokenReader struct{ read bool }

func (r *brokenReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(p, `{"id":"partial`), nil
	}
	return 0, errors.New("read interrupted")
}
func (*brokenReader) Close() error { return nil }
func TestDirectHTTPMatchesActualPythonSDK(t *testing.T) {
	expected := fixture(t)
	if expected.SDKVersion != PythonSDKBaseline || expected.SDKRetries != DefaultRetries {
		t.Fatal("SDK baseline drift")
	}
	for model, limit := range expected.Ceilings {
		if DirectTokenCeiling(model) != limit {
			t.Fatal("SDK model ceiling drift", model)
		}
	}
	success := expected.Cases[0].Reply
	successBody, _ := json.Marshal(success)
	for _, c := range expected.Cases {
		t.Run(c.Name, func(t *testing.T) {
			frames := []frame{}
			waiter := &captureWaiter{waits: []float64{}}
			rt := transportFunc(func(r *http.Request) (*http.Response, error) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				r.Body.Close()
				frames = append(frames, frame{r.URL.String(), r.Method, map[string]string{"x-api-key": r.Header.Get("x-api-key"), "anthropic-version": r.Header.Get("anthropic-version"), "x-stainless-retry-count": r.Header.Get("x-stainless-retry-count"), "content-type": r.Header.Get("content-type")}, data})
				index := len(frames) - 1
				code := 200
				kind := ""
				if len(c.Statuses) > 0 {
					if index >= len(c.Statuses) {
						index = len(c.Statuses) - 1
					}
					raw := c.Statuses[index]
					if raw[0] == '"' {
						json.Unmarshal(raw, &kind)
					} else {
						json.Unmarshal(raw, &code)
					}
				}
				switch kind {
				case "connection":
					return nil, errors.New("broken socket")
				case "timeout":
					return nil, timedError{}
				case "body":
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: &brokenReader{}, Request: r}, nil
				}
				payload := successBody
				if code != 200 {
					payload = []byte(`{"type":"error","error":{"type":"fixture_error","message":"fixture failure"}}`)
				}
				headers := http.Header{"Request-Id": []string{"request-fixture"}}
				for k, v := range c.Headers {
					headers.Set(k, v)
				}
				return &http.Response{StatusCode: code, Header: headers, Body: io.NopCloser(bytes.NewReader(payload)), Request: r}, nil
			})
			cfg := Config{BaseURL: "https://provider.invalid/proxy/", APIKey: "fixture-key", HTTPClient: &http.Client{Transport: rt}, Waiter: waiter, Jitter: func() float64 { return 0 }, Now: func() time.Time { return time.Unix(1700000000, 0) }}
			if c.CustomTimeout {
				cfg.Timeout = 3 * time.Second
			}
			client, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			r := sourceRequest(t, expected.Request)
			if c.Model != "" {
				r.Model = c.Model
			}
			if c.MaxTokens != 0 {
				r.MaxTokens = c.MaxTokens
			}
			reply, err := client.Complete(context.Background(), r)
			if c.ErrorClass == nil {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(reply, *c.Reply) {
					a, _ := json.Marshal(reply)
					b, _ := json.Marshal(c.Reply)
					t.Fatalf("reply %s != %s", a, b)
				}
			} else {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Class() != *c.ErrorClass || failure.Status != c.Status {
					t.Fatal("error differs", err, c.ErrorClass)
				}
			}
			if len(frames) != len(c.Frames) || !reflect.DeepEqual(waiter.waits, c.Waits) {
				t.Fatal("retry behavior differs", len(frames), len(c.Frames), waiter.waits, c.Waits)
			}
			for i, frame := range frames {
				want := c.Frames[i]
				if frame.Path != want.Path || frame.Method != want.Method || !reflect.DeepEqual(frame.Headers, want.Headers) {
					t.Fatal("headers/path differs", frame, want)
				}
				equalJSON(t, frame.Body, want.Body)
			}
		})
	}
	for i, base := range []string{"", "https://provider.invalid/proxy/"} {
		client, err := New(Config{BaseURL: base, APIKey: "fixture-key"})
		if err != nil {
			t.Fatal(err)
		}
		if client.Describe() != expected.Descriptions[i] {
			t.Fatal(client.Describe(), expected.Descriptions[i])
		}
	}
}
