package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWholeUsageIngressAndArchiveKeepAdmissionAtomic(t *testing.T) {
	for _, raw := range []string{"NaN", "Infinity", "[NaN]", `{"input_tokens":NaN}`, strings.Repeat(" ", MaxWireBytes+1)} {
		if _, err := DecodeSDKUsage([]byte(raw), false); err == nil {
			t.Fatal("invalid SDK usage accepted", raw[:min(len(raw), 40)])
		}
	}
	for _, raw := range []string{`{"input_tokens":1}`, `{"value":13}`, "[]", "NaN"} {
		usage := TokenUsage{InputTokens: IntegerUsageCount(7), OutputTokens: IntegerUsageCount(8)}
		if err := json.Unmarshal([]byte(raw), &usage); err == nil {
			t.Fatal("invalid native/archive usage admitted")
		}
		if usage.InputTokens.Metered() != 7 || usage.OutputTokens.Metered() != 8 {
			t.Fatal("failed archive decode replaced counts")
		}
	}
	partial, err := DecodeSDKUsage([]byte(`{"input_tokens":1}`), false)
	if err != nil || partial.InputTokens.Metered() != 1 || partial.OutputTokens.Kind() != UsageNull {
		t.Fatal("SDK partial model construction differs", err)
	}
	for _, raw := range []string{"null", "13", `"secret-marker"`} {
		usage, err := DecodeSDKUsage([]byte(raw), false)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(usage)
		clone := (ModelReply{Usage: usage}).Clone().Usage.MapStrings(func(s string) string { return strings.ReplaceAll(s, "secret-marker", "masked") })
		if clone.HasUsageCounts() {
			t.Fatal("whole usage gained count authority")
		}
		after, _ := json.Marshal(usage)
		if string(before) != string(after) {
			t.Fatal("whole usage clone mutated original")
		}
	}
}
