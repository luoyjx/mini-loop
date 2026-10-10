package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type partialSDKCase struct {
	Kind, Name, SSE string
	Snapshots       []string `json:"snapshots_json"`
	Expected        string   `json:"expected_json"`
	Error           *struct{ Class, Message string }
}

func partialSDKCases(t *testing.T) []partialSDKCase {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-provider-partial-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []partialSDKCase }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 98 {
		t.Fatal("SDK inventory changed")
	}
	return fixture.Cases
}

func partialSDKInput(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	expected, err := jsonvalue.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	blocks, ok := expected.Array()
	if !ok || len(blocks) != 1 {
		t.Fatal("unexpected SDK content")
	}
	input, ok := blocks[0].Lookup("input")
	if !ok {
		t.Fatal("SDK input missing")
	}
	return input
}

func TestStreamPartialInputMatchesSDKAtEveryDelta(t *testing.T) {
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range partialSDKCases(t) {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			state := streamAssembly{}
			deltaIndex := 0
			rejected := false
			// This gate checks the boundary accumulation and rejection stage. Final
			// client argument conversion and SDK root/nonfinite adaptations are separate.
			for _, frame := range strings.Split(row.SSE, "\n\n") {
				lines := strings.Split(frame, "\n")
				if len(lines) < 2 {
					continue
				}
				event := strings.TrimPrefix(lines[0], "event: ")
				if event == "content_block_stop" {
					break
				}
				err := client.consumeStream(&state, event, []byte(strings.TrimPrefix(lines[1], "data: ")), nil)
				if event != "content_block_delta" {
					if err != nil {
						t.Fatal(err)
					}
					continue
				}
				if row.Error != nil && deltaIndex == len(row.Snapshots) {
					if err == nil {
						t.Fatal("SDK-rejected input delta was accepted")
					}
					rejected = true
					break
				}
				if err != nil {
					t.Fatal("SDK input delta refused", deltaIndex, err)
				}
				b := state.blocks[0]
				got := b.parsedInput
				if !b.partialInput {
					var err error
					got, err = jsonvalue.Decode(string(b.input))
					if err != nil {
						t.Fatal(err)
					}
				}
				expected := partialSDKInput(t, row.Snapshots[deltaIndex])
				a, _ := jsonvalue.AppendLegacy(nil, got)
				e, _ := jsonvalue.AppendLegacy(nil, expected)
				if !got.PythonEqual(expected) && string(a) != string(e) {
					t.Fatal("SDK accumulation differs", deltaIndex, string(a), string(e))
				}
				deltaIndex++
			}
			if rejected != (row.Error != nil) {
				t.Fatal("SDK rejection stage differs")
			}
			if !rejected && deltaIndex != len(row.Snapshots) {
				t.Fatal("missing delivered SDK snapshot")
			}
		})
	}
}

func TestStreamPartialObjectsMatchSDKFinalAndHistory(t *testing.T) {
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	matched := 0
	for _, row := range partialSDKCases(t) {
		if row.Error != nil {
			continue
		}
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			reply, err := client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
			input := partialSDKInput(t, row.Expected)
			_, finiteErr := input.MarshalJSON()
			if input.Kind() != jsonvalue.Object || finiteErr != nil {
				if err == nil {
					t.Fatal("unsupported root/nonfinite adaptation changed")
				}
				return
			}
			if err != nil {
				t.Fatal("SDK partial object refused", err)
			}
			encoded, err := json.Marshal(reply.Content)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonvalue.Decode(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonvalue.Decode(row.Expected)
			if err != nil || !got.PythonEqual(expected) {
				t.Fatal("SDK stopped object differs", string(encoded))
			}
			message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}
			encoded, err = json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := protocol.DecodeStoredMessage(encoded)
			if err != nil {
				t.Fatal(err)
			}
			blocks, _ := stored.Content.Blocks()
			encoded, err = json.Marshal(blocks)
			if err != nil {
				t.Fatal(err)
			}
			got, err = jsonvalue.Decode(string(encoded))
			if err != nil || !got.PythonEqual(expected) {
				t.Fatal("partial input changed in history")
			}
			if _, clientTool := blocks[0].ToolUse(); row.Kind == "server_tool_use" && clientTool {
				t.Fatal("observational input became executable")
			}
			matched++
		})
	}
	if matched != 68 {
		t.Fatal("finite SDK object inventory changed", matched)
	}
}

func TestStreamPartialInputBoundsBeforeParsing(t *testing.T) {
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	state := streamAssembly{started: true, blocks: []streamBlock{{kind: protocol.BlockServerToolUse, partialInput: true, input: []byte(strings.Repeat(" ", protocol.MaxWireBytes-1)), text: &strings.Builder{}}}}
	raw := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"invalid"}}`
	err = client.consumeStream(&state, "content_block_delta", []byte(raw), nil)
	failure, ok := err.(*Failure)
	if !ok || failure.Kind != FailureLimit {
		t.Fatal("raw bound lost before partial parse", err)
	}
	if len(state.blocks[0].input) > protocol.MaxWireBytes {
		t.Fatal("over-limit input was appended")
	}
}
