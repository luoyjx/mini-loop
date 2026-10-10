package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestNonTextBashDisplayMatchesActualSourceEvents(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-command.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name   string
			Input  string `json:"input_json"`
			Events []struct{ Display ToolDisplay }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 36 {
		t.Fatal("actual Source display inventory changed")
	}
	for i, row := range fixture.Cases {
		input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.Input))
		if err != nil {
			t.Fatal(err)
		}
		if got := ToolLabel(input); got != row.Events[0].Display {
			t.Fatal(i, row.Name, "Source tool_use display changed", got, row.Events[0].Display)
		}
	}
}

func TestBashDisplayUsesMaskedObservationsBeforePreviewAndKeepsLiveInput(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-command.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Labels []struct {
			Name    string
			Input   string `json:"input_json"`
			Masked  string `json:"masked_input_json"`
			Display ToolDisplay
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Labels) != 12 {
		t.Fatal("Source display projection inventory changed")
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("DISPLAY_KEY", "sk-display-key-123")
	registry.RegisterValue("DISPLAY_VALUE", "sk-display-value-123")
	for _, row := range fixture.Labels {
		t.Run(row.Name, func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			live := SessionEvent{kind: EventToolUse, toolUse: ToolUseEvent{Name: protocol.ToolBash, ID: "display", Input: input, Display: ToolLabel(input)}}
			projected := maskedEvent(registry, live)
			if projected.toolUse.Display != row.Display {
				t.Fatal("Source masked display changed", projected.toolUse.Display, row.Display)
			}
			encoded, err := json.Marshal(projected.toolUse.Input)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonvalue.Decode(string(encoded))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := jsonvalue.Decode(row.Masked)
			if err != nil {
				t.Fatal(err)
			}
			gotWire, err := got.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			expectedWire, err := expected.MarshalJSON()
			if err != nil || !bytes.Equal(gotWire, expectedWire) {
				t.Fatal("Source masked value or kind changed", string(gotWire), err)
			}
			wire, err := json.Marshal(projected)
			if err != nil || strings.Contains(string(wire), "sk-display-") {
				t.Fatal("event projection leaked a registered secret", string(wire), err)
			}
			after, err := json.Marshal(live.toolUse.Input)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatal("display projection changed live command", string(after), err)
			}
		})
	}
}

func TestBackgroundDisplayMatchesMaskedSourceAndPreservesLiveCommand(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Labels []struct {
			Name    string
			Input   protocol.BackgroundRunInput
			Masked  protocol.BackgroundRunInput
			Display ToolDisplay
		} `json:"background_labels"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Labels) != 14 {
		t.Fatal("Source background display inventory changed")
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("BACKGROUND_DISPLAY", "sk-background-display-123")
	for _, row := range fixture.Labels {
		t.Run(row.Name, func(t *testing.T) {
			input := protocol.BackgroundRunToolInput(row.Input)
			live := SessionEvent{kind: EventToolUse, toolUse: ToolUseEvent{Name: protocol.ToolBackgroundRun, ID: "background-display", Input: input, Display: ToolLabel(input)}}
			projected := maskedEvent(registry, live)
			if projected.toolUse.Display != row.Display {
				t.Fatal("Source masked background display changed", projected.toolUse.Display, row.Display)
			}
			masked, ok := projected.toolUse.Input.BackgroundRun()
			if !ok || masked.Command != row.Masked.Command {
				t.Fatal("masked background input changed", masked.Command, row.Masked.Command)
			}
			original, ok := live.toolUse.Input.BackgroundRun()
			if !ok || original.Command != row.Input.Command {
				t.Fatal("display projection changed executable command")
			}
			wire, err := json.Marshal(projected)
			if err != nil || strings.Contains(string(wire), "sk-background-display-123") {
				t.Fatal("event projection leaked a registered secret", string(wire), err)
			}
		})
	}
}
