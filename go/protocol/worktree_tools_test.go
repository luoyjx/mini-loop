package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestWorktreeInputsMatchSourceIdentityAndStayDetached(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-worktree-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Variants []struct {
			Name      ToolName
			Input     json.RawMessage
			Canonical string
		}
	}
	if err = json.Unmarshal(data, &source); err != nil || len(source.Variants) != 8 {
		t.Fatal("incomplete source input corpus", err)
	}
	for _, sample := range source.Variants {
		input, err := DecodeToolInput(sample.Name, sample.Input)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := input.CanonicalJSON()
		if err != nil || canonical != sample.Canonical {
			t.Fatalf("%s: %s / %s / %v", sample.Name, canonical, sample.Canonical, err)
		}
		wire, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		copy, err := DecodeToolInput(sample.Name, wire)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := copy.CanonicalJSON(); got != canonical {
			t.Fatal("lost null/default identity")
		}
		if v, ok := input.CreateWorktree(); ok && v.TaskID != nil {
			*v.TaskID = "mutated"
		}
		if v, ok := input.RemoveWorktree(); ok && v.DiscardChanges != nil {
			*v.DiscardChanges = !*v.DiscardChanges
		}
		if got, _ := input.CanonicalJSON(); got != canonical {
			t.Fatal("aliased accessor")
		}
	}
	input, err := DecodeToolInput(ToolCreateWorktree, []byte(`{"name":"secret","task_id":"task_secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	masked := MapToolInputStrings(input, func(v string) string { return strings.ReplaceAll(v, "secret", "hidden") })
	data, err = json.Marshal(masked)
	if err != nil || strings.Contains(string(data), "secret") {
		t.Fatal(string(data), err)
	}
	if got, _ := input.CanonicalJSON(); !strings.Contains(got, "secret") {
		t.Fatal("record mask altered live execution")
	}
	for _, name := range []ToolName{ToolCreateWorktree, ToolRemoveWorktree, ToolKeepWorktree, ToolEnterWorktree} {
		for _, raw := range []string{`{}`, `{"name":null}`, `{"name":42}`, `{"name":"one","unknown":true}`} {
			if _, err := DecodeToolInput(name, []byte(raw)); err == nil {
				t.Fatalf("admitted malformed %s %s", name, raw)
			}
		}
	}
	if _, err := DecodeToolInput(ToolRemoveWorktree, []byte(`{"name":"one","discard_changes":"yes"}`)); err == nil {
		t.Fatal("nonboolean discard admitted")
	}
	if len(DefaultToolNames()) != 10 {
		t.Fatal("optional tools became defaults")
	}
}
