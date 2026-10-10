package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBashObservedOptionsSurviveDirectAndStreamIngress(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Field, Name string
			Input       string `json:"input_json"`
			Enabled     bool   `json:"background_enabled"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	var template string
	for _, row := range partialSDKCases(t) {
		if row.Kind == "tool_use" && row.Name == "complete" {
			template = row.SSE
			break
		}
	}
	if template == "" {
		t.Fatal("SDK stream envelope missing")
	}
	count := 0
	for _, row := range fixture.Cases {
		if row.Enabled {
			continue
		} // same wire input, two Source consumer activation profiles
		t.Run(row.Field+"/"+row.Name, func(t *testing.T) {
			direct, err := decodeReply([]byte(`{"id":"fixture","type":"message","role":"assistant","model":"fixture-model","stop_sequence":null,"content":[{"type":"tool_use","id":"options","name":"bash","input":` + row.Input + `}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":2}}`))
			if err != nil {
				t.Fatal(err)
			}
			frames := strings.Split(template, "\n\n")
			for i, frame := range frames {
				if !strings.HasPrefix(frame, "event: content_block_delta\n") {
					continue
				}
				encoded, err := json.Marshal(struct {
					Type  string `json:"type"`
					Index int    `json:"index"`
					Delta struct {
						Type    string `json:"type"`
						Partial string `json:"partial_json"`
					} `json:"delta"`
				}{Type: "content_block_delta", Delta: struct {
					Type    string `json:"type"`
					Partial string `json:"partial_json"`
				}{Type: "input_json_delta", Partial: row.Input}})
				if err != nil {
					t.Fatal(err)
				}
				frames[i] = "event: content_block_delta\ndata: " + string(encoded)
			}
			stream, err := client.readStream(context.Background(), strings.NewReader(strings.Join(frames, "\n\n")), nil)
			if err != nil {
				t.Fatal(err)
			}
			var expected bytes.Buffer
			if err := json.Compact(&expected, []byte(row.Input)); err != nil {
				t.Fatal(err)
			}
			for i, reply := range []protocol.ModelReply{direct, stream} {
				content := reply.Content
				use, ok := content[0].ToolUse()
				if !ok {
					t.Fatal("Bash input discriminator lost")
				}
				encoded, err := json.Marshal(use.Input)
				if err != nil || string(encoded) != expected.String() {
					t.Fatal(i, "Source option shape changed", string(encoded), err)
				}
			}
		})
		count++
	}
	if count != 28 {
		t.Fatal("distinct Source wire inventory changed", count)
	}
}
