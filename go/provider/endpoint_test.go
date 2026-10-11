package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestEndpointRequestsMatchActualSDK(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-endpoint.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Message json.RawMessage
		SSE     string
		Cases   []struct {
			BaseURL    string `json:"base_url"`
			Mode, Text string
			Calls      []struct {
				URL, Method string
				RawPath     string `json:"raw_path"`
				APIKey      string `json:"api_key"`
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 224 {
		t.Fatal("endpoint fixture inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Mode+"/"+row.BaseURL, func(t *testing.T) {
			if len(row.Calls) != 1 {
				t.Fatal("Source request count changed")
			}
			want := row.Calls[0]
			calls := 0
			httpClient := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != want.URL || req.URL.EscapedPath() != want.RawPath || req.Method != want.Method || req.Header.Get("x-api-key") != want.APIKey {
					t.Errorf("actual request %s %s path=%s; Source %s %s path=%s", req.Method, req.URL.String(), req.URL.EscapedPath(), want.Method, want.URL, want.RawPath)
				}
				body := string(fixture.Message)
				headers := http.Header{"Content-Type": []string{"application/json"}}
				if row.Mode == "stream" {
					body = fixture.SSE
					headers.Set("Content-Type", "text/event-stream")
				}
				return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			cfg := Config{BaseURL: row.BaseURL, APIKey: "fixture-key", HTTPClient: httpClient}
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}}
			var reply protocol.ModelReply
			var err error
			if row.Mode == "stream" {
				var client *StreamingClient
				client, err = NewStreaming(cfg)
				if err == nil {
					reply, err = client.CompleteStream(context.Background(), request, nil)
				}
			} else {
				var client *Client
				client, err = New(cfg)
				if err == nil {
					if client.Describe().Endpoint != row.BaseURL {
						t.Fatal("configured endpoint identity changed")
					}
					reply, err = client.Complete(context.Background(), request)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(reply.Content) != 1 {
				t.Fatal("reply or request count differs", calls, reply)
			}
			text, ok := reply.Content[0].Text()
			if !ok || text.Text != row.Text {
				t.Fatal("reply text differs", text)
			}

		})
	}
}
