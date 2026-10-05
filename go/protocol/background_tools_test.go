package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestBackgroundInputsMatchSourceNullIdentityAndDetachedMasking(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-background-tools.json")
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
	if err = json.Unmarshal(data, &source); err != nil || len(source.Variants) != 11 {
		t.Fatal("incomplete source variants", err)
	}
	for _, row := range source.Variants {
		value, err := DecodeToolInput(row.Name, row.Input)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := value.CanonicalJSON()
		if err != nil || canonical != row.Canonical {
			t.Fatal(canonical, row.Canonical, err)
		}
		wire, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeToolInput(row.Name, wire)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := decoded.CanonicalJSON(); got != canonical {
			t.Fatal("optional null identity lost")
		}
		if v, ok := value.BackgroundRun(); ok {
			if v.Timeout != nil {
				*v.Timeout = 999
			}
			if v.ApprovalPrefix != nil && len(*v.ApprovalPrefix) > 0 {
				(*v.ApprovalPrefix)[0] = "mutated"
			}
		}
		if v, ok := value.CheckBackground(); ok && v.ID != nil {
			*v.ID = "mutated"
		}
		if got, _ := value.CanonicalJSON(); got != canonical {
			t.Fatal("accessor aliased live input")
		}
	}
	for _, raw := range []string{`{}`, `{"command":null}`, `{"command":1}`, `{"command":"ok","timeout":"yes"}`, `{"command":"ok","timeout":1.5}`, `{"command":"ok","unknown":true}`, `{"command":"ok","approval_prefix":[1]}`} {
		if _, err := DecodeToolInput(ToolBackgroundRun, []byte(raw)); err == nil {
			t.Fatal("malformed run admitted", raw)
		}
	}
	for _, raw := range []string{`{"bg_id":7}`, `{"extra":true}`} {
		if _, err := DecodeToolInput(ToolCheckBackground, []byte(raw)); err == nil {
			t.Fatal("malformed check admitted")
		}
	}
	for name, raw := range map[ToolName]string{ToolBackgroundRun: `{"command":"secret","timeout":null,"approval_prefix":["secret","prefix"]}`, ToolCheckBackground: `{"bg_id":"bg_secret"}`} {
		value, err := DecodeToolInput(name, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		masked := MapToolInputStrings(value, func(s string) string { return strings.ReplaceAll(s, "secret", "hidden") })
		data, _ := json.Marshal(masked)
		if strings.Contains(string(data), "secret") {
			t.Fatal("secret recording escaped")
		}
		original, _ := value.CanonicalJSON()
		if !strings.Contains(original, "secret") {
			t.Fatal("mask altered execution")
		}
	}
	if len(DefaultToolNames()) != 10 {
		t.Fatal("optional tools became defaults")
	}
}
