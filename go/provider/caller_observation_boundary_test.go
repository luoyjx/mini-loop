package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// These are byte-bound regressions, separate from the fixed SDK semantic
// inventory. A finite sanitizer must not measure an ASCII-escaped temporary
// representation as though it were the normalized UTF-8 block.
func TestCallerObservationSanitizerKeepsUTF8ByteBounds(t *testing.T) {
	text := strings.Repeat("界", (protocol.MaxWireBytes-4096)/len("界"))
	for _, row := range []struct{ name, block, field string }{
		{"text", `{"type":"text","text":"` + text + `"}`, "text"},
		{"caller", `{"type":"tool_use","id":"actual-use","name":"bash","input":{"command":"true"},"caller":{"type":"future_caller","extra":"` + text + `"}}`, "caller"},
	} {
		if len(row.block) >= protocol.MaxWireBytes {
			t.Fatal("regression setup exceeds the intended UTF-8 bound")
		}
		value, err := jsonvalue.Decode(row.block)
		if err != nil {
			t.Fatal(err)
		}
		ascii, err := value.MarshalJSON()
		if err != nil || len(ascii) <= protocol.MaxWireBytes {
			t.Fatal("regression must exceed the bound only after ASCII escaping", err)
		}
		for _, mode := range []string{"direct", "stream"} {
			t.Run(row.name+"/"+mode, func(t *testing.T) {
				reply, err := decodeCallerBoundary(t, mode, row.block)
				if err != nil || len(reply.Content) != 1 {
					t.Fatal("valid UTF-8 observation rejected", err, len(reply.Content))
				}
				block := reply.Content[0]
				if row.field == "text" {
					observed, ok := block.Text()
					if !ok || observed.Text != text {
						t.Fatal("large UTF-8 text changed")
					}
				} else {
					use, ok := block.ToolUse()
					if !ok || use.Caller == nil || use.ID != "actual-use" || use.Name != protocol.ToolBash {
						t.Fatal("large caller metadata replaced outer identity")
					}
					observation, ok := use.Caller.SDKObservation()
					extra, present := observation.Value().Lookup("extra")
					got, isText := extra.Text()
					if !ok || !present || !isText || got != text {
						t.Fatal("large UTF-8 caller metadata changed")
					}
				}
				encoded, err := block.MarshalObservationJSON()
				if err != nil || len(encoded) >= protocol.MaxWireBytes {
					t.Fatal("normalized block no longer fits the byte bound", len(encoded), err)
				}
			})
		}
	}
}

func TestCallerObservationSanitizerRetainsRefusalBoundaries(t *testing.T) {
	tooLarge := strings.Repeat("界", protocol.MaxWireBytes/len("界")+1)
	for _, row := range []struct{ name, block string }{
		{"oversized-text", `{"type":"text","text":"` + tooLarge + `"}`},
		{"oversized-caller", `{"type":"tool_use","id":"actual-use","name":"bash","input":{},"caller":{"type":"future_caller","extra":"` + tooLarge + `"}}`},
		{"unrelated-tool-field", `{"type":"tool_use","id":"actual-use","name":"bash","input":{},"caller":{"type":"future_caller","extra":NaN},"unrelated":Infinity}`},
		{"unrelated-text-field", `{"type":"text","text":"safe","unrelated":NaN}`},
		{"nonfinite-identity", `{"type":"tool_use","id":NaN,"name":"bash","input":{},"caller":{"type":"future_caller"}}`},
	} {
		for _, mode := range []string{"direct", "stream"} {
			t.Run(row.name+"/"+mode, func(t *testing.T) {
				if _, err := decodeCallerBoundary(t, mode, row.block); err == nil {
					t.Fatal("caller sanitizer relaxed byte/finite outer-field admission")
				}
			})
		}
	}
}

func decodeCallerBoundary(t *testing.T, mode, block string) (protocol.ModelReply, error) {
	t.Helper()
	if mode == "direct" {
		return decodeReply([]byte(`{"id":"boundary","type":"message","role":"assistant","model":"fixture-model","content":[` + block + `],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`))
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	wire := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"boundary","type":"message","role":"assistant","model":"fixture-model","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}` + "\n\n" +
		"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":` + block + "}\n\n" +
		"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n" +
		"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}` + "\n\n" +
		"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
	return client.readStream(context.Background(), strings.NewReader(wire), nil)
}
