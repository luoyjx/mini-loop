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
	HandlerBound bool                `json:"handler_bound"`
}

type sourceFeatureGroups struct {
	Tasks        bool `json:"tasks"`
	Background   bool `json:"background"`
	Memory       bool `json:"memory"`
	Cron         bool `json:"cron"`
	Plan         bool `json:"plan"`
	Goals        bool `json:"goals"`
	Diagnostics  bool `json:"diagnostics"`
	SessionQuery bool `json:"session_query"`
	Teams        bool `json:"teams"`
	Worktrees    bool `json:"worktrees"`
	MCP          bool `json:"mcp"`
	SelfAudit    bool `json:"self_audit"`
}

type sourceFeatureSelection struct {
	Kind            string              `json:"kind"`
	Groups          sourceFeatureGroups `json:"groups"`
	EnableFeatures  bool                `json:"enable_features"`
	CustomTools     bool                `json:"custom_tools"`
	CustomInjectors bool                `json:"custom_injectors"`
	Workflows       bool                `json:"workflows"`
	AST             bool                `json:"ast"`
	Decisions       string              `json:"decisions"`
}

type sourceFeatureBindings struct {
	Manager bool `json:"manager"`
	Session bool `json:"session"`
}

type sourceFeatureCase struct {
	Name      string                 `json:"name"`
	Tools     []featureDefinitionID  `json:"tools"`
	Selection sourceFeatureSelection `json:"selection"`
	Bindings  sourceFeatureBindings  `json:"bindings"`
	Owner     OwnerID                `json:"owner"`
	Mode      PermissionMode         `json:"mode"`
	Workflows bool                   `json:"workflows"`
}

// This provider selects diagnostics inventory only. Calling it is a test error;
// these contracts make no claim about a native Python syntax parser.
type inventoryDiagnostics struct{}

func (inventoryDiagnostics) Diagnose(context.Context, diagnostics.Request) ([]diagnostics.Diagnostic, error) {
	return nil, errors.New("inventory-only diagnostics must not execute")
}
func (inventoryDiagnostics) Scope() string { return "inventory-only" }

// The Source recipe supplies independent group switches, never native tool
// definitions. Manager construction reaches the production registration paths.
func selectSourceFeatureGroups(config *ManagerConfig, groups sourceFeatureGroups) {
	services := &config.Services
	services.TaskTools = groups.Tasks
	services.BackgroundTools = groups.Background
	services.MemoryTools = groups.Memory
	services.CronTools = groups.Cron
	services.PlanModeTools = groups.Plan
	services.GoalTools = groups.Goals
	if groups.Diagnostics {
		services.DiagnosticsProvider = inventoryDiagnostics{}
	}
	services.SessionQueryTools = groups.SessionQuery
	services.TeamTools = groups.Teams
	services.WorktreeTools = groups.Worktrees
	services.MCPTools = groups.MCP
	services.SelfAuditTools = groups.SelfAudit
}

