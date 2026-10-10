package provider

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestDirectNonfiniteSDKObservationsFailNextEncodingBeforeTransport(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonfinite-consumer.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name string
			Response   string `json:"response_json"`
			Content    string `json:"content_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 4 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			reply, err := decodeReply([]byte(row.Response))
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := protocol.MarshalObservedBlocks(reply.Clone().Content)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := jsonvalue.Decode(string(snapshot))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonvalue.Decode(row.Content)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := actual.Sorted().MarshalLegacyUTF8()
			b, _ := expected.Sorted().MarshalLegacyUTF8()
			if string(a) != string(b) {
				t.Fatalf("direct SDK observation changed: %s != %s", a, b)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
			defer server.Close()
			client, err := New(Config{APIKey: "fixture-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			messages := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}, {Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}}
			if row.Kind == "tool_use" {
				messages = append(messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("call", "binding failure", true))})
			}
			request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: messages}
			if err = request.Validate(); err != nil {
				t.Fatal("valid transcript rejected before encoding", err)
			}
			if _, err = client.Complete(context.Background(), request); err == nil {
				t.Fatal("nonfinite observation sent as HTTP request")
			}
			if requests.Load() != 0 {
				t.Fatal("request transport called before rejecting nonfinite encoding")
			}
		})
	}
}

func TestNonfiniteSDKDecodeRefusesMetadataAndOtherBlocks(t *testing.T) {
	for _, input := range []string{
		`{"id":NaN,"type":"message","role":"assistant","model":"model","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		`{"id":"message","type":"message","role":"assistant","model":"model","content":[{"type":"text","text":NaN}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		`{"id":"message","type":"message","role":"assistant","model":"model","content":[{"type":"tool_use","id":Infinity,"name":"bash","input":{"command":"true","keep":NaN}}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`,
		`{"content":[{"type":"tool_use","input":{"keep":NaN,}}]}`,
	} {
		if _, err := decodeReply([]byte(input)); err == nil {
			t.Fatalf("accepted nonfinite metadata or malformed reply: %s", input)
		}
	}
}
