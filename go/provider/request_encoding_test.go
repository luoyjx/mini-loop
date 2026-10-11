package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type encodingCredentials struct{ calls atomic.Int32 }

func (c *encodingCredentials) FetchToken(context.Context, bool) (AccessToken, error) {
	c.calls.Add(1)
	return NewAccessToken("fixture-token", nil)
}

func TestNonfiniteSDKRequestFailsBeforeCredentialsAndHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-sdk.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			Response string                           `json:"response_json"`
			Failure  *struct{ Class, Message string } `json:"forward_error"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	profiles := 0
	for _, row := range fixture.Cases {
		if row.Failure == nil {
			continue
		}
		profiles++
		for _, streaming := range []bool{false, true} {
			mode := "direct"
			if streaming {
				mode = "streaming"
			}
			t.Run(row.Name+"/"+mode, func(t *testing.T) {
				reply, err := decodeReply([]byte(row.Response))
				if err != nil {
					t.Fatal(err)
				}
				var requests, deltas atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
				defer server.Close()
				credentials := &encodingCredentials{}
				cfg := Config{BaseURL: server.URL, Credentials: credentials}
				request := protocol.ModelRequest{Model: "model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{
					{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")},
					{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)},
					{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("call", "accepted", false))},
				}}
				if streaming {
					client, createErr := NewStreaming(cfg)
					if createErr != nil {
						t.Fatal(createErr)
					}
					_, err = client.CompleteStream(context.Background(), request, func(protocol.StreamDelta) error { deltas.Add(1); return nil })
				} else {
					client, createErr := New(cfg)
					if createErr != nil {
						t.Fatal(createErr)
					}
					_, err = client.Complete(context.Background(), request)
				}
				var failure *Failure
				if !errors.As(err, &failure) || failure.Kind != FailureNonfiniteRequest || string(failure.Class()) != row.Failure.Class || failure.Message != row.Failure.Message {
					t.Fatalf("SDK encoding failure changed: %#v", err)
				}
				view := failure.RecoveryFailure()
				if view.Kind != protocol.ModelFailureProtocol || view.Class != row.Failure.Class || view.Status != 0 || view.RetryAfterSeconds != nil {
					t.Fatal(view)
				}
				if requests.Load() != 0 || deltas.Load() != 0 || credentials.calls.Load() != 0 {
					t.Fatal("encoding failure crossed effect boundary")
				}
			})
		}
	}
	if profiles != 6 {
		t.Fatal("Source nonfinite inventory changed", profiles)
	}
}
