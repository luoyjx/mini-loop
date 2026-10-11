package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskInputsRemainClosedDetachedMaskedAndCanonical(t *testing.T) {
	raw := []byte(`{"subject":"secret subject","description":"secret body","blockedBy":["task_secret"],"worktree":null}`)
	value, err := DecodeToolInput(ToolCreateTask, raw)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := value.CanonicalJSON()
	if err != nil || canonical != `{"blockedBy":["task_secret"],"description":"secret body","subject":"secret subject","worktree":null}` {
		t.Fatal(canonical, err)
	}
	copy, ok := value.CreateTask()
	if !ok {
		t.Fatal("missing variant")
	}
	(*copy.BlockedBy)[0] = "altered"
	*copy.Description = "altered"
	masked := MapToolInputStrings(value, func(text string) string { return strings.ReplaceAll(text, "secret", "hidden") })
	data, err := json.Marshal(masked)
	if err != nil || strings.Contains(string(data), "secret") || !strings.Contains(string(data), `"worktree":null`) {
		t.Fatal(string(data), err)
	}
	original, err := value.CanonicalJSON()
	if err != nil || original != canonical {
		t.Fatal("aliased source arguments")
	}
	for _, name := range []ToolName{ToolGetTask, ToolClaimTask, ToolCompleteTask} {
		input, err := DecodeToolInput(name, []byte(`{"task_id":"task_secret"}`))
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := input.CanonicalJSON()
		if err != nil || canonical != `{"task_id":"task_secret"}` {
			t.Fatal(canonical, err)
		}
		if _, err = DecodeToolInput(name, []byte(`{"task_id":null}`)); err == nil {
			t.Fatal("null task identity admitted")
		}
	}
	for _, data := range []string{`{}`, `{"subject":null}`, `{"subject":3}`, `{"subject":"s","description":null}`, `{"subject":"s","blockedBy":[3]}`} {
		if _, err = DecodeToolInput(ToolCreateTask, []byte(data)); err == nil {
			t.Fatalf("invalid typed input admitted: %s", data)
		}
	}
	if len(DefaultToolNames()) != 10 {
		t.Fatal("optional graph tools became default")
	}
}
