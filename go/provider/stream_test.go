package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type streamCase struct {
	Name, Wire string
	Frames     []struct {
		Body          json.RawMessage
		Retry, Accept string
	}
	Waits      []float64
	Reply      *protocol.ModelReply
	ErrorClass *string                `json:"error_class"`
	RawDeltas  []protocol.StreamDelta `json:"raw_deltas"`
	Closed     bool
}
type streamFixture struct {
	Request protocol.ModelRequest
	Cases   []streamCase
}

func streams(t *testing.T) streamFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-streams.json")
	if err != nil {
		t.Fatal(err)
	}
	var f streamFixture
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	f.Request.Purpose = protocol.PurposeAgentTurn
	return f
}

type fragmentedBody struct {
	value           string
	dropped, closed bool
}

func (b *fragmentedBody) Read(p []byte) (int, error) {
	if b.value == "" {
		if b.dropped {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, io.EOF
	}
	n := len(p)
	if n > 3 {
		n = 3
	}
	if n > len(b.value) {
		n = len(b.value)
	}
	copy(p, b.value[:n])
	b.value = b.value[n:]
	return n, nil
}
func (b *fragmentedBody) Close() error { b.closed = true; return nil }
func TestStreamingMatchesActualSDKAndPythonTransport(t *testing.T) {
	f := streams(t)
	for _, spec := range f.Cases {
		t.Run(spec.Name, func(t *testing.T) {
			waiter := &captureWaiter{}
			attempts := 0
			var bodies []*fragmentedBody
			client, err := NewStreaming(Config{APIKey: "fixture-key", BaseURL: "https://provider.invalid/", Waiter: waiter, Jitter: func() float64 { return 0 }, HTTPClient: &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				if attempts >= len(spec.Frames) {
					t.Fatal("extra HTTP attempt")
				}
				frame := spec.Frames[attempts]
				attempts++
				body, _ := io.ReadAll(req.Body)
				var actual, want protocol.ModelRequest
				// Strip stream at the local wire boundary before concrete request decoding.
				var wire struct {
					Stream bool `json:"stream"`
				}
				if json.Unmarshal(body, &wire) != nil || !wire.Stream {
					t.Fatal("stream flag missing")
				}
				var actualObject, wantObject map[string]json.RawMessage
				json.Unmarshal(body, &actualObject)
				json.Unmarshal(frame.Body, &wantObject)
				if len(actualObject) != len(wantObject) {
					t.Fatalf("request fields differ %s", body)
				}
				delete(actualObject, "stream")
				delete(wantObject, "stream")
				a, _ := json.Marshal(actualObject)
				w, _ := json.Marshal(wantObject)
				if err := json.Unmarshal(a, &actual); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(w, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, want) {
					t.Fatalf("body mismatch %s / %s", a, w)
				}
				if req.Header.Get("x-stainless-retry-count") != frame.Retry || req.Header.Get("accept") != frame.Accept || req.URL.Path != "/v1/messages" || req.Header.Get("x-api-key") != "fixture-key" {
					t.Fatalf("headers/path mismatch: %v", req.Header)
				}
				if spec.Name == "retry" && attempts == 1 {
					return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"1"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"retry"}}`))}, nil
				}
				b := &fragmentedBody{value: spec.Wire, dropped: spec.Name == "drop"}
				bodies = append(bodies, b)
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: b}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			var deltas []protocol.StreamDelta
			reply, err := client.CompleteStream(context.Background(), f.Request, func(d protocol.StreamDelta) error { deltas = append(deltas, d); return nil })
			if spec.ErrorClass == nil {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(reply, *spec.Reply) {
					t.Fatalf("reply differs: %#v / %#v", reply, *spec.Reply)
				}
			} else {
				var failure *Failure
				if !errors.As(err, &failure) {
					t.Fatalf("failure missing: %v", err)
				}
				if spec.Name == "drop" {
					if failure.Kind != FailureConnection {
						t.Fatal(failure)
					}
				} else if string(failure.Class()) != *spec.ErrorClass {
					t.Fatalf("class %s / %s", failure.Class(), *spec.ErrorClass)
				}
			}
			if len(deltas) != len(spec.RawDeltas) || (len(deltas) > 0 && !reflect.DeepEqual(deltas, spec.RawDeltas)) {
				t.Fatalf("SDK progress differs: %#v / %#v", deltas, spec.RawDeltas)
			}
			if attempts != len(spec.Frames) {
				t.Fatalf("attempts %d", attempts)
			}
			waits := waiter.waits
			if len(waits) != len(spec.Waits) {
				t.Fatal(waits, spec.Waits)
			}
			for i, d := range waits {
				if d != spec.Waits[i] {
					t.Fatal(waits, spec.Waits)
				}
			}
			for _, b := range bodies {
				if !b.closed {
					t.Fatal("unclosed stream")
				}
			}
		})
	}
}
func TestStreamingRejectsIncompleteInvalidAndUnboundedWire(t *testing.T) {
	f := streams(t)
	good := f.Cases[0].Wire
	cases := []struct {
		name, wire string
		kind       FailureKind
	}{
		{"empty", "", FailureConnection},
		{"unterminated", good[:strings.Index(good, "event: message_stop")] + "event: message_stop\ndata: {\"type\":\"message_stop\"}", FailureConnection},
		{"early-delta", `event: content_block_delta` + "\n" + `data: {"index":0,"delta":{"type":"text_delta","text":"bad"}}` + "\n\n", FailureProtocol},
		{"index", strings.Replace(good, `"index": 0`, `"index": 99`, 1), FailureProtocol},
		{"unsigned", strings.Replace(good, `"signature": "signed-proof"`, `"signature": ""`, 1), FailureProtocol},
		{"invalid-json", strings.Replace(good, `"partial_json": "\"content\":\"你好\\n\"}"`, `"partial_json": "bad}"`, 1), FailureProtocol},
		{"discriminator", strings.Replace(good, `"type": "text_delta"`, `"type": "thinking_delta"`, 1), FailureProtocol},
		{"invalid-utf8", strings.Replace(good, "你好", "\xff", 1), FailureProtocol},
		{"citation", strings.Replace(good, `"type": "text_delta"`, `"type": "citations_delta"`, 1), FailureProtocol},
		{"after-stop", good + "event: message_delta\ndata: {}\n\n", FailureProtocol},
	}
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			c, _ := New(Config{APIKey: "key"})
			_, err := c.readStream(context.Background(), strings.NewReader(v.wire), nil)
			var failure *Failure
			if !errors.As(err, &failure) || failure.Kind != v.kind {
				t.Fatalf("want %s got %v", v.kind, err)
			}
		})
	}
	c, _ := New(Config{APIKey: "key", MaxResponseBytes: 20})
	_, err := c.readStream(context.Background(), strings.NewReader(good), nil)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureLimit {
		t.Fatal(err)
	}
	c, _ = New(Config{APIKey: "key"})
	stop := errors.New("consumer stopped")
	_, err = c.readStream(context.Background(), strings.NewReader(good), func(protocol.StreamDelta) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatal(err)
	}
	_, err = c.readStream(context.Background(), strings.NewReader(strings.Repeat("x", protocol.MaxWireBytes+1)), nil)
	if !errors.As(err, &failure) || failure.Kind != FailureLimit {
		t.Fatal(err)
	}
}
func TestStreamingDoesNotRetryDroppedBodyOrCallback(t *testing.T) {
	f := streams(t)
	for _, callback := range []bool{false, true} {
		calls := 0
		body := &fragmentedBody{value: f.Cases[0].Wire, dropped: true}
		waiter := &captureWaiter{}
		c, _ := NewStreaming(Config{APIKey: "key", Waiter: waiter, Timeout: time.Second, HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}}, nil
		})}})
		stop := errors.New("consumer stop")
		_, err := c.CompleteStream(context.Background(), f.Request, func(protocol.StreamDelta) error {
			if callback {
				return stop
			}
			return nil
		})
		if err == nil || calls != 1 || len(waiter.waits) != 0 || !body.closed {
			t.Fatalf("invalid retry/close %v %d", err, calls)
		}
		if callback && !errors.Is(err, stop) {
			t.Fatal(err)
		}
	}
}
