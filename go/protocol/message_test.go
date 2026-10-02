package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// These are the wire shapes emitted by Python's fake model for a bash turn.
const fakeTurn = `[
  {"role":"user","content":"inspect this repository"},
  {"role":"assistant","content":[
    {"type":"text","text":"Working on it."},
    {"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"echo handled: inspect this repository"}}
  ]},
  {"role":"user","content":[
    {"type":"tool_result","tool_use_id":"toolu_1","content":"handled: inspect this repository"}
  ]},
  {"role":"assistant","content":[{"type":"text","text":"Done. Tool said: handled: inspect this repository"}]}
]`

func TestPythonFakeTurnRoundTrip(t *testing.T) {
	var messages []Message
	if err := json.Unmarshal([]byte(fakeTurn), &messages); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	blocks, ok := messages[1].Content.Blocks()
	if !ok || len(blocks) != 2 {
		t.Fatal("assistant tool content was not decoded as blocks")
	}
	use, ok := blocks[1].ToolUse()
	input, bash := use.Input.Bash()
	if !ok || !bash || input.Command != "echo handled: inspect this repository" {
		t.Fatalf("typed bash input lost: %+v", use)
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	var restored []Message
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if err := ValidateTranscript(restored); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsUnknownToolAndInputFields(t *testing.T) {
	for _, block := range []string{
		`{"type":"tool_use","id":"toolu_1","name":"mystery","input":{"command":"echo x"}}`,
		`{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"echo x","opaque":1}}`,
		`{"type":"tool_use","id":"toolu_1","name":"bash","input":"echo x"}`,
		`{"type":"tool_use","id":"toolu_1","name":"bash","input":{}}`,
		`{"type":"tool_use","id":"toolu_1","name":"bash"}`,
	} {
		var got Block
		if err := json.Unmarshal([]byte(block), &got); err == nil {
			t.Errorf("accepted unsupported tool payload: %s", block)
		}
	}
	var background Block
	if err := json.Unmarshal([]byte(`{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"echo x","run_in_background":true}}`), &background); err != nil {
		t.Fatal(err)
	}
	use, _ := background.ToolUse()
	input, _ := use.Input.Bash()
	if input.RunInBackground == nil || !*input.RunInBackground {
		t.Fatal("explicit background flag was lost")
	}
	*input.RunInBackground = false
	useAgain, _ := background.ToolUse()
	inputAgain, _ := useAgain.Input.Bash()
	if !*inputAgain.RunInBackground {
		t.Fatal("caller changed a decoded block through its accessor")
	}
}

func TestRejectsUnsignedThinkingAndUnpairedTools(t *testing.T) {
	var unsigned Block
	if err := json.Unmarshal([]byte(`{"type":"thinking","thinking":"reason","signature":""}`), &unsigned); err == nil {
		t.Fatal("unsigned thinking block accepted")
	}
	cases := [][]Message{
		{{Role: RoleAssistant, Content: BlockContent(NewBashUse("u1", "echo x"))}},
		{{Role: RoleUser, Content: BlockContent(NewToolResult("u1", "x", false))}},
		{
			{Role: RoleAssistant, Content: BlockContent(NewBashUse("u1", "echo x"))},
			{Role: RoleUser, Content: BlockContent(NewToolResult("wrong", "x", false))},
		},
	}
	for i, messages := range cases {
		if err := ValidateTranscript(messages); err == nil {
			t.Errorf("case %d accepted unpaired tool blocks", i)
		}
	}
}

func TestContentBoundary(t *testing.T) {
	for _, wire := range []string{
		`{"role":"user","content":[]}`,
		`{"role":"user","content":{}}`,
		`{"role":"system","content":"x"}`,
		`{"role":"user","content":[{"type":"text","text":"x","extra":1}]}`,
	} {
		var message Message
		if err := json.Unmarshal([]byte(wire), &message); err == nil {
			t.Errorf("accepted invalid content: %s", wire)
		}
	}
	var empty Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":""}`), &empty); err != nil {
		t.Fatalf("Python-compatible empty user string rejected: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"role":"user","content":"`+strings.Repeat("x", MaxWireBytes)+`"}`), &empty); err == nil {
		t.Fatal("oversized message accepted")
	}
}
