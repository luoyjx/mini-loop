package agent_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
	"github.com/luoyjx/mini-loop/go/sandbox"
	"github.com/luoyjx/mini-loop/go/shell"
)

type bashEffectCase struct {
	Name         string `json:"name"`
	Mode         string `json:"sandbox_mode"`
	CommandJSON  string `json:"command_json_ascii"`
	SelectedJSON string `json:"selected_argv_json_ascii"`
	SandboxCalls int    `json:"sandbox_calls"`
	Result       *struct {
		Rendered string `json:"rendered"`
		ExitCode *int   `json:"exit_code"`
	} `json:"result"`
	Exception *struct {
		Class   string `json:"exception_class"`
		Message string `json:"message"`
	} `json:"exception"`
	MarkerHex   *string `json:"marker_hex"`
	ArgumentHex *string `json:"argument_hex"`
}

type bashEffectSandbox struct {
	mode     string
	selected []string
	received string
	calls    int
}

func (s *bashEffectSandbox) ForWorkspace(string) (shell.Sandbox, error) { return s, nil }
func (s *bashEffectSandbox) Argv(command string) ([]string, error) {
	s.received, s.calls = command, s.calls+1
	if s.mode == "null" {
		return (&sandbox.NullSandbox{}).Argv(command)
	}
	return append([]string(nil), s.selected...), nil
}

func bashEffectText(t *testing.T, data string) string {
	t.Helper()
	value, err := jsonvalue.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	text, ok := value.Text()
	if !ok {
		t.Fatal("Source command is not text")
	}
	return text
}

func bashEffectArgv(t *testing.T, data string) []string {
	t.Helper()
	value, err := jsonvalue.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := value.Array()
	if !ok {
		t.Fatal("Source selected argv is not an array")
	}
	result := make([]string, len(items))
	for i, item := range items {
		var valid bool
		result[i], valid = item.Text()
		if !valid {
			t.Fatal("Source selected argv element is not text")
		}
	}
	return result
}

func bashEffectFile(t *testing.T, root, name string) *string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	value := hex.EncodeToString(data)
	return &value
}

// This starts at the real SDK decoder and reaches the real execution gate with
// no journal. A journal fingerprint rejection cannot substitute for the shell
// effect boundary; all marker paths are private to this single test case.
func TestSDKSurrogateBashReachesSafeFilesystemBoundaryWithoutJournal(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-bash-surrogates.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Encoding string           `json:"filesystem_encoding"`
		Errors   string           `json:"filesystem_errors"`
		Cases    []bashEffectCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 12 || fixture.Encoding != "utf-8" || fixture.Errors != "surrogateescape" {
		t.Fatal("Source shell profile changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			var requests atomic.Uint64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"bash-effect-reply","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"tool_use","id":"bash-effect-call","name":"bash","input":{"command":%s}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, row.CommandJSON)
			}))
			defer upstream.Close()
			zero := 0
			model, err := provider.New(provider.Config{APIKey: "fixture-key", BaseURL: upstream.URL, MaxRetries: &zero})
			if err != nil {
				t.Fatal(err)
			}
			defer model.CloseIdleConnections()
			reply, err := model.Complete(context.Background(), protocol.ModelRequest{Model: "fixture-model", MaxTokens: 64, Purpose: protocol.PurposeAgentTurn,
				Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("run the owned fixture")}}})
			if err != nil || len(reply.Content) != 1 || requests.Load() != 1 {
				t.Fatalf("actual provider ingress: %+v %v", reply, err)
			}
			use, ok := reply.Content[0].ToolUse()
			if !ok {
				t.Fatal("actual provider did not retain the Bash tool call")
			}
			input, ok := use.Input.Bash()
			if !ok || input.Command != bashEffectText(t, row.CommandJSON) {
				t.Fatal("SDK ingress replaced the Source command text before execution")
			}
			root := t.TempDir()
			backend := &bashEffectSandbox{mode: row.Mode, selected: bashEffectArgv(t, row.SelectedJSON)}
			executor, err := shell.New(shell.Config{Workspace: root, Timeout: 3 * time.Second, Sandbox: backend})
			if err != nil {
				t.Fatal(err)
			}
			catalogue, err := agent.NewBashToolCatalog(executor)
			if err != nil {
				t.Fatal(err)
			}
			gate, err := agent.NewToolGate(catalogue, agent.DefaultPermissionPolicy(nil), agent.GateHooks{})
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := gate.Dispatch(context.Background(), agent.ToolAuthority{SessionID: "bash-effect", OwnerID: "operator", Workspace: executor.Workspace(), Mode: agent.ModeAuto}, agent.ToolCall{ID: use.ID, Input: use.Input})
			marker, argument := bashEffectFile(t, root, "effect.marker"), bashEffectFile(t, root, "argument.bytes")
			if !reflect.DeepEqual(marker, row.MarkerHex) || !reflect.DeepEqual(argument, row.ArgumentHex) {
				show := func(value *string) string {
					if value == nil {
						return "absent"
					}
					return *value
				}
				t.Errorf("SDK/gate actual side effects marker=%s argument=%s; Source marker=%s argument=%s", show(marker), show(argument), show(row.MarkerHex), show(row.ArgumentHex))
			}
			if err != nil || outcome.Denied || outcome.Replayed || backend.calls != row.SandboxCalls || backend.received != input.Command {
				t.Fatalf("gate skipped or moved the selected sandbox boundary: %+v %v", outcome, err)
			}
			if row.Exception != nil {
				if !outcome.Failed || outcome.Output != "Error: "+row.Exception.Message {
					t.Fatalf("actual failed tool outcome %+v; Source backend exception %+v", outcome, row.Exception)
				}
				if _, observed := outcome.CommandResult(); observed {
					t.Fatal("pre-child encoding exception published a structured process result")
				}
			} else {
				if row.Result == nil || outcome.Failed || outcome.Output != row.Result.Rendered {
					t.Fatalf("Source-successful surrogateescape/repair was changed: %+v", outcome)
				}
				metadata, present := outcome.CommandResult()
				if !present || !reflect.DeepEqual(metadata.ExitCode, row.Result.ExitCode) || metadata.TimedOut || metadata.Overflowed {
					t.Fatal("successful actual process metadata missing")
				}
			}
			if strings.Contains(outcome.Output, "argument.bytes") {
				t.Fatal("private marker path leaked in tool output")
			}
		})
	}
}
