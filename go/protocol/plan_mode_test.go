package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPlanModeBoundaryRefusesAuthorityAndMalformedInputs(t *testing.T) {
	for _, name := range []ToolName{ToolEnterPlanMode, ToolExitPlanMode} {
		for _, raw := range []string{`[]`, `null`, `{"plan":null}`, `{"plan":42}`, `{"session":"foreign"}`, `{"plan":"# ok","approved":true}`} {
			if _, err := DecodeToolInput(name, []byte(raw)); err == nil {
				t.Fatal(name, raw)
			}
		}
	}
	if _, err := DecodeToolInput(ToolExitPlanMode, []byte(`{}`)); err == nil {
		t.Fatal("missing plan")
	}
	v := ExitPlanModeToolInput(ExitPlanModeInput{Plan: "# secret\n步骤"})
	masked := MapToolInputStrings(v, func(s string) string { return strings.ReplaceAll(s, "secret", "hidden") })
	for _, input := range []ToolInput{v, masked, EnterPlanModeToolInput()} {
		b, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		copy, err := DecodeToolInput(input.Name(), b)
		if err != nil {
			t.Fatal(err)
		}
		canonical, _ := input.CanonicalJSON()
		got, _ := copy.CanonicalJSON()
		if got != canonical {
			t.Fatal(got, canonical)
		}
		if _, err := input.SortedPythonJSON(); err != nil {
			t.Fatal(err)
		}
	}
	live, _ := v.ExitPlanMode()
	projection, _ := masked.ExitPlanMode()
	if live.Plan != "# secret\n步骤" || projection.Plan != "# hidden\n步骤" {
		t.Fatal(live, projection)
	}
	if len(DefaultToolNames()) != 10 {
		t.Fatal("plan tools became default")
	}
}
