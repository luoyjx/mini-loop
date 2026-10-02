package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type schedulingContract struct {
	CompletionOrder  []string         `json:"completion_order"`
	DefaultToolLimit ConcurrencyLimit `json:"default_tool_limit"`
	Modes            []struct {
		Name   string
		Static bool
		Mode   ExecutionMode
	}
	Prompt      string
	PromptSeen  []string `json:"prompt_seen"`
	Results     []protocol.Block
	StepOutputs []StepHash `json:"step_outputs"`
	TodoTurns   []struct {
		Counter   int
		Reminders []string
	} `json:"todo_turns"`
}

func readSchedulingContract(t *testing.T) schedulingContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-scheduling.json")
	if err != nil {
		t.Fatal(err)
	}
	var result schedulingContract
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Modes) != 6 || len(result.Results) != 5 || len(result.TodoTurns) != 4 || result.DefaultToolLimit != DefaultToolConcurrency {
		t.Fatal("scheduling inventory drift")
	}
	return result
}
func TestExecutionModesMatchPython(t *testing.T) {
	for _, entry := range readSchedulingContract(t).Modes {
		t.Run(entry.Name, func(t *testing.T) {
			definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskRead, ParallelSafe: entry.Static}, echoToolHandler{})
			if err != nil {
				t.Fatal(err)
			}
			switch entry.Name {
			case "override-exclusive":
				definition = definition.WithExecutionClassifier(executionClassifierFunc(func(ToolCall) (ExecutionMode, error) { return ExecutionExclusive, nil }))
			case "override-parallel":
				definition = definition.WithExecutionClassifier(executionClassifierFunc(func(ToolCall) (ExecutionMode, error) { return ExecutionParallel, nil }))
			case "invalid":
				definition = definition.WithExecutionClassifier(executionClassifierFunc(func(ToolCall) (ExecutionMode, error) { return "invalid", nil }))
			case "error":
				definition = definition.WithExecutionClassifier(executionClassifierFunc(func(ToolCall) (ExecutionMode, error) { return "", errors.New("failed") }))
			}
			if got := definition.ExecutionMode(gateTestCall("x")); got != entry.Mode {
				t.Fatalf("%s != %s", got, entry.Mode)
			}
		})
	}
}

type echoToolHandler struct{}

func (echoToolHandler) ExecuteTool(_ context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
	v, _ := input.Bash()
	return v.Command, nil
}

func compareSchedulingResults(t *testing.T, s *Session, log []string) {
	t.Helper()
	contract := readSchedulingContract(t)
	if !reflect.DeepEqual(log, contract.CompletionOrder) {
		t.Fatalf("completion order differs from Python: %v", log)
	}
	var results []protocol.Block
	for _, message := range s.Messages() {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if _, ok := block.ToolResult(); ok {
				results = append(results, block)
			}
		}
	}
	if !reflect.DeepEqual(results, contract.Results) {
		t.Fatalf("ordered results differ from Python: %+v", results)
	}
	for i, step := range s.recentSteps {
		if step.OutputHash != contract.StepOutputs[i] {
			t.Fatal("ordered stuck steps differ from Python")
		}
	}
}
