package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTeamToolSourceSchemasKeywordsAndIdentities(t *testing.T) {
	var source struct {
		Tools []struct {
			Schema            ToolSchema
			Readonly          bool
			Risk              string
			ParallelSafe      bool   `json:"parallel_safe"`
			ExecutionMode     string `json:"execution_mode"`
			AbsentFromDefault bool   `json:"absent_from_default"`
		}
		Cases []struct {
			Name       ToolName
			InputJSON  string `json:"input_json"`
			Accepted   bool
			Canonical  string
			Sorted     string
			MaskedJSON string `json:"masked_json"`
		}
	}
	data, err := os.ReadFile("../testdata/python-team-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	schemas := TeamSchemas()
	if len(source.Tools) != 10 || len(source.Cases) != 74 {
		t.Fatal("source census drift")
	}
	for i, row := range source.Tools {
		if !reflect.DeepEqual(schemas[i], row.Schema) {
			t.Fatal("schema differs", schemas[i].Name)
		}
		if err := schemas[i].Validate(); err != nil {
			t.Fatal(err)
		}
		readonly := row.Schema.Name == ToolReadInbox || row.Schema.Name == ToolListTeammates || row.Schema.Name == ToolListProtocols
		risk := "write"
		if readonly {
			risk = "read"
		} else if row.Schema.Name == ToolSpawnTeammate {
			risk = "exec"
		}
		if row.Readonly != readonly || row.Risk != risk || row.ParallelSafe || row.ExecutionMode != "exclusive" || !row.AbsentFromDefault {
			t.Fatal("source traits drift", row)
		}
		if _, ok := DefaultToolSchema(row.Schema.Name); ok {
			t.Fatal("became default", row.Schema.Name)
		}
	}
	for _, row := range source.Cases {
		t.Run(string(row.Name)+"/"+string(row.InputJSON), func(t *testing.T) {
			input, err := DecodeToolInput(row.Name, []byte(row.InputJSON))
			if (err == nil) != row.Accepted {
				t.Fatal("keyword binding differs", err)
			}
			if !row.Accepted {
				return
			}
			for _, copy := range []ToolInput{input, input.clone(), MapToolInputStrings(input, nil)} {
				assertTeamIdentity(t, copy, row.Canonical, row.Sorted)
				wire, err := json.Marshal(copy)
				if err != nil {
					t.Fatal(err)
				}
				restored, err := DecodeToolInput(copy.Name(), wire)
				if err != nil {
					t.Fatal(err)
				}
				assertTeamIdentity(t, restored, row.Canonical, row.Sorted)
				// Provider blocks and persisted conversation content use the same decoder.
				block := NewToolUse("team", copy)
				wire, err = json.Marshal(block)
				if err != nil {
					t.Fatal(err)
				}
				var stored Block
				if err := json.Unmarshal(wire, &stored); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(block, stored) {
					t.Fatal("block storage changed variant")
				}
			}
			masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "secret", "hidden") })
			expected, err := DecodeToolInput(row.Name, []byte(row.MaskedJSON))
			if err != nil {
				t.Fatal(err)
			}
			want, _ := expected.CanonicalJSON()
			got, err := masked.CanonicalJSON()
			if err != nil || got != want {
				t.Fatal("source structural mask differs", got, want, err)
			}
			assertTeamIdentity(t, input, row.Canonical, row.Sorted)
		})
	}
	if len(DefaultToolNames()) != 10 {
		t.Fatal("default catalogue changed")
	}
	// Returned schema trees must be detached from later calls and other leaves.
	(*schemas[1].InputSchema.Properties)["metadata"] = InputSchema{Type: SchemaString}
	*schemas[0].InputSchema.Properties = nil
	if !reflect.DeepEqual(TeamSchemas()[1], source.Tools[1].Schema) {
		t.Fatal("schema aliases catalogue")
	}
}

func assertTeamIdentity(t *testing.T, input ToolInput, canonical, sorted string) {
	t.Helper()
	if got, err := input.CanonicalJSON(); err != nil || got != canonical {
		t.Fatal("canonical drift", got, canonical, err)
	}
	if got, err := input.SortedPythonJSON(); err != nil || got != sorted {
		t.Fatal("step identity drift", got, sorted, err)
	}
}

