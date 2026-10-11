package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func TestPythonDefaultToolInventory(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-contract-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		DefaultToolNames []ToolName `json:"default_tool_names"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest.DefaultToolNames, DefaultToolNames()) {
		t.Fatalf("Python default tools changed: Python=%v Go=%v", manifest.DefaultToolNames, DefaultToolNames())
	}
	names := DefaultToolNames()
	names[0] = "overwritten"
	if DefaultToolNames()[0] != ToolBash {
		t.Fatal("caller changed the default tool inventory")
	}
}

func TestEveryDefaultToolHasTypedWireInput(t *testing.T) {
	cases := []struct {
		name ToolName
		body string
	}{
		{ToolBash, `{"command":"echo hi","run_in_background":false,"approval_prefix":["git","pull"]}`},
		{ToolReadFile, `{"path":"a.txt","limit":20,"offset":2}`},
		{ToolWriteFile, `{"path":"a.txt","content":""}`},
		{ToolEditFile, `{"path":"a.txt","old_text":"before","new_text":"after"}`},
		{ToolGlob, `{"pattern":"**/*.go"}`},
		{ToolTodoWrite, `{"items":[{"content":"work","status":"in_progress","activeForm":"Working"}]}`},
		{ToolTask, `{"prompt":"inspect","agent_type":"Explore"}`},
		{ToolLoadSkill, `{"name":"code_review","scope":"agent"}`},
		{ToolCompress, `{}`},
		{ToolAskUser, `{"question":"Which branch?"}`},
	}
	for _, tc := range cases {
		t.Run(string(tc.name), func(t *testing.T) {
			input, err := DecodeToolInput(tc.name, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if input.Name() != tc.name {
				t.Fatalf("decoded %q as %q", tc.name, input.Name())
			}
			wire := fmt.Sprintf(`{"type":"tool_use","id":"u1","name":%q,"input":%s}`, tc.name, tc.body)
			var block Block
			if err := json.Unmarshal([]byte(wire), &block); err != nil {
				t.Fatal(err)
			}
			use, ok := block.ToolUse()
			if !ok || use.Name != tc.name || use.Input.Name() != tc.name {
				t.Fatalf("wrong typed block: %+v", use)
			}
			encoded, err := json.Marshal(block)
			if err != nil {
				t.Fatal(err)
			}
			var restored Block
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if err := restored.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestToolInputsRejectUnknownMissingAndInvalidValues(t *testing.T) {
	cases := []struct {
		name ToolName
		body string
	}{
		{ToolBash, `{}`},
		{ToolBash, `{"command":"echo","approval_prefix":"git"}`},
		{ToolReadFile, `{"limit":1}`},
		{ToolWriteFile, `{"path":"a"}`},
		{ToolEditFile, `{"path":"a","old_text":"x"}`},
		{ToolGlob, `{}`},
		{ToolTodoWrite, `{}`},
		{ToolTodoWrite, `{"items":[{"content":"x","status":"bogus","activeForm":"x"}]}`},
		{ToolTodoWrite, `{"items":[{"content":"x","status":"pending"}]}`},
		{ToolTask, `{"prompt":"x","agent_type":"unknown"}`},
		{ToolLoadSkill, `{"name":"x","scope":"unknown"}`},
		{ToolCompress, `{"surprise":true}`},
		{ToolAskUser, `{}`},
		{ToolAskUser, `null`},
		{ToolName("unknown"), `{}`},
	}
	for _, tc := range cases {
		if _, err := DecodeToolInput(tc.name, []byte(tc.body)); err == nil {
			t.Errorf("accepted %q with %s", tc.name, tc.body)
		}
	}
}

func TestToolInputCannotBeDecodedWithoutItsName(t *testing.T) {
	var input ToolInput
	if err := json.Unmarshal([]byte(`{"command":"echo hi"}`), &input); err == nil {
		t.Fatal("unnamed input silently decoded to an empty tool variant")
	}
}

func TestOptionalValuesSurviveWithoutLeakingMutation(t *testing.T) {
	input, err := DecodeToolInput(ToolReadFile, []byte(`{"path":"a","limit":0,"offset":0}`))
	if err != nil {
		t.Fatal(err)
	}
	first, ok := input.ReadFile()
	if !ok || first.Limit == nil || first.Offset == nil || *first.Limit != 0 || *first.Offset != 0 {
		t.Fatalf("explicit zeros were lost: %+v", first)
	}
	*first.Limit = 20
	second, _ := input.ReadFile()
	if *second.Limit != 0 {
		t.Fatal("caller changed an immutable tool input through an optional pointer")
	}
	bare, err := DecodeToolInput(ToolReadFile, []byte(`{"path":"a"}`))
	if err != nil {
		t.Fatal(err)
	}
	missing, _ := bare.ReadFile()
	if missing.Limit != nil || missing.Offset != nil {
		t.Fatal("absent values were turned into explicit zeroes")
	}
}

func TestConstructorsDetachOptionalPointersAndSlices(t *testing.T) {
	background := true
	prefix := []string{"git", "pull"}
	value := BashToolInput(BashInput{
		Command: "git pull", RunInBackground: &background, ApprovalPrefix: &prefix,
	})
	background = false
	prefix[0] = "python"
	got, _ := value.Bash()
	if !*got.RunInBackground || (*got.ApprovalPrefix)[0] != "git" {
		t.Fatal("constructor retained caller-owned optional values")
	}
	items := []TodoItem{{Content: "work", Status: TodoPending, ActiveForm: "Working"}}
	todo := TodoWriteToolInput(TodoWriteInput{Items: items})
	items[0].Content = "changed"
	gotTodo, _ := todo.TodoWrite()
	if gotTodo.Items[0].Content != "work" {
		t.Fatal("constructor retained caller-owned item slice")
	}
}
