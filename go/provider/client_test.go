package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func minimalRequest() protocol.ModelRequest {
	return protocol.ModelRequest{Model: "requested", MaxTokens: 100, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("go")}}, Purpose: protocol.PurposeAgentTurn}
}

const goodReply = `{"id":"message","type":"message","role":"assistant","model":"served","content":[{"type":"text","text":"done","citations":null}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":11,"output_tokens":2,"cache_creation":{"ephemeral_5m_input_tokens":0},"server_tool_use":null},"container":null,"stop_details":null}`

func TestClientRejectsUnsafeConfigAndNeverPrintsCredential(t *testing.T) {
	for _, base := range []string{"file:///tmp/a", "https://user:password@host", "https://host/?key=secret", "https://host/#secret", "https://host/?", "https://"} {
		if _, err := New(Config{BaseURL: base, APIKey: "fixture-key"}); err == nil {
			t.Fatal("unsafe endpoint accepted", base)
		}
	}
	for _, key := range []string{"", " ", "bad\r\nheader", "bad\x00header", "bad\theader"} {
		if _, err := New(Config{APIKey: key}); err == nil {
			t.Fatal("unsafe credential accepted")
		}
	}
	for _, cfg := range []Config{{APIKey: "key", Timeout: -time.Second}, {APIKey: "key", Timeout: 25 * time.Hour}, {APIKey: "key", MaxResponseBytes: MaxWireLimit + 1}, {APIKey: "key", MaxRequestBytes: -1}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("bad config accepted")
		}
	}
	for _, n := range []int{-1, 11} {
		if _, err := New(Config{APIKey: "key", MaxRetries: &n}); err == nil {
			t.Fatal("bad retries")
		}
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	description, _ := json.Marshal(client.Describe())
	serialized, _ := json.Marshal(client)
	if strings.Contains(fmt.Sprintf("%v %+v %#v %s %s", client, client, client, description, serialized), "fixture-key") {
		t.Fatal("client/debug identity leaks credential")
	}
}
func TestClientLimitsMalformedRepliesAndRedactsErrors(t *testing.T) {
	zero := 0
	for _, tc := range []struct {
		name, body string
		status     int
		limit      int64
		kind       FailureKind
	}{
		{"oversize", strings.Repeat("x", 100), 200, 10, FailureLimit},
		{"malformed", `{`, 200, 100, FailureProtocol},
		{"invalid-utf8", strings.Replace(goodReply, "done", string([]byte{255}), 1), 200, 10000, FailureProtocol},
		{"missing", `{}`, 200, 100, FailureProtocol},
		{"unsigned", strings.Replace(goodReply, `{"type":"text","text":"done","citations":null}`, `{"type":"thinking","thinking":"private"}`, 1), 200, 10000, FailureProtocol},
		{"citations", strings.Replace(goodReply, `"citations":null`, `"citations":[{"type":"unported"}]`, 1), 200, 10000, FailureProtocol},
		{"unknown", strings.Replace(goodReply, `"type":"text"`, `"type":"redacted_thinking"`, 1), 200, 10000, FailureProtocol},
		{"key-echo", `{"error":{"message":"fixture-key says no"}}`, 401, 10000, FailureStatus},
		{"plain-error", "fixture-key plaintext error", 502, 10000, FailureStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			var closed bool
			body := &closeReader{Reader: strings.NewReader(tc.body), closed: &closed}
			client, err := New(Config{BaseURL: "https://provider.invalid", APIKey: "fixture-key", MaxResponseBytes: tc.limit, MaxRetries: &zero, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Request-Id": []string{"fixture-key"}}, Body: body, Request: r}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Complete(context.Background(), minimalRequest())
			var failure *Failure
			if !errors.As(err, &failure) || failure.Kind != tc.kind || strings.Contains(err.Error(), "fixture-key") || strings.Contains(failure.RequestID, "fixture-key") || calls != 1 || !closed {
				t.Fatal("failure/cleanup/redaction differs", err, calls, closed)
			}
			if tc.name == "oversize" && body.read != 11 {
				t.Fatal("response cap read beyond cap+1", body.read)
			}
		})
	}
	var calls int
	client, _ := New(Config{APIKey: "key", MaxRequestBytes: 1, HTTPClient: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })}})
	_, err := client.Complete(context.Background(), minimalRequest())
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureLimit || calls != 0 {
		t.Fatal("oversize request sent")
	}
}

