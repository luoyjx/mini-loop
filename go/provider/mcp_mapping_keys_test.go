package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestMCPMappingKeysSurviveDirectAndStreamReplies(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-mcp-mapping-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			ToolName string `json:"tool_name"`
			Input    string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			row.ToolName = "mcp__peer__echo"
			name, _ := json.Marshal(row.ToolName)
			delta, _ := json.Marshal(row.Input)
			wire := fmt.Sprintf(`{"id":"msg","type":"message","role":"assistant","model":"fixture","content":[{"type":"tool_use","id":"unknown","name":%s,"input":%s}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, name, row.Input)
			direct, err := decodeReply([]byte(wire))
			if err != nil {
				t.Fatal(err)
			}
			stream := fmt.Sprintf("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"fixture\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"unknown\",\"name\":%s,\"input\":{}}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":1}}\n\ndata: {\"type\":\"message_stop\"}\n\n", name, delta)
			for _, event := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
				prefix := `data: {"type":"` + event + `"`
				stream = strings.ReplaceAll(stream, prefix, "event: "+event+"\n"+prefix)
			}
			streamed, err := client.readStream(context.Background(), strings.NewReader(stream), nil)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonvalue.Decode(row.Input)
			if err != nil {
				t.Fatal(err)
			}
			expectedLegacy, _ := expected.MarshalLegacyUTF8()
			for _, reply := range []protocol.ModelReply{direct, streamed} {
				use, ok := reply.Content[0].ToolUse()
				if !ok || string(use.Name) != row.ToolName {
					t.Fatal("unknown tool name lost")
				}
				if _, ok := use.Input.MCP(); ok != (expected.Kind() == jsonvalue.Object) {
					t.Fatal("non-object MCP root entered callable arguments")
				}
				observation, err := use.Input.Observation()
				if err != nil {
					t.Fatal(err)
				}
				encoded, err := observation.LegacyJSON()
				if err != nil {
					t.Fatal(err)
				}
				observed, err := jsonvalue.Decode(string(encoded))
				if err != nil || observed.Kind() != expected.Kind() || string(encoded) != string(expectedLegacy) {
					t.Fatal("reply lost unknown argument kind", string(encoded), err)
				}
			}
		})
	}
}
