package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/luoyjx/mini-loop/go/diagnostics"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type featureDefinitionID string

type sourceFeatureDefinition struct {
	ID           featureDefinitionID `json:"definition_id"`
	Name         protocol.ToolName   `json:"name"`
	Schema       protocol.ToolSchema `json:"schema"`
	Readonly     bool                `json:"readonly"`
	ParallelSafe bool                `json:"parallel_safe"`
	Risk         *ToolRisk           `json:"risk"`
	Capabilities []Capability        `json:"capabilities"`
	Modes        [2]ExecutionMode    `json:"modes"`
}

type sourceFeatureCase struct {
	Name  string                `json:"name"`
	Tools []featureDefinitionID `json:"tools"`
}

// This provider selects diagnostics inventory only. Calling it is a test error;
// these contracts make no claim about a native Python syntax parser.
type inventoryDiagnostics struct{}

func (inventoryDiagnostics) Diagnose(context.Context, diagnostics.Request) ([]diagnostics.Diagnostic, error) {
	return nil, errors.New("inventory-only diagnostics must not execute")
}
func (inventoryDiagnostics) Scope() string { return "inventory-only" }

func TestExplicitFeatureCatalogueMatchesSourceDefinitions(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-feature-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Definitions []sourceFeatureDefinition `json:"definitions"`
		Registries  []sourceFeatureCase       `json:"registries"`
		Managers    []sourceFeatureCase       `json:"managers"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	definitions := make(map[featureDefinitionID]sourceFeatureDefinition)
	for _, definition := range fixture.Definitions {
		definitions[definition.ID] = definition
	}
	var full sourceFeatureCase
	for _, row := range fixture.Registries {
		if row.Name == "full" {
			full = row
		}
	}
	if len(full.Tools) != 48 {
		t.Fatalf("Source full inventory: %d", len(full.Tools))
	}
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.DiagnosticsProvider = inventoryDiagnostics{}
	config.Services.SessionQueryTools = true
	config.Services.MCPTools = true
	config.Services.TeamTools = true
	config.Services.MemoryTools = true
	config.Services.SelfAuditTools = true
	config.Services.GoalTools = true
	config.Services.PlanModeTools = true
	config.Services.CronTools = true
	config.Services.BackgroundTools = true
	config.Services.WorktreeTools = true
	config.Services.TaskTools = true
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "fixture"})
	catalog := session.core.gate.catalog
	// Exercise partial bundles and independent additions against Source order.
	// Only names are needed here; this checks publication, not tool execution.
	for _, row := range append(fixture.Registries, fixture.Managers...) {
		t.Run("order/"+row.Name, func(t *testing.T) {
			var selected []ToolDefinition
			var expected []protocol.ToolName
			for _, id := range row.Tools {
				definition := definitions[id]
				selected = append(selected, ToolDefinition{name: definition.Name})
				expected = append(expected, definition.Name)
			}
			slices.Reverse(selected)
			orderBuiltinDefinitions(selected)
			var actual []protocol.ToolName
			for _, definition := range selected {
				actual = append(actual, definition.Name())
			}
			if !slices.Equal(actual, expected) {
				t.Fatalf("Go=%v Source=%v", actual, expected)
			}
		})
	}
	if len(catalog.Names()) != len(full.Tools) {
		t.Fatalf("Go inventory: %v", catalog.Names())
	}
	var sourceNames []protocol.ToolName
	for _, id := range full.Tools {
		sourceNames = append(sourceNames, definitions[id].Name)
	}
	if !slices.Equal(catalog.Names(), sourceNames) {
		t.Fatalf("order: Go=%v Source=%v", catalog.Names(), sourceNames)
	}
	for _, id := range full.Tools {
		expected, exists := definitions[id]
		if !exists {
			t.Fatalf("missing Source definition %s", id)
		}
		t.Run(string(expected.Name), func(t *testing.T) {
			actual, exists := catalog.Lookup(expected.Name)
			if !exists {
				t.Fatal("tool absent")
			}
			if expected.Risk == nil {
				t.Fatal("Source risk absent")
			}
			if actual.Risk() != *expected.Risk || actual.Readonly() != expected.Readonly || actual.ParallelSafe() != expected.ParallelSafe {
				t.Errorf("traits: Go=%s/%t/%t Source=%s/%t/%t", actual.Risk(), actual.Readonly(), actual.ParallelSafe(), *expected.Risk, expected.Readonly, expected.ParallelSafe)
			}
			caps := actual.Capabilities()
			slices.Sort(caps)
			if !slices.Equal(caps, expected.Capabilities) {
				t.Errorf("capabilities: %v != %v", caps, expected.Capabilities)
			}
			if !reflect.DeepEqual(actual.schema, expected.Schema) {
				t.Errorf("schema differs from Source")
			}
			for i, flag := range []bool{false, true} {
				call := ToolCall{ID: "inventory", Input: protocol.BashToolInput(protocol.BashInput{Command: "inventory", RunInBackground: &flag})}
				if mode := actual.ExecutionMode(call); mode != expected.Modes[i] {
					t.Errorf("mode %d: %s != %s", i, mode, expected.Modes[i])
				}
			}
		})
	}
	// A caller's explicit inventory keeps its own order and is not widened.
	read, _ := catalog.Lookup(protocol.ToolReadFile)
	bash, _ := catalog.Lookup(protocol.ToolBash)
	custom, err := NewToolCatalog(read, bash)
	if err != nil {
		t.Fatal(err)
	}
	customConfig := managerTestConfig(t.TempDir(), &FakeProvider{})
	customConfig.Services.Tools = custom
	customSession := createManaged(t, makeManager(t, customConfig), CreateSessionRequest{Owner: "fixture"})
	if !slices.Equal(customSession.core.gate.catalog.Names(), []protocol.ToolName{protocol.ToolReadFile, protocol.ToolBash}) {
		t.Fatalf("custom order/inventory changed: %v", customSession.core.gate.catalog.Names())
	}
}
