package benchmark

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestRunArmPostConstructionFilteringMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-benchmark-efficiency-filter.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name       string
			Mode       efficiency.Mode
			Selection  *[]protocol.ToolName
			ModelNames []protocol.ToolName `json:"model_names"`
			Passed     bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 7 {
		t.Fatal("Source filter inventory drift")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			prepared := false
			calls := 0
			provider := agent.NewFakeProvider(agent.FakeProviderConfig{Responder: runnerResponder(func(_ context.Context, request protocol.ModelRequest) (agent.FakeGeneration, error) {
				calls++
				if !prepared {
					t.Error("model called before setup")
				}
				names := []protocol.ToolName{}
				for _, schema := range request.Tools {
					names = append(names, schema.Name)
				}
				if !slices.Equal(names, row.ModelNames) {
					t.Errorf("model inventory: %v want %v", names, row.ModelNames)
				}
				return agent.FakeGeneration{Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn}, nil
			})}).ObjectView()
			cfg := armManagerConfig(t.TempDir(), provider)
			selection := agent.DefaultManagerEfficiencyConfig()
			selection.Mode = row.Mode
			cfg.Efficiency = &selection
			task, err := NewTask(TaskConfig{Name: row.Name, Prompt: "finish", Expect: textJudge("done"), ToolNames: row.Selection, Setup: func(context.Context, string) error { prepared = true; return nil }})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := RunArm(context.Background(), "test", cfg, []Task{task})
			if err != nil || len(rows) != 1 || rows[0].Passed != row.Passed || calls != 1 {
				t.Fatal("arm result drift", rows, err, calls)
			}
		})
	}
}
