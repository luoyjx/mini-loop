package provider

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"strings"
	"testing"
)

func TestWebFetchResultMatchesSDKDirectAndStream(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-web-fetch.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Mode, SSE, Name string
			Message         json.RawMessage
			Expected        jsonvalue.Value `json:"expected_content"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 42 {
		t.Fatal("Source inventory changed")
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Mode+"/"+row.Name, func(t *testing.T) {
			var reply protocol.ModelReply
			var err error
			if row.Mode == "stream" {
				reply, err = client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
			} else {
				reply, err = decodeReply(row.Message)
			}
			if err != nil {
				t.Fatal(err)
			}
			value, ok := reply.Content[0].WebFetchToolResult()
			if !ok || value.ToolUseID != "server_fetch" {
				t.Fatal("web fetch result identity lost")
			}
			encoded, err := json.Marshal(reply.Content)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonvalue.Decode(string(encoded))
			if err != nil || !got.PythonEqual(row.Expected) {
				t.Fatal("SDK content projection differs", string(encoded))
			}
			message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}
			encoded, err = json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := protocol.DecodeStoredMessage(encoded)
			if err != nil {
				t.Fatal(err)
			}
			blocks, _ := restored.Content.Blocks()
			encoded, err = json.Marshal(blocks)
			if err != nil {
				t.Fatal(err)
			}
			got, err = jsonvalue.Decode(string(encoded))
			if err != nil || !got.PythonEqual(row.Expected) {
				t.Fatal("forwarded/stored web fetch result changed", string(encoded))
			}
			if _, tool := blocks[0].ToolUse(); tool {
				t.Fatal("server result metadata became executable")
			}
		})
	}
	for _, raw := range []string{
		`{"type":"web_fetch_tool_result","tool_use_id":"id"}`,
		`{"type":"web_fetch_tool_result","tool_use_id":"id","content":null}`,
		`{"type":"web_fetch_tool_result","tool_use_id":"id","content":[]}`,
		`{"type":"web_fetch_tool_result","tool_use_id":"id","content":{"type":"web_fetch_tool_result_error"}}`,
		`{"type":"web_fetch_tool_result","tool_use_id":"id","content":{"type":"web_fetch_result","url":"url","content":{"type":"document","source":{"type":"url","data":"x","media_type":"text/plain"}}}}`,
	} {
		if _, err := decodeBlock([]byte(raw)); err == nil {
			t.Fatal("invalid web fetch admitted", raw)
		}
	}
}
