package provider

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestCapturedAuthorityHTTPWireMatchesSDK(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-authority-wire.json")
	if err != nil {
		t.Fatal(err)
	}
	type wireRequest struct {
		Method, Target, Version, Host string
		APIKey                        string `json:"api_key"`
	}
	var fixture struct {
		UnicodeVersion string `json:"unicode_version"`
		Message        json.RawMessage
		SSE            string
		Cases          []struct {
			Name, Mode string
			BaseURL    string `json:"base_url"`
			Identity   Description
			Dials      []struct {
				Host string
				Port int
			}
			Requests []wireRequest
			Text     string
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 34 {
		t.Fatal("Source wire inventory changed")
	}
	if fixture.UnicodeVersion != pytext.PythonUnicodeVersion {
		t.Fatal("Source host casing and native Unicode tables differ")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			if len(row.Dials) != 1 || len(row.Requests) != 1 {
				t.Fatal("Source effects changed")
			}
			observed := make(chan wireRequest, 1)
			dials := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				observed <- wireRequest{r.Method, r.RequestURI, r.Proto, r.Host, r.Header.Get("x-api-key")}
				if row.Mode == "stream" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte(fixture.SSE))
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(fixture.Message)
				}
			}))
			defer server.Close()
			transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				select {
				case dials <- address:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			cfg := Config{BaseURL: row.BaseURL, APIKey: "fixture-key", HTTPClient: &http.Client{Transport: transport}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}}
			var reply protocol.ModelReply
			var identity Description
			if row.Mode == "stream" {
				var c *StreamingClient
				c, err = NewStreaming(cfg)
				if err == nil {
					identity = c.Describe()
					reply, err = c.CompleteStream(ctx, request, nil)
				}
			} else {
				var c *Client
				c, err = New(cfg)
				if err == nil {
					identity = c.Describe()
					reply, err = c.Complete(ctx, request)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if identity != row.Identity {
				t.Fatal("configured identity changed", identity, row.Identity)
			}
			select {
			case got := <-observed:
				if got != row.Requests[0] {
					t.Fatal("real HTTP wire differs", got, row.Requests[0])
				}
			case <-ctx.Done():
				t.Fatal("no HTTP wire observation")
			}
			select {
			case got := <-dials:
				want := net.JoinHostPort(row.Dials[0].Host, strconv.Itoa(row.Dials[0].Port))
				if got != want {
					t.Fatal("transport dial authority differs", got, want)
				}
			case <-ctx.Done():
				t.Fatal("no TCP dial observation")
			}
			if len(reply.Content) != 1 {
				t.Fatal("reply content count differs")
			}
			text, ok := reply.Content[0].Text()
			if !ok || text.Text != row.Text {
				t.Fatal("reply text differs")
			}
		})
	}
}
