package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCronInputsMatchSourceIdentityAndDetachFlags(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-cron-surfaces.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Variants []struct {
			Name      ToolName
			Input     json.RawMessage
			Canonical string
		}
	}
	if err = json.Unmarshal(data, &f); err != nil || len(f.Variants) != 6 {
		t.Fatal(err)
	}
	for _, row := range f.Variants {
		v, err := DecodeToolInput(row.Name, row.Input)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := v.CanonicalJSON(); err != nil || got != row.Canonical {
			t.Fatal(got, row.Canonical, err)
		}
		if input, ok := v.ScheduleCron(); ok {
			if input.Recurring != nil {
				*input.Recurring = !*input.Recurring
			}
			if input.Durable != nil {
				*input.Durable = !*input.Durable
			}
		}
		wire, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		copy, err := DecodeToolInput(row.Name, wire)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := copy.CanonicalJSON(); got != row.Canonical {
			t.Fatal("aliased flags or lost absent/false", got)
		}
	}
	v, _ := DecodeToolInput(ToolScheduleCron, []byte(`{"cron":"secret","prompt":"secret"}`))
	masked := MapToolInputStrings(v, func(s string) string { return strings.ReplaceAll(s, "secret", "hidden") })
	if b, _ := json.Marshal(masked); strings.Contains(string(b), "secret") {
		t.Fatal(string(b))
	}
	if b, _ := json.Marshal(v); !strings.Contains(string(b), "secret") {
		t.Fatal("changed live input")
	}
	for _, raw := range []string{`{}`, `{"cron":null,"prompt":"x"}`, `{"cron":"* * * * *","prompt":"x","session_id":"foreign"}`, `{"cron":"* * * * *","prompt":"x","recurring":null}`, `{"cron":"* * * * *","prompt":"x","durable":"yes"}`} {
		if _, err := DecodeToolInput(ToolScheduleCron, []byte(raw)); err == nil {
			t.Fatal("malformed model input admitted", raw)
		}
	}
	for _, name := range []ToolName{ToolListCrons, ToolCancelCron, "arm_cron"} {
		if _, err := DecodeToolInput(name, []byte(`{"session_id":"foreign"}`)); err == nil {
			t.Fatal(name)
		}
	}
	if len(DefaultToolNames()) != 10 {
		t.Fatal("cron became default")
	}
}
