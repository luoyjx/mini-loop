package provider

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDirectNonfiniteSDKObservationsFailNextEncodingBeforeTransport(t *testing.T) {
	assertNonfiniteSDKConsumers(t, "../testdata/python-provider-nonfinite-consumer.json", 4)
}

func TestNonfiniteKnownBashSDKInputsFailNextEncodingBeforeTransport(t *testing.T) {
	assertNonfiniteSDKConsumers(t, "../testdata/python-provider-nonfinite-bash.json", 36)
}

func TestNonfiniteBackgroundSDKInputsFailNextEncodingBeforeTransport(t *testing.T) {
	assertNonfiniteSDKConsumers(t, "../testdata/python-provider-nonfinite-background.json", 27)
}

func assertNonfiniteSDKConsumers(t *testing.T, fixturePath string, count int) {
	t.Helper()
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name    string
			Response      string `json:"response_json"`
			Content       string `json:"content_json"`
			SSE           string `json:"sse"`
			StreamContent string `json:"stream_content_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != count {
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
			if row.SSE != "" {
				client, err := New(Config{APIKey: "fixture-key"})
				if err != nil {
					t.Fatal(err)
				}
				streamed, err := client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
				if err != nil {
					t.Fatal("SDK stream input refused", err)
				}
				data, err := protocol.MarshalObservedBlocks(streamed.Content)
				if err != nil {
					t.Fatal(err)
				}
				observed, err := jsonvalue.Decode(string(data))
				if err != nil {
					t.Fatal(err)
				}
				expected, err := jsonvalue.Decode(row.StreamContent)
				if err != nil {
					t.Fatal(err)
				}
				a, _ := observed.Sorted().MarshalLegacyUTF8()
				b, _ := expected.Sorted().MarshalLegacyUTF8()
				if string(a) != string(b) {
					t.Fatal("SDK stream snapshot changed", string(a), string(b))
				}
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
