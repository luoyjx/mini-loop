package agent

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type efficiencyChildRow struct {
	Name               string          `json:"name"`
	Role               AgentRole       `json:"role"`
	Mode               efficiency.Mode `json:"mode"`
	Selected           *bool           `json:"selected"`
	RequireOptIn       bool            `json:"require_opt_in"`
	Persist            bool            `json:"persist"`
	Output             string          `json:"output"`
	Permission         PermissionMode  `json:"permission"`
	SharedRoot         bool            `json:"shared_root"`
	StorePresent       bool            `json:"store_present"`
	SharedStore        bool            `json:"shared_store"`
	RecoveryTool       bool            `json:"recovery_tool"`
	ConciseRequested   bool            `json:"concise_requested"`
	FreshLedger        bool            `json:"fresh_ledger"`
	Observations       int             `json:"observations"`
	Requests           int             `json:"requests"`
	Responses          []bool          `json:"responses"`
	Recovered          bool            `json:"recovered"`
	StoreOpen          bool            `json:"store_open"`
	ParentHistoryEmpty bool            `json:"parent_history_empty"`
}
type efficiencyChildObservation struct {
	descriptor efficiency.Descriptor
	calls      int
}

func (p *efficiencyChildObservation) Descriptor() efficiency.Descriptor { return p.descriptor }
func (p *efficiencyChildObservation) Reduce(context.Context, efficiency.MaskedObservation, efficiency.ReductionOptions) (efficiency.ObservationReduction, error) {
	p.calls++
	return efficiency.NewReduction(efficiency.ReductionConfig{Content: "short"})
}

type efficiencyChildRequest struct {
	descriptor efficiency.Descriptor
	calls      int
}

func (p *efficiencyChildRequest) Descriptor() efficiency.Descriptor { return p.descriptor }
func (p *efficiencyChildRequest) Optimize(_ context.Context, c efficiency.OptimizationContext, _ efficiency.RequestOptimizationOptions) (efficiency.RequestOptimizationResult, error) {
	p.calls++
	s, err := c.Snapshot()
	if err != nil {
		return efficiency.RequestOptimizationResult{}, err
	}
	return efficiency.NewRequestOptimization(efficiency.RequestOptimizationConfig{Request: s.Request})
}

type efficiencyChildResponse struct {
	policy *efficiency.ConciseResponsePlanner
	calls  []bool
}

func (p *efficiencyChildResponse) Descriptor() efficiency.Descriptor { return p.policy.Descriptor() }
func (p *efficiencyChildResponse) Plan(ctx context.Context, c efficiency.ResponseContext) (efficiency.ResponsePlan, error) {
	s, err := c.Snapshot()
	if err != nil {
		return efficiency.ResponsePlan{}, err
	}
	p.calls = append(p.calls, s.ConciseRequested)
	return p.policy.Plan(ctx, c)
}

type efficiencyChildPolicy bool

func (p efficiencyChildPolicy) Select(_ AgentRole, c *ToolCatalog) (*ToolCatalog, error) {
	var definitions []ToolDefinition
	for _, d := range c.ordered {
		if d.name == protocol.ToolReadFile || (bool(p) && d.name == protocol.ToolReadTokenArtifact) {
			definitions = append(definitions, d)
		}
	}
	return NewToolCatalog(definitions...)
}

func TestSubagentEfficiencyMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-subagents.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []efficiencyChildRow `json:"rows"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 8 {
		t.Fatal("source inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "proof"), []byte(strings.Repeat("masked authority ", 200)), 0600); err != nil {
				t.Fatal(err)
			}
			od, err := efficiency.NewDescriptor(efficiency.DefaultDescriptor("child-observation", "1", efficiency.Observation))
			if err != nil {
				t.Fatal(err)
			}
			rd, err := efficiency.NewDescriptor(efficiency.DefaultDescriptor("child-request", "1", efficiency.RequestContext))
			if err != nil {
				t.Fatal(err)
			}
			obs := &efficiencyChildObservation{descriptor: od}
			req := &efficiencyChildRequest{descriptor: rd}
			pc := efficiency.DefaultConciseResponseConfig()
			pc.RequireOptIn = row.RequireOptIn
			policy, err := efficiency.NewConciseResponsePlanner(pc)
			if err != nil {
				t.Fatal(err)
			}
			resp := &efficiencyChildResponse{policy: &policy, calls: []bool{}}
			var registry efficiency.Registry
			if _, err = registry.RegisterObservation(obs, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = registry.RegisterRequest(req, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = registry.RegisterResponse(resp, nil); err != nil {
				t.Fatal(err)
			}
			rc := efficiency.DefaultRuntimeConfig()
			rc.DefaultMode = row.Mode
			var store *efficiency.RawStore
			var seed string
			if row.Mode == efficiency.Enforce && row.Persist {
				store, err = efficiency.NewRawStore(efficiency.DefaultRawStoreConfig(root))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				rc.RawStore = store
				pointer, err := store.PutMasked("seed masked authority")
				if err != nil {
					t.Fatal(err)
				}
				p, err := pointer.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				seed = p.Ref
			}
			runtime, err := registry.Runtime(rc)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close(context.Background(), efficiency.LifecycleCloseOptions{})
			var child *Session
			rounds := 0
			recovered := false
			provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
				rounds++
				for _, m := range r.Messages {
					blocks, _ := m.Content.Blocks()
					for _, b := range blocks {
						if result, ok := b.ToolResult(); ok && strings.Contains(result.Content, "seed masked authority") {
							recovered = true
						}
					}
				}
				if rounds == 1 {
					return fakeReply([]protocol.Block{protocol.NewToolUse("read", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "proof"}))}, protocol.StopToolUse), nil
				}
				if rounds == 2 && seed != "" {
					if _, ok := child.gate.catalog.Lookup(protocol.ToolReadTokenArtifact); ok {
						return fakeReply([]protocol.Block{protocol.NewToolUse("recover", protocol.ReadTokenArtifactToolInput(protocol.ReadTokenArtifactInput{RawRef: seed}))}, protocol.StopToolUse), nil
					}
				}
				return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
			})
			cfg := runtimeConfig(root, provider)
			cfg.Mode = ModeAuto
			cfg.TokenEfficiency = runtime
			cfg.ConciseResponse = true
			cfg.TokenArtifactTools = store != nil
			cfg.TokenArtifactStore = store
			cfg.SubagentMaxRounds = 4
			cfg.ObservationRaw = &ObservationRawConfig{Persist: row.Persist, MinBytes: big.NewInt(1)}
			if row.Selected != nil {
				cfg.RoleToolPolicy = efficiencyChildPolicy(*row.Selected)
			}
			parent, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			run, err := DefaultRunContext()
			if err != nil {
				t.Fatal(err)
			}
			pinned := parent.subagentParent(run)
			var childRun RunContext
			child, childRun, err = newSubagent(context.Background(), SubagentRequest{Parent: pinned, Prompt: "inspect", Role: row.Role, RunContext: run})
			if err != nil {
				t.Fatal(err)
			}
			got := row
			got.SharedRoot = child.tokenEfficiency == parent.tokenEfficiency
			got.ConciseRequested = child.conciseResponse
			got.FreshLedger = child.requestProjections == nil
			got.Permission = child.permissionMode()
			raw := child.tokenEfficiency.RawStore()
			got.StorePresent = raw != nil
			got.SharedStore = store != nil && raw == store
			_, got.RecoveryTool = child.gate.catalog.Lookup(protocol.ToolReadTokenArtifact)
			// The caller's pinned integer and the child snapshot must be independent.
			pinned.observationRaw.MinBytes.SetInt64(99)
			if child.observationRaw.MinBytes.Int64() != 1 || parent.observationRaw.MinBytes.Int64() != 1 {
				t.Fatal("raw settings aliased")
			}
			got.Output, err = child.RunWithContext(context.Background(), "inspect", childRun)
			if err != nil {
				t.Fatal(err)
			}
			got.Observations, got.Requests, got.Responses, got.Recovered = obs.calls, req.calls, resp.calls, recovered
			got.StoreOpen = store != nil && !store.Closed()
			got.ParentHistoryEmpty = len(parent.messages) == 0
			if !reflect.DeepEqual(got, row) {
				t.Fatalf("got %+v\nwant %+v", got, row)
			}
		})
	}
}

func TestManagedTaskChildDrainsBeforeEfficiencyRevocation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "proof"), []byte(strings.Repeat("masked authority ", 200)), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := efficiency.NewDescriptor(efficiency.DefaultDescriptor("child-drain", "1", efficiency.Observation))
	if err != nil {
		t.Fatal(err)
	}
	probe := &managerEfficiencyProbe{descriptor: d, entered: make(chan struct{})}
	var registry efficiency.Registry
	if _, err := registry.RegisterObservation(probe, nil); err != nil {
		t.Fatal(err)
	}
	rc := efficiency.DefaultRuntimeConfig()
	rc.DefaultMode = efficiency.Enforce
	runtime, err := registry.Runtime(rc)
	if err != nil {
		t.Fatal(err)
	}
	role := protocol.AgentType("worker")
	provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		if r.System != nil && strings.Contains(*r.System, " subagent in ") {
			return fakeReply([]protocol.Block{protocol.NewToolUse("read", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "proof"}))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "inspect", AgentType: &role}))}, protocol.StopToolUse), nil
	})
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.Services.TokenEfficiency = runtime
	cfg.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { return root, nil })
	selection := DefaultManagerEfficiencyConfig()
	selection.RawMinBytes = big.NewInt(1)
	cfg.Efficiency = &selection
	manager := makeManager(t, cfg)
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	store := parent.core.tokenEfficiency.RawStore()
	if store == nil {
		t.Fatal("parent scope missing")
	}
	probe.stores = append(probe.stores, store)
	done := make(chan error, 1)
	go func() { _, err := parent.Run(context.Background(), "delegate"); done <- err }()
	select {
	case <-probe.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("child observation never entered")
	}
	if store.Closed() {
		t.Fatal("store revoked before drain")
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("task consumer not joined")
	}
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if !probe.cancelled || !probe.allClosed || probe.close != 1 || !store.Closed() {
		t.Fatal("child drain/store/root close ordering")
	}
}
