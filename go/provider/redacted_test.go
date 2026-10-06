package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestRedactedThinkingDirectAndSSEOpaqueRoundTrip(t *testing.T) {
	message := `{"id":"msg_redacted","type":"message","role":"assistant","model":"served-model","content":[{"type":"redacted_thinking","data":"opaque-data"},{"type":"text","text":"answer"}],"stop_reason":"end_turn","usage":{"input_tokens":17,"output_tokens":5}}`
	want, err := decodeReply([]byte(message))
	if err != nil {
		t.Fatal(err)
	}
	if block, ok := want.Content[0].RedactedThinking(); !ok || block.Data != "opaque-data" {
		t.Fatal(block)
	}
	encoded := []byte(`{"id":"msg_redacted","type":"message","role":"assistant","model":"served-model","content":[],"stop_reason":null,"usage":{"input_tokens":17,"output_tokens":0}}`)
	wire := fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":%s}\n\n", encoded)
	wire += "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"redacted_thinking\",\"data\":\"opaque-data\"}}\n\n"
	wire += "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	wire += "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"answer\"}}\n\n"
	wire += "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n"
	wire += "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":5}}\n\n"
	wire += "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	c, err := New(Config{APIKey: "offline"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.readStream(context.Background(), strings.NewReader(wire), func(delta protocol.StreamDelta) error {
		if delta.Kind == protocol.DeltaText {
			t.Fatal("opaque start became text progress")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("%s != %s", a, b)
	}
	for _, raw := range []string{`{"type":"redacted_thinking"}`, `{"type":"redacted_thinking","data":null}`, `{"type":"redacted_thinking","data":3}`} {
		if _, err := decodeBlock([]byte(raw)); err == nil {
			t.Fatal("missing opaque data admitted", raw)
		}
	}
	bad := strings.Replace(wire, `"data":"opaque-data"`, `"missing":"opaque-data"`, 1)
	if _, err := c.readStream(context.Background(), strings.NewReader(bad), nil); err == nil {
		t.Fatal("missing stream data admitted")
	}
}