func TestTeamInputsRejectWrongTypesAndRetainDuplicateSemantics(t *testing.T) {
	cases := []struct {
		name ToolName
		raw  string
	}{
		{ToolSpawnTeammate, `{"name":"x","role":1,"prompt":"x"}`},
		{ToolSpawnTeammate, `{"name":null,"role":"x","prompt":"x"}`},
		{ToolSendMessage, `{"to":"x","content":false}`},
		{ToolSendMessage, `{"to":"x","content":"x","type":null}`},
		{ToolSendMessage, `{"to":"x","content":"x","metadata":[]}`},
		{ToolSendMessage, `{"to":"x","content":"x","metadata":false}`},
		{ToolSendMessage, `{"to":"x","content":"x","metadata":{"n":1e400}}`},
		{ToolSendMessage, `{"to":"x","content":"x","metadata":{"s":"\ud800"}}`},
		{ToolSendMessage, `{"to":"x","content":"x","metadata":{"\ud800":1}}`},
		{ToolBroadcast, `{"content":null}`},
		{ToolRequestShutdown, `{"target":"x","reason":null}`},
		{ToolRequestPlan, `{"teammate":"x","task":[]}`},
		{ToolSubmitPlan, `{"plan":0}`},
		{ToolReviewPlan, `{"request_id":"r","approve":"false"}`},
		{ToolReviewPlan, `{"request_id":"r","approve":null}`},
		{ToolReviewPlan, `{"request_id":"r","approve":false,"feedback":null}`},
	}
	for _, row := range cases {
		if _, err := DecodeToolInput(row.name, []byte(row.raw)); err == nil {
			t.Fatal("malformed typed input admitted", row)
		}
	}
	for _, schema := range TeamSchemas() {
		for _, raw := range []string{"", "null", "[]", "true", "{} {}", `{"owner":null}`} {
			if _, err := DecodeToolInput(schema.Name, []byte(raw)); err == nil {
				t.Fatal("invalid boundary admitted", schema.Name, raw)
			}
		}
	}
	raw := `{"to":"bob","content":"x","metadata":{"a":1,"b":2,"a":3}}`
	input, err := DecodeToolInput(ToolSendMessage, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	assertTeamIdentity(t, input, `{"content":"x","metadata":{"a":3,"b":2},"to":"bob"}`, `{"content": "x", "metadata": {"a": 3, "b": 2}, "to": "bob"}`)
	for _, raw := range []string{`null`, `[]`, `true`, `1`, `"x"`} {
		var metadata TeamMetadata
		if err := json.Unmarshal([]byte(raw), &metadata); err == nil {
			t.Fatal("non-object metadata", raw)
		}
	}
}

func TestTeamConstructorsAndAccessorsDetachOptionals(t *testing.T) {
	text := "before"
	var metadata TeamMetadata
	if err := json.Unmarshal([]byte(`{"z":[{"secret":"before"}],"a":1}`), &metadata); err != nil {
		t.Fatal(err)
	}
	inputs := []ToolInput{
		SpawnTeammateToolInput(SpawnTeammateInput{"n", "r", "p"}),
		SendMessageToolInput(SendMessageInput{"bob", "hello", &text, &metadata}),
		ReadInboxToolInput(), BroadcastToolInput(BroadcastInput{"hello"}), ListTeammatesToolInput(),
		RequestShutdownToolInput(RequestShutdownInput{"bob", &text}),
		RequestPlanToolInput(RequestPlanInput{"bob", "task"}), SubmitPlanToolInput(SubmitPlanInput{"plan"}),
		ReviewPlanToolInput(ReviewPlanInput{"req", false, &text}), ListProtocolsToolInput(),
	}
	text = "after"
	if err := json.Unmarshal([]byte(`{"replacement":true}`), &metadata); err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		before, err := input.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(before, "after") || strings.Contains(before, "replacement") {
			t.Fatal("constructor alias", before)
		}
		if v, ok := input.SendMessage(); ok {
			*v.Type = "mutated"
			if err := json.Unmarshal([]byte(`{"mutated":true}`), v.Metadata); err != nil {
				t.Fatal(err)
			}
			// Array()/Keys() are detached; no mutation can change the input's metadata.
			live, _ := input.SendMessage()
			keys := live.Metadata.Value().Keys()
			keys[0] = "changed"
			value, _ := live.Metadata.Value().Lookup("z")
			items, _ := value.Array()
			items[0] = metadata.Value()
		}
		if v, ok := input.RequestShutdown(); ok {
			*v.Reason = "mutated"
		}
		if v, ok := input.ReviewPlan(); ok {
			*v.Feedback = "mutated"
		}
		after, _ := input.CanonicalJSON()
		if after != before {
			t.Fatal("accessor alias", before, after)
		}
	}
	if _, ok := inputs[0].SpawnTeammate(); !ok {
		t.Fatal("spawn discriminator")
	}
	if _, ok := inputs[3].Broadcast(); !ok {
		t.Fatal("broadcast discriminator")
	}
	if _, ok := inputs[6].RequestPlan(); !ok {
		t.Fatal("request discriminator")
	}
	if _, ok := inputs[7].SubmitPlan(); !ok {
		t.Fatal("submit discriminator")
	}
	foreign := ReadInboxToolInput()
	if _, ok := foreign.SpawnTeammate(); ok {
		t.Fatal("foreign spawn")
	}
	if _, ok := foreign.SendMessage(); ok {
		t.Fatal("foreign send")
	}
	if _, ok := foreign.Broadcast(); ok {
		t.Fatal("foreign broadcast")
	}
	if _, ok := foreign.RequestShutdown(); ok {
		t.Fatal("foreign shutdown")
	}
	if _, ok := foreign.RequestPlan(); ok {
		t.Fatal("foreign request")
	}
	if _, ok := foreign.SubmitPlan(); ok {
		t.Fatal("foreign submit")
	}
	if _, ok := foreign.ReviewPlan(); ok {
		t.Fatal("foreign review")
	}
	v := SendMessageToolInput(SendMessageInput{To: "bob", Metadata: &TeamMetadata{}})
	wire, err := json.Marshal(v)
	if err != nil || string(wire) != `{"to":"bob","content":"","metadata":{}}` {
		t.Fatal(string(wire), err)
	}
}
