package agent

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBashModelOptionsMatchActualSourceConsumersWithNativeEffects(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Field, Name       string
			Input             string `json:"input_json"`
			Enabled           bool   `json:"background_enabled"`
			Trace             []string
			DefaultCandidate  []string `json:"default_candidate"`
			ProposedCandidate []string `json:"proposed_candidate"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 56 {
		t.Fatal("Source consumer inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Field+"/"+row.Name+"/"+map[bool]string{true: "enabled", false: "disabled"}[row.Enabled], func(t *testing.T) {
			input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(DefaultGrantCandidate(input).Tokens(), row.DefaultCandidate) || !slices.Equal(ProposedGrantCandidate(input).Tokens(), row.ProposedCandidate) {
				t.Fatal("Source approval candidate changed")
			}
			s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, row.Enabled)
			out := backgroundDispatch(t, s, input)
			if out.Failed || out.Denied {
				t.Fatal("Source-accepted options failed", out)
			}
			started := strings.HasPrefix(out.Output, "Started background task")
			if started != slices.Contains(row.Trace, "background") {
				t.Fatal("Source foreground/background selection changed", out.Output, row.Trace)
			}
			if !started {
				if out.Output != "field-fixture" {
					t.Fatal("foreground effect changed", out.Output)
				}
				if _, ok := out.CommandResult(); !ok {
					t.Fatal("foreground process metadata missing")
				}
				return
			}
			id := background.ID(strings.TrimSuffix(strings.Fields(out.Output)[3], ":"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			manager := backgroundManager(t, s)
			if err := manager.Wait(ctx, id); err != nil {
				t.Fatal(err)
			}
			result := manager.Check(id)
			if !strings.Contains(result, "field-fixture") {
				t.Fatal("background effect changed", result)
			}
		})
	}
}

func TestBashObservedOptionsDoNotBypassPermission(t *testing.T) {
	s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, true)
	input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(`{"command":"printf field-fixture","run_in_background":"false","approval_prefix":{"allow":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.gate.Dispatch(context.Background(), ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: ModeReadonly}, ToolCall{ID: "denied-options", Input: input})
	if err != nil || !out.Denied || s.backgroundInitialized() {
		t.Fatal("observed metadata supplied execution authority", out, err)
	}
}

func TestNonfiniteBashOptionsAndCommandsCannotGrantReadonlyPermission(t *testing.T) {
	assertNonfiniteShellReadonly(t, "../testdata/python-provider-nonfinite-bash.json", protocol.ToolBash)
}

func TestNonfiniteBackgroundCommandsCannotGrantReadonlyPermission(t *testing.T) {
	assertNonfiniteShellReadonly(t, "../testdata/python-provider-nonfinite-background.json", protocol.ToolBackgroundRun)
}

func assertNonfiniteShellReadonly(t *testing.T, path string, name protocol.ToolName) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Mode  string
			Input string `json:"input_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, true)
	for _, row := range fixture.Cases {
		if row.Mode != "keep" {
			continue
		}
		input, err := protocol.DecodeModelToolInput(name, []byte(row.Input))
		if err != nil {
			t.Fatal(err)
		}
		out, err := s.gate.Dispatch(context.Background(), ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: ModeReadonly}, ToolCall{ID: "nonfinite-readonly", Input: input})
		if err != nil || !out.Denied || s.backgroundInitialized() {
			t.Fatal("observation supplied authority", out, err)
		}
	}
}
