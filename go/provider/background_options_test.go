package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBackgroundOptionsMatchCapturedSDKRepliesAndForwarding(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-sdk.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name          string
			Response      string                           `json:"response_json"`
			Content       string                           `json:"content_json"`
			SSE           string                           `json:"sse"`
			StreamContent string                           `json:"stream_content_json"`
			Requests      int32                            `json:"forward_requests"`
			Failure       *struct{ Class, Message string } `json:"forward_error"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 48 {
		t.Fatal("Source SDK inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			direct, err := decodeReply([]byte(row.Response))
			if err != nil {
				t.Fatal("direct SDK reply refused", err)
			}
			client, err := New(Config{APIKey: "fixture-key"})
			if err != nil {
				t.Fatal(err)
			}
			streamed, err := client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
			if err != nil {
				t.Fatal("stream SDK reply refused", err)
			}
			for _, sample := range []struct {
				Blocks   []protocol.Block
				Expected string
			}{{direct.Clone().Content, row.Content}, {streamed.Clone().Content, row.StreamContent}} {
				encoded, err := protocol.MarshalObservedBlocks(sample.Blocks)
				if err != nil {
					t.Fatal(err)
				}
				actual, err := jsonvalue.Decode(string(encoded))
				if err != nil {
					t.Fatal(err)
				}
				expected, err := jsonvalue.Decode(sample.Expected)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := actual.Sorted().MarshalLegacyUTF8()
				want, _ := expected.Sorted().MarshalLegacyUTF8()
				if string(got) != string(want) {
					t.Fatal("Source SDK observation changed", string(got), string(want))
				}
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"final","type":"message","role":"assistant","model":"model","content":[{"type":"text","text":"finished"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":0}}`)
			}))
			defer server.Close()
			client, err = New(Config{APIKey: "fixture-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			messages := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}, {Role: protocol.RoleAssistant, Content: protocol.BlockContent(direct.Content...)}, {Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("call", "accepted", false))}}
			_, err = client.Complete(context.Background(), protocol.ModelRequest{Model: "model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: messages})
			if (err != nil) != (row.Failure != nil) || requests.Load() != row.Requests {
				t.Fatal("Source forwarding phase changed", err, requests.Load(), row.Requests)
			}
		})
	}
}
