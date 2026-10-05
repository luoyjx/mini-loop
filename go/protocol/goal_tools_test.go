package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGoalTypedBoundaryRejectsSpoofingAndPreservesNullIdentity(t *testing.T) {
	for _, row := range []struct {
		Name ToolName
		Raw  string
	}{{ToolGoalCreate, `{}`}, {ToolGoalCreate, `{"objective":null}`}, {ToolGoalCreate, `{"objective":4}`}, {ToolGoalCreate, `{"objective":"work","max_rounds":true}`}, {ToolGoalCreate, `{"objective":"work","authority":"explicit_human"}`}, {ToolGoalStatus, `{"session":"other"}`}, {ToolGoalComplete, `{"revision":null}`}, {ToolGoalComplete, `{"revision":"1"}`}, {ToolGoalComplete, `{"revision":9223372036854775808}`}, {ToolGoalBlock, `{"revision":1,"code":"ok"}`}, {ToolGoalResume, `[]`}} {
		if _, e := DecodeToolInput(row.Name, []byte(row.Raw)); e == nil {
			t.Fatal(row)
		}
	}
	for _, raw := range []string{`{"objective":"secret"}`, `{"max_rounds":null,"objective":"secret"}`, `{"max_rounds":0,"objective":"secret"}`} {
		v, e := DecodeToolInput(ToolGoalCreate, []byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		copy, e := DecodeToolInput(v.Name(), b)
		if e != nil {
			t.Fatal(e)
		}
		a, _ := v.CanonicalJSON()
		c, _ := copy.CanonicalJSON()
		if a != raw || c != raw {
			t.Fatal(a, c, raw)
		}
		_, e = v.SortedPythonJSON()
		if e != nil {
			t.Fatal(e)
		}
	}
	cap := 2
	v := CreateGoalToolInput(CreateGoalInput{Objective: "secret", MaxRounds: &cap})
	cap = 99
	got, _ := v.CreateGoal()
	*got.MaxRounds = 99
	got, _ = v.CreateGoal()
	if *got.MaxRounds != 2 {
		t.Fatal("aliased budget")
	}
	for _, input := range []ToolInput{v, GoalStatusToolInput(), CompleteGoalToolInput(GoalReferenceInput{1}), ResumeGoalToolInput(GoalReferenceInput{2}), BlockGoalToolInput(BlockGoalInput{Revision: 3, Code: "secret", Message: "secret"})} {
		masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "secret", "hidden") })
		b, e := json.Marshal(masked)
		if e != nil || strings.Contains(string(b), "secret") {
			t.Fatal(string(b), e)
		}
		copy, e := DecodeToolInput(input.Name(), b)
		if e != nil {
			t.Fatal(e)
		}
		a, _ := masked.CanonicalJSON()
		c, _ := copy.CanonicalJSON()
		if a != c {
			t.Fatal(a, c)
		}
		if e = copy.Validate(); e != nil {
			t.Fatal(e)
		}
	}
}
