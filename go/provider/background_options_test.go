package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBackgroundOptionInputsSurviveDirectAndStreamingReplyDecoders(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string
			Input string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 48 {
		t.Fatal("Source option inventory changed")
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			response := fmt.Sprintf(`{"id":"message","type":"message","role":"assistant","model":"model","content":[{"type":"tool_use","id":"call","name":"background_run","input":%s}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":0}}`, row.Input)
			direct, err := decodeReply([]byte(response))
			if err != nil {
				t.Fatal("direct reply refused optional observation", err)
			}
			partial, _ := json.Marshal(row.Input)
			sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"message\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"model\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call\",\"name\":\"background_run\",\"input\":{}}}\n\n" +
				fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", partial) +
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":0}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			streamed, err := client.readStream(context.Background(), strings.NewReader(sse), nil)
			if err != nil {
				t.Fatal("stream refused optional observation", err)
			}
			expected, err := protocol.DecodeModelToolInput(protocol.ToolBackgroundRun, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			want, _ := expected.CanonicalJSON()
			for _, blocks := range [][]protocol.Block{direct.Clone().Content, streamed.Clone().Content} {
				if len(blocks) != 1 {
					t.Fatal("reply changed block inventory")
				}
				use, ok := blocks[0].ToolUse()
				if !ok {
					t.Fatal("reply changed block kind")
				}
				got, err := use.Input.CanonicalJSON()
				if err != nil || got != want {
					t.Fatal("SDK input observation changed", got, want, err)
				}
			}
		})
	}
}
