package provider

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"strings"
	"testing"
)

func TestTextEditorResultMatchesSDKDirectAndStream(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-text-editor.json")
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
	if len(fixture.Cases) != 48 {
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
			value, ok := reply.Content[0].TextEditorToolResult()
			if !ok || value.ToolUseID != "server_editor" {
				t.Fatal("text editor result identity lost")
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
				t.Fatal("forwarded/stored text editor result changed", string(encoded))
			}
			if _, tool := blocks[0].ToolUse(); tool {
				t.Fatal("server result metadata became executable")
			}
		})
	}
	for _, raw := range []string{
		`{"type":"text_editor_code_execution_tool_result","tool_use_id":"id"}`,
		`{"type":"text_editor_code_execution_tool_result","tool_use_id":"id","content":null}`,
		`{"type":"text_editor_code_execution_tool_result","tool_use_id":"id","content":[]}`,
		`{"type":"text_editor_code_execution_tool_result","tool_use_id":"id","content":{"type":"text_editor_code_execution_tool_result_error"}}`,
	} {
		if _, err := decodeBlock([]byte(raw)); err == nil {
			t.Fatal("invalid text editor metadata admitted", raw)
		}
	}
}

func TestStreamClientCallerObservationAdmission(t *testing.T) {
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		caller string
		valid  bool
	}{
		{`null`, true},
		{`{"type":"direct"}`, true},
		{`{"type":"code_execution_20250825","tool_id":"parent"}`, true},
		{`{"type":"code_execution_20260120","tool_id":"parent"}`, true},
		{`"opaque"`, true},
		{`{"type":"future_metadata"}`, true},
		{`{"type":"direct","tool_id":"unexpected"}`, true},
		{`{"type":"code_execution_20260120"}`, true},
		{`{"type":null,"tool_id":"parent"}`, true},
		{`"\udfff"`, true},
		{`{"type":"code_execution_20260120","tool_id":"parent\ud800"}`, true},
		{`{"type":"direct","\udfff":"\ud800"}`, false},
		{`{"type":"\ud800"}`, true},
	} {
		state := streamAssembly{started: true}
		raw := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"client","name":"bash","input":{"command":"true"},"caller":` + row.caller + `}}`
		err := client.consumeStream(&state, "content_block_start", []byte(raw), nil)
		if (err == nil) != row.valid {
			t.Fatal("client caller admission changed", row.caller, err)
		}
		if row.caller == `{"type":"\ud800"}` {
			// The actual fixed SDK supplemental probe accepts this discriminator
			// in both modes, then refuses its next request during UTF-8 encoding.
			if err := client.consumeStream(&state, "content_block_stop", []byte(`{"type":"content_block_stop","index":0}`), nil); err != nil {
				t.Fatal("captured caller discriminator was rejected at block stop", err)
			}
			assertSurrogateContent(t, []protocol.Block{state.blocks[0].block}, `[{"id":"client","caller":{"type":"\ud800"},"input":{"command":"true"},"name":"bash","type":"tool_use"}]`)
			if _, err := json.Marshal(state.blocks[0].block); !errors.Is(err, jsonvalue.ErrSurrogate) {
				t.Fatal("caller discriminator was replaced during wire encoding", err)
			}
		}
	}
}
