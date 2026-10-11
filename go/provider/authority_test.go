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

func TestCapturedASCIIAuthorityRequestsMatchSDK(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-url.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Message json.RawMessage
		SSE     string
		Cases   []struct {
			Name, Mode string
			BaseURL    *string `json:"base_url"`
			Identity   Description
			Calls      []struct {
				URL     string
				RawPath string `json:"raw_path"`
				Method  string
				APIKey  string `json:"api_key"`
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{"default": true, "empty": true, "uppercase-host": true, "uppercase-scheme": true, "https-default-port": true, "http-default-port": true, "https-other-port": true, "zero-padded-port": true, "empty-port": true, "ipv6": true, "ipv6-expanded": true, "ipv6-default-port": true, "punycode-host": true, "trailing-host-dot": true, "oversized-port": true, "trailing-space": true}
	count := 0
	for _, row := range fixture.Cases {
		if !selected[row.Name] {
			continue
		}
		count++
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			if len(row.Calls) != 1 {
				t.Fatal("Source captured call count changed")
			}
			want := row.Calls[0]
			calls := 0
			cfg := Config{APIKey: "fixture-key"}
			if row.BaseURL != nil {
				cfg.BaseURL = *row.BaseURL
			}
			cfg.HTTPClient = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != want.URL || req.URL.EscapedPath() != want.RawPath || req.Method != want.Method || req.Header.Get("x-api-key") != want.APIKey {
					t.Errorf("request URL %s path %s; Source %s path %s", req.URL.String(), req.URL.EscapedPath(), want.URL, want.RawPath)
				}
				body := string(fixture.Message)
				headers := http.Header{"Content-Type": []string{"application/json"}}
				if row.Mode == "stream" {
					body = fixture.SSE
					headers.Set("Content-Type", "text/event-stream")
				}
				return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}}
			var reply protocol.ModelReply
			var identity Description
			if row.Mode == "stream" {
				var c *StreamingClient
				c, err = NewStreaming(cfg)
				if err == nil {
					identity = c.Describe()
					reply, err = c.CompleteStream(context.Background(), request, nil)
				}
			} else {
				var c *Client
				c, err = New(cfg)
				if err == nil {
					identity = c.Describe()
					reply, err = c.Complete(context.Background(), request)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if identity != row.Identity {
				t.Fatal("configured audit identity changed", identity, row.Identity)
			}
			if calls != 1 || len(reply.Content) != 1 {
				t.Fatal("request/reply count differs")
			}
			text, ok := reply.Content[0].Text()
			if !ok || text.Text != "ok" {
				t.Fatal("reply text differs")
			}
		})
	}
	if count != 32 {
		t.Fatal("captured ASCII authority inventory changed", count)
	}
}
