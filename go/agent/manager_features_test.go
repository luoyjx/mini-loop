package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestManagerFullFeaturesSelectsSourceCataloguesAndOverrides(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-feature-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Definitions []sourceFeatureDefinition `json:"definitions"`
		Managers    []sourceFeatureCase       `json:"managers"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	definitions := make(map[featureDefinitionID]sourceFeatureDefinition)
	for _, definition := range fixture.Definitions {
		definitions[definition.ID] = definition
	}
	for _, row := range fixture.Managers {
		t.Run(row.Name, func(t *testing.T) {
			config := managerTestConfig(t.TempDir(), &FakeProvider{})
			config.EnableFeatures = row.Name != "off"
			if config.EnableFeatures && row.Name != "full-custom-tools" {
				config.Services.DiagnosticsProvider = inventoryDiagnostics{}
			}
			if row.Name == "full-custom-tools" {
				// A caller supplies this explicit ten-tool catalogue. This test
				// exercises publication, not effects through its retained handlers.
				base := createManaged(t, makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{})), CreateSessionRequest{Owner: "fixture"})
				config.Services.Tools = base.core.gate.catalog
			}
			if row.Name == "full-custom-injectors" {
				config.Services.Injectors = []MessageInjector{}
			}
			config.Services.WorkflowTools = row.Name == "full-workflows" || row.Name == "full-independent"
			config.Services.DecisionTools = row.Name == "full-decisions" || row.Name == "full-independent"
			if row.Name == "full-ast" || row.Name == "full-independent" {
				ast, _ := nativeASTConfig(t)
				config.Services.ASTConfig = &ast
			}
			manager := makeManager(t, config)
			session := createManaged(t, manager, CreateSessionRequest{Owner: "fixture"})
			var expected []protocol.ToolName
			for _, id := range row.Tools {
				expected = append(expected, definitions[id].Name)
			}
			if !slices.Equal(session.core.gate.catalog.Names(), expected) {
				t.Fatalf("Go=%v Source=%v", session.core.gate.catalog.Names(), expected)
			}
			for _, id := range row.Tools {
				want := definitions[id]
				actual, _ := session.core.gate.catalog.Lookup(want.Name)
				if want.Name == protocol.ToolBash && actual.ExecutionMode(ToolCall{Input: protocol.BashToolInput(protocol.BashInput{Command: "inventory", RunInBackground: boolPointer(true)})}) != want.Modes[1] {
					t.Fatal("custom/full bash classifier changed")
				}
			}
			if config.EnableFeatures && (session.core.background == nil || session.core.teamManager != manager) {
				t.Fatal("full lifecycle omitted for custom catalogue")
			}
			if (session.core.injectors != nil) != (row.Name == "full-custom-injectors") {
				t.Fatal("explicit injector override lost")
			}
			if row.Name == "full" && (manager.config.Services.WorkflowTools || manager.config.Services.DecisionTools || manager.config.Services.ASTConfig != nil) {
				t.Fatal("independent additions enabled implicitly")
			}
		})
	}
}

func TestManagerFullCustomCatalogueOverrideDoesNotRequireNativeBackgroundExecutor(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.EnableFeatures = true
	config.Services.Injectors = []MessageInjector{}
	config.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) {
		return echoExecutor{}, nil
	})
	catalog, err := NewBashToolCatalog(echoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	config.Services.Tools = catalog
	session := createManaged(t, makeManager(t, config), CreateSessionRequest{Owner: "fixture"})
	if session.core.background != nil || !slices.Equal(session.core.gate.catalog.Names(), []protocol.ToolName{protocol.ToolBash}) {
		t.Fatal("custom catalogue/injector override installed background effects")
	}
}

func TestManagerFullFeaturesMissingParserFailsBeforeWorkspaceEffects(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	config := managerTestConfig(root, &FakeProvider{})
	config.EnableFeatures = true
	manager, err := NewSessionManager(config)
	if manager != nil || err == nil || !strings.Contains(err.Error(), "explicit diagnostics provider") {
		t.Fatal(manager, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("parser rejection created workspace", err)
	}
	if config.Services.TaskTools || config.Services.TeamTools || config.Services.BackgroundTools {
		t.Fatal("constructor mutated caller settings")
	}
}
