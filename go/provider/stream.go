package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// StreamingClient selects streaming explicitly. The underlying client remains
// safe to share; every call owns its response, accumulator and progress sink.
type StreamingClient struct{ client *Client }

func NewStreaming(cfg Config) (*StreamingClient, error) {
	client, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &StreamingClient{client}, nil
}
func (c *StreamingClient) Describe() Description { return c.client.Describe() }
func (c *StreamingClient) String() string        { return c.client.String() + " (streaming)" }
func (c *StreamingClient) GoString() string      { return c.String() }
func (c *StreamingClient) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	return c.CompleteStream(ctx, request, nil)
}

// SDK retries end once successful response headers arrive. A dropped stream
// surfaces to the caller; replaying it here would splice provisional generations.
func (c *StreamingClient) CompleteStream(ctx context.Context, request protocol.ModelRequest, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, &Failure{Kind: FailureProtocol, Message: "invalid provider request"}
	}
	wire, err := request.Wire()
	if err != nil {
		return protocol.ModelReply{}, &Failure{Kind: FailureProtocol, Message: "invalid provider request"}
	}
	body, err := json.Marshal(struct {
		protocol.WireRequest
		Stream bool `json:"stream"`
	}{wire, true})
	if err != nil {
		return protocol.ModelReply{}, &Failure{Kind: FailureProtocol, Message: "cannot encode streaming request"}
	}
	if int64(len(body)) > c.client.requestLimit {
		return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Message: "provider request exceeds wire limit"}
	}
	for attempt := 0; ; attempt++ {
		reply, failure, retry, delay, err := c.attempt(ctx, body, attempt, emit)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		if failure == nil {
			return reply, nil
		}
		if !retry || attempt >= c.client.retries {
			return protocol.ModelReply{}, failure
		}
		if err := c.client.wait.Wait(ctx, delay); err != nil {
			return protocol.ModelReply{}, err
		}
	}
}
func (c *StreamingClient) attempt(ctx context.Context, body []byte, attempt int, emit func(protocol.StreamDelta) error) (protocol.ModelReply, *Failure, bool, time.Duration, error) {
	client := c.client
	callCtx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, client.requestURL, bytes.NewReader(body))
	if err != nil {
		return protocol.ModelReply{}, nil, false, 0, errors.New("cannot construct provider request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json") // AsyncAnthropic also uses this with stream=true.
	req.Header.Set("X-Api-Key", client.key)
	req.Header.Set("Anthropic-Version", APIVersion)
	req.Header.Set("X-Stainless-Retry-Count", strconv.Itoa(attempt))
	response, err := client.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return protocol.ModelReply{}, nil, false, 0, ctx.Err()
		}
		return protocol.ModelReply{}, streamReadFailure(callCtx, err), true, client.backoff(attempt, nil), nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, err := io.ReadAll(io.LimitReader(response.Body, client.responseLimit+1))
		if ctx.Err() != nil {
			return protocol.ModelReply{}, nil, false, 0, ctx.Err()
		}
		if int64(len(data)) > client.responseLimit {
			return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Message: "provider response exceeds wire limit"}, false, 0, nil
		}
		if err != nil {
			return protocol.ModelReply{}, streamReadFailure(callCtx, err), true, client.backoff(attempt, nil), nil
		}
		after := parseRetryAfter(response.Header, client.now())
		failure := &Failure{Kind: FailureStatus, Status: response.StatusCode, Message: client.statusMessage(response.StatusCode, data), RequestID: client.clean(response.Header.Get("request-id"), 200), RetryAfter: after, RetryAfterSeconds: retryAfterSeconds(response.Header.Get("retry-after"))}
		return protocol.ModelReply{}, failure, shouldRetry(response.StatusCode, response.Header), client.backoff(attempt, after), nil
	}
	reply, err := client.readStream(callCtx, response.Body, emit)
	if ctx.Err() != nil {
		return protocol.ModelReply{}, nil, false, 0, ctx.Err()
	}
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) {
			failure.RequestID = client.clean(response.Header.Get("request-id"), 200)
			failure.RetryAfterSeconds = retryAfterSeconds(response.Header.Get("retry-after"))
			return protocol.ModelReply{}, failure, false, 0, nil
		}
		// Callback failures are owned by the consumer and must not be classified
		// as HTTP failures or automatically retried.
		return protocol.ModelReply{}, nil, false, 0, err
	}
	return reply, nil, false, 0, nil
}
func streamReadFailure(ctx context.Context, err error) *Failure {
	var timeout interface{ Timeout() bool }
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return &Failure{Kind: FailureTimeout, Message: "Request timed out while streaming."}
	}
	return &Failure{Kind: FailureConnection, Message: "Connection error while streaming."}
}
