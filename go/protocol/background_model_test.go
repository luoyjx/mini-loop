package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestBackgroundModelObservationsRetainHistoryMaskAndIdentity(t *testing.T) {
	for _, raw := range []string{
		`null`, `["secret-value",{"secret-key":1}]`, `"secret-value"`, `17`, `true`, `{}`,
		`{"command":null}`, `{"command":["secret-value"],"timeout":null,"approval_prefix":null}`,
		`{"command":{"secret-key":"secret-value"},"timeout":3}`,
		`{"command":"printf true","z_extra":"secret-value","a_extra":2}`,
	} {
		t.Run(raw, func(t *testing.T) {
			input, err := DecodeModelToolInput(ToolBackgroundRun, []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeToolInput(ToolBackgroundRun, []byte(raw)); err == nil {
				t.Fatal("strict explicit admission changed")
			}
			expected, err := jsonvalue.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []ToolInput{input, input.clone()} {
				wire, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				observed, err := jsonvalue.Decode(string(wire))
				if err != nil || !observed.PythonEqual(expected) {
					t.Fatal("observed input changed", string(wire), err)
				}
				canonical, err := value.CanonicalJSON()
				want, wantErr := PythonJSON(expected.Sorted(), false, true)
				if err != nil || wantErr != nil || canonical != want {
					t.Fatal("step identity changed", canonical, want, err)
				}
				message := Message{Role: RoleAssistant, Content: BlockContent(NewToolUse("background-input", value))}
				encoded, err := json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
				stored, err := DecodeStoredMessage(encoded)
				if err != nil {
					t.Fatal(err)
				}
				blocks, _ := stored.Content.Blocks()
				use, _ := blocks[0].ToolUse()
				again, err := json.Marshal(use.Input)
				if err != nil || string(again) != string(wire) {
					t.Fatal("history lost observation", string(again), err)
				}
				before, fault := value.BackgroundBindingFailure()
				after, restored := use.Input.BackgroundBindingFailure()
				if fault != restored || (fault && before.Error() != after.Error()) {
					t.Fatal("history lost binding stage")
				}
			}
			masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "secret", "masked") })
			projection, err := json.Marshal(masked)
			if err != nil || strings.Contains(string(projection), "secret") {
				t.Fatal("secret in observation projection", string(projection), err)
			}
			live, err := json.Marshal(input)
			observed, decodeErr := jsonvalue.Decode(string(live))
			if err != nil || decodeErr != nil || !observed.PythonEqual(expected) {
				t.Fatal("mask mutated executable input", string(live), err)
			}
		})
	}
	for _, raw := range []string{`{"command":NaN}`, `{"command":{"nested":Infinity}}`} {
		input, err := DecodeModelToolInput(ToolBackgroundRun, []byte(raw))
		if err != nil {
			t.Fatal("SDK observation refused", err)
		}
		if _, err := json.Marshal(input); err == nil {
			t.Fatal("nonfinite input entered finite encoding")
		}
		if _, err := DecodeToolInput(ToolBackgroundRun, []byte(raw)); err == nil {
			t.Fatal("strict caller admitted nonfinite input")
		}
	}
}