type closeReader struct {
	io.Reader
	closed *bool
	read   int
}

func (r *closeReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}
func (r *closeReader) Close() error { *r.closed = true; return nil }
func TestRedirectDoesNotForwardAPIKeyOrMutateSuppliedClient(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); w.Write([]byte(goodReply)) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer redirect.Close()
	supplied := &http.Client{}
	client, err := New(Config{BaseURL: redirect.URL, APIKey: "key", HTTPClient: supplied})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), minimalRequest())
	var failure *Failure
	if !errors.As(err, &failure) || failure.Status != 307 || targetCalls.Load() != 0 || supplied.CheckRedirect != nil {
		t.Fatal("redirect leaked key or changed client", err, targetCalls.Load())
	}
}
func TestActualHTTPTimeoutAndCallerCancellationStopRetries(t *testing.T) {
	entered := make(chan struct{}, 2)
	exited := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		entered <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
		exited <- struct{}{}
	}))
	defer func() { close(release); server.Close() }()
	zero := 0
	client, err := New(Config{BaseURL: server.URL, APIKey: "key", Timeout: 20 * time.Millisecond, MaxRetries: &zero})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), minimalRequest())
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureTimeout {
		t.Fatal("timeout classification", err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("HTTP context did not cancel")
	}
	<-entered
	client, _ = New(Config{BaseURL: server.URL, APIKey: "key"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.Complete(ctx, minimalRequest()); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not close request")
	}
}
func TestRetryWaitIsOwnedByCallerAndMetadataKeepsOuterHeaderMeaning(t *testing.T) {
	entered := make(chan struct{}, 1)
	var calls atomic.Int32
	client, err := New(Config{APIKey: "key", HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		entered <- struct{}{}
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"300"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"limited"}}`)), Request: r}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.Complete(ctx, minimalRequest()); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatal("cancelled retry continued", err, calls.Load())
	}
	zero := 0
	cfg := Config{APIKey: "key", MaxRetries: &zero, HTTPClient: client.http}
	noRetry, _ := New(cfg)
	_, err = noRetry.Complete(context.Background(), minimalRequest())
	var failure *Failure
	if !errors.As(err, &failure) || failure.RetryAfter != nil || failure.RetryAfterSeconds == nil || *failure.RetryAfterSeconds != 300 {
		t.Fatal("outer retry header lost", err)
	}
}
func TestSharedClientConcurrentCallsKeepBodiesAndRepliesDetached(t *testing.T) {
	var count atomic.Int32
	rt := transportFunc(func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		defer r.Body.Close()
		var input struct{ Messages []protocol.Message }
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			return nil, err
		}
		text, _ := input.Messages[0].Content.Plain()
		payload := strings.Replace(goodReply, `"done"`, strconvQuote(text), 1)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
	})
	client, _ := New(Config{APIKey: "key", HTTPClient: &http.Client{Transport: rt}})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text := fmt.Sprint(i)
			r := minimalRequest()
			r.Messages[0].Content = protocol.PlainContent(text)
			reply, err := client.Complete(context.Background(), r)
			if err != nil {
				t.Error(err)
				return
			}
			block, _ := reply.Content[0].Text()
			if block.Text != text {
				t.Error("cross-request payload", block.Text, text)
			}
		}(i)
	}
	wg.Wait()
	if count.Load() != 32 {
		t.Fatal(count.Load())
	}
}
func strconvQuote(s string) string { data, _ := json.Marshal(s); return string(data) }

func TestDecoderAcceptsNullableEmptyMetadataWithoutChangingThinkingOrCaller(t *testing.T) {
	for _, citations := range []string{"null", "[]", "[ ]"} {
		reply, err := decodeReply([]byte(strings.Replace(goodReply, `"citations":null`, `"citations":`+citations, 1)))
		if err != nil || len(reply.Content) != 1 {
			t.Fatal(citations, err)
		}
	}
	for _, caller := range []string{"", `,"caller":null`, `,"caller":{"type":"direct"}`} {
		body := strings.Replace(goodReply, `{"type":"text","text":"done","citations":null}`, `{"type":"tool_use","id":"b","name":"bash","input":{"command":"true"}`+caller+`}`, 1)
		reply, err := decodeReply([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(reply.Content[0])
		if !strings.Contains(string(data), `"caller":`) {
			t.Fatal("SDK nullable caller default lost")
		}
	}
}
