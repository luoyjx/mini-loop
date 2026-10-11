package protocol

import (
	"strings"
	"testing"
)

func TestOptionalNullSurvivesWireMaskingAndCanonicalIdentity(t *testing.T) {
	for _, item := range []struct {
		name                       ToolName
		explicit, absent, fragment string
	}{
		{ToolBash, `{"command":"echo x","run_in_background":null,"approval_prefix":null}`, `{"command":"echo x"}`, `"run_in_background":null`},
		{ToolReadFile, `{"path":"x","limit":null,"offset":null}`, `{"path":"x"}`, `"limit":null`},
		{ToolTask, `{"prompt":"x","agent_type":null}`, `{"prompt":"x"}`, `"agent_type":null`},
		{ToolLoadSkill, `{"name":"x","scope":null}`, `{"name":"x"}`, `"scope":null`},
	} {
		explicit, err := DecodeToolInput(item.name, []byte(item.explicit))
		if err != nil {
			t.Fatal(err)
		}
		absent, err := DecodeToolInput(item.name, []byte(item.absent))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := explicit.MarshalJSON()
		if err != nil || !strings.Contains(string(encoded), item.fragment) {
			t.Fatalf("%s null lost: %s, %v", item.name, encoded, err)
		}
		canonical, err := explicit.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		other, _ := absent.CanonicalJSON()
		if canonical == other || !strings.Contains(canonical, item.fragment) {
			t.Fatal("absent and null identities collapsed")
		}
		masked := MapToolInputStrings(explicit, func(value string) string { return strings.ReplaceAll(value, "x", "masked") })
		projection, err := masked.MarshalJSON()
		if err != nil || !strings.Contains(string(projection), item.fragment) {
			t.Fatal("recording copy erased null presence")
		}
		again, err := DecodeToolInput(item.name, encoded)
		if err != nil {
			t.Fatal(err)
		}
		roundtrip, _ := again.CanonicalJSON()
		if roundtrip != canonical {
			t.Fatal("wire roundtrip changed identity")
		}
	}
}
