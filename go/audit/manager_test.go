package audit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sandbox"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/sqlitestate"
)

type auditNeverProvider struct{ t *testing.T }

func (p auditNeverProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	p.t.Fatal("audit called provider")
	return protocol.ModelReply{}, nil
}

type auditNeverTool struct{ t *testing.T }

func (p auditNeverTool) ExecuteTool(context.Context, agent.ToolAuthority, protocol.ToolInput) (string, error) {
	p.t.Fatal("audit executed a tool")
	return "", nil
}

func TestManagerAuditMatchesActualSourceProfilesWithoutCreatingSessions(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-audit-live.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name        string
			Environment map[string]string
			Observation LiveObservation
			Findings    []Finding
			Rendered    string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	matched := 0
	for _, row := range fixture.Cases {
		// Runtime log collection and persistence faults have direct agent tests below.
		if row.Name == "problem-ledgers" || row.Name == "plain-problems" || row.Name == "broken-store" {
			continue
		}
		matched++
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			cfg := agent.ManagerConfig{WorkspaceRoot: root, Services: agent.ManagerServices{Provider: auditNeverProvider{t}}}
			if row.Observation.Catalog != nil {
				definitions := []agent.ToolDefinition{}
				for _, tool := range row.Observation.Catalog.Tools {
					definition, err := agent.NewToolDefinitionWithSchema(protocol.ToolSchema{Name: tool.Name, Description: "fixture", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}, agent.ToolTraits{Risk: tool.Risk, ParallelSafe: tool.ParallelSafe}, auditNeverTool{t})
					if err != nil {
						t.Fatal(err)
					}
					definitions = append(definitions, definition)
				}
				cfg.Services.Tools, err = agent.NewToolCatalog(definitions...)
				if err != nil {
					t.Fatal(err)
				}
			}
			switch row.Name {
			case "exposed":
				cfg.Services.Secrets = secrets.Null{}
			case "masked", "hardened":
				env := make(secrets.Environment, len(row.Environment))
				for name, value := range row.Environment {
					env[secrets.Name(name)] = value
				}
				cfg.Services.Secrets = secrets.FromEnvironment(secrets.Config{}, env, nil, nil)
			case "partial", "case-sensitive", "cached-secret-faults":
				registry := secrets.New(secrets.Config{})
				switch row.Name {
				case "partial":
					registry.RegisterValue("B_SECRET", "fixture-long-b")
				case "case-sensitive":
					registry.RegisterValue("A_API_KEY", "fixture-long-a")
				default:
					registry.RegisterLookup("FAILED_TOKEN", func() (string, error) { return "", errors.New("fixture lookup failed") })
					registry.RegisterValue("SHORT_TOKEN", "tiny")
					registry.MaskText("fixture")
				}
				cfg.Services.Secrets = registry
			}
			if row.Observation.SandboxReason != nil {
				cfg.Services.Sandbox = sandbox.NewUnavailable(*row.Observation.SandboxReason)
			}
			if row.Name == "hardened" {
				cfg.Services.Sandbox, err = sandbox.NewSeatbelt(sandbox.Config{WritableRoots: []string{root}})
				if err != nil {
					t.Fatal(err)
				}
				store, err := sqlitestate.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
				cfg.Services.StateStore = store
			}
			if row.Name == "workflows" {
				cfg.Services.WorkflowTools = true
			}
			manager, err := agent.NewSessionManager(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := manager.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			findings, err := Manager(context.Background(), manager, config.Settings{WorkspaceRoot: root, FakeLLM: true}, row.Environment)
			if err != nil || !reflect.DeepEqual(findings, row.Findings) {
				t.Fatalf("native %#v; Source %#v; error %v", findings, row.Findings, err)
			}
			text, err := Render(findings)
			if err != nil || text != row.Rendered {
				t.Fatal(text, row.Rendered, err)
			}
			if manager.Summary().Sessions != 0 {
				t.Fatal("audit created sessions")
			}
		})
	}
	if matched != 13 {
		t.Fatal("unexpected Source manager coverage", matched)
	}
}

func TestManagerAuditRefusesMissingManagerAndCancelledRequests(t *testing.T) {
	if findings, err := Manager(context.Background(), nil, config.Settings{}, map[string]string{}); err == nil || findings != nil {
		t.Fatal(findings, err)
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: auditNeverProvider{t}}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if findings, err := Manager(ctx, manager, config.Settings{}, map[string]string{}); !errors.Is(err, context.Canceled) || findings != nil {
		t.Fatal(findings, err)
	}
}