func compareSourceFeatureCatalogue(t *testing.T, catalog *ToolCatalog, row sourceFeatureCase, definitions map[featureDefinitionID]sourceFeatureDefinition, observed map[featureDefinitionID]bool) {
	t.Helper()
	var expectedNames []protocol.ToolName
	for _, id := range row.Tools {
		expected, exists := definitions[id]
		if !exists {
			t.Fatalf("missing Source definition %s", id)
		}
		expectedNames = append(expectedNames, expected.Name)
	}
	if !slices.Equal(catalog.Names(), expectedNames) {
		t.Fatalf("actual built-in order: Go=%v Source=%v", catalog.Names(), expectedNames)
	}
	for _, id := range row.Tools {
		expected := definitions[id]
		t.Run(string(id), func(t *testing.T) {
			actual, exists := catalog.Lookup(expected.Name)
			if !exists {
				t.Fatal("actual built-in tool absent")
			}
			if !expected.HandlerBound || actual.handler == nil {
				t.Fatal("Source/native executable handler is unbound")
			}
			if expected.Risk == nil {
				t.Fatal("Source built-in risk absent")
			}
			if actual.Risk() != *expected.Risk || actual.Readonly() != expected.Readonly || actual.ParallelSafe() != expected.ParallelSafe {
				t.Errorf("traits: Go=%s/%t/%t Source=%s/%t/%t", actual.Risk(), actual.Readonly(), actual.ParallelSafe(), *expected.Risk, expected.Readonly, expected.ParallelSafe)
			}
			caps := actual.Capabilities()
			slices.Sort(caps)
			if !slices.Equal(caps, expected.Capabilities) {
				t.Errorf("capabilities: Go=%v Source=%v", caps, expected.Capabilities)
			}
			// ToolSchema is the closed protocol representation of the complete
			// Source schema; compare every field, including input metadata.
			if !reflect.DeepEqual(actual.schema, expected.Schema) {
				t.Errorf("complete schema differs: Go=%#v Source=%#v", actual.schema, expected.Schema)
			}
			for i, flag := range []bool{false, true} {
				call := ToolCall{ID: "inventory", Input: protocol.BashToolInput(protocol.BashInput{Command: "inventory", RunInBackground: &flag})}
				if mode := actual.ExecutionMode(call); mode != expected.Modes[i] {
					t.Errorf("mode %d: Go=%s Source=%s", i, mode, expected.Modes[i])
				}
			}
			observed[id] = true
		})
	}
}

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
	if len(fixture.Definitions) != 57 || len(fixture.Registries) != 14 || len(fixture.Managers) != 8 {
		t.Fatal("Source feature inventory drift")
	}
	definitions := make(map[featureDefinitionID]sourceFeatureDefinition)
	for _, definition := range fixture.Definitions {
		if _, exists := definitions[definition.ID]; exists || definition.ID == "" {
			t.Fatalf("duplicate or empty Source definition %s", definition.ID)
		}
		definitions[definition.ID] = definition
	}
	observed := make(map[featureDefinitionID]bool)
	for _, row := range fixture.Registries {
		t.Run("registry/"+row.Name, func(t *testing.T) {
			if row.Selection.Kind != "default" && row.Selection.Kind != "full" {
				t.Fatalf("missing Source registry selection: %q", row.Selection.Kind)
			}
			config := managerTestConfig(t.TempDir(), &FakeProvider{})
			// Partial explicit services must not pass through EnableFeatures,
			// whose documented meaning selects every group again.
			selectSourceFeatureGroups(&config, row.Selection.Groups)
			manager := makeManager(t, config)
			session := createManaged(t, manager, CreateSessionRequest{Owner: "fixture"})
			core, groups := session.core, row.Selection.Groups
			if core.runtime == nil || core.runtime.session != core || core.runtime.workflowManager != manager {
				t.Fatal("native session/manager binding absent")
			}
			if (core.background != nil) != groups.Background || (core.mcp != nil) != groups.MCP ||
				(core.runtime.diagnostics != nil) != groups.Diagnostics ||
				(core.runtime.transcriptSearch != nil) != groups.SessionQuery ||
				(core.teamManager != nil) != groups.Teams {
				t.Fatal("native selected service bindings differ")
			}
			compareSourceFeatureCatalogue(t, core.gate.catalog, row, definitions, observed)
		})
	}
	for _, row := range fixture.Managers {
		t.Run("manager/"+row.Name, func(t *testing.T) {
			selection := row.Selection
			if selection.Decisions != "off" && selection.Decisions != "llm" {
				t.Fatalf("missing Source decision selection: %q", selection.Decisions)
			}
			config := managerTestConfig(t.TempDir(), &FakeProvider{})
			config.Defaults.PermissionMode = row.Mode
			config.EnableFeatures = selection.EnableFeatures
			if config.EnableFeatures && !selection.CustomTools {
				config.Services.DiagnosticsProvider = inventoryDiagnostics{}
			}
			if selection.CustomTools {
				base := createManaged(t, makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{})), CreateSessionRequest{Owner: "fixture"})
				config.Services.Tools = base.core.gate.catalog
			}
			if selection.CustomInjectors {
				config.Services.Injectors = []MessageInjector{}
			}
			config.Services.WorkflowTools = selection.Workflows
			config.Services.DecisionTools = selection.Decisions == "llm"
			if selection.AST {
				ast, _ := nativeASTConfig(t)
				config.Services.ASTConfig = &ast
			}
			manager := makeManager(t, config)
			session := createManaged(t, manager, CreateSessionRequest{Owner: row.Owner})
			core := session.core
			bindings := sourceFeatureBindings{Manager: core.runtime != nil && core.runtime.workflowManager == manager, Session: core.runtime != nil && core.runtime.session == core}
			if bindings != row.Bindings || !bindings.Manager || !bindings.Session {
				t.Fatalf("bindings: Go=%+v Source=%+v", bindings, row.Bindings)
			}
			if core.owner != row.Owner || core.mode != row.Mode {
				t.Fatal("Source owner/mode binding differs")
			}
			if (core.runtime.ast != nil) != selection.AST || (manager.Workflows() != nil) != row.Workflows || core.runtime.workflows != manager.Workflows() {
				t.Fatal("independent AST/workflow service activation differs")
			}
			if selection.EnableFeatures && (core.background == nil || core.teamManager != manager) {
				t.Fatal("full feature lifecycle binding differs")
			}
			if (core.injectors != nil) != selection.CustomInjectors {
				t.Fatal("explicit injector override lost")
			}
			compareSourceFeatureCatalogue(t, core.gate.catalog, row, definitions, observed)
		})
	}
	for _, definition := range fixture.Definitions {
		if !observed[definition.ID] {
			t.Errorf("Source definition never compared with a real native catalogue: %s", definition.ID)
		}
	}
}

func TestFeatureCatalogueRetainsExplicitCustomOrder(t *testing.T) {
	base := createManaged(t, makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{})), CreateSessionRequest{Owner: "fixture"})
	read, _ := base.core.gate.catalog.Lookup(protocol.ToolReadFile)
	bash, _ := base.core.gate.catalog.Lookup(protocol.ToolBash)
	custom, err := NewToolCatalog(read, bash)
	if err != nil {
		t.Fatal(err)
	}
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.Tools = custom
	session := createManaged(t, makeManager(t, config), CreateSessionRequest{Owner: "fixture"})
	if !slices.Equal(session.core.gate.catalog.Names(), []protocol.ToolName{protocol.ToolReadFile, protocol.ToolBash}) {
		t.Fatalf("custom order/inventory changed: %v", session.core.gate.catalog.Names())
	}
}
