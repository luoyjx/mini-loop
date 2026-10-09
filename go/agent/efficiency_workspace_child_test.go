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

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type workspaceChildEfficiencyRow struct {
	Name                  string          `json:"name"`
	Role                  AgentRole       `json:"role"`
	Mode                  efficiency.Mode `json:"mode"`
	Selected              *bool           `json:"selected"`
	Persist               bool            `json:"persist"`
	Output                string          `json:"output"`
	Permission            PermissionMode  `json:"permission"`
	EnteredWorkspace      bool            `json:"entered_workspace"`
	FreshStore            bool            `json:"fresh_store"`
	StoreWorkspaceMatches bool            `json:"store_workspace_matches"`
	SharedRoot            bool            `json:"shared_root"`
	RecoveryTool          bool            `json:"recovery_tool"`
	Limits                []float64       `json:"limits"`
	Observations          []string        `json:"observations"`
	ForeignRecovered      bool            `json:"foreign_recovered"`
	OwnRecovered          bool            `json:"own_recovered"`
	ParentSeedRetained    bool            `json:"parent_seed_retained"`
	ParentStoreOpen       bool            `json:"parent_store_open"`
	ChildStoreOpen        bool            `json:"child_store_open"`
}
type workspaceChildReducer struct {
	descriptor   efficiency.Descriptor
	observations []string
}

func (p *workspaceChildReducer) Descriptor() efficiency.Descriptor { return p.descriptor }
func (p *workspaceChildReducer) Reduce(_ context.Context, o efficiency.MaskedObservation, _ efficiency.ReductionOptions) (efficiency.ObservationReduction, error) {
	s, err := o.Snapshot()
	if err != nil {
		return efficiency.ObservationReduction{}, err
	}
	p.observations = append(p.observations, s.Content)
	return efficiency.NewReduction(efficiency.ReductionConfig{Content: "short"})
}

func TestEnteredWorkspaceChildEfficiencyMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-workspace-child.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []workspaceChildEfficiencyRow `json:"rows"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 7 {
		t.Fatal("source inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			home, entered := t.TempDir(), t.TempDir()
			for _, entry := range []struct{ root, content string }{{home, "home workspace authority"}, {entered, "new workspace authority"}} {
				if err := os.WriteFile(filepath.Join(entry.root, "proof"), []byte(entry.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			rawConfig := efficiency.DefaultRawStoreConfig(home)
			rawConfig.TTLSeconds = 99
			rawConfig.MaxArtifactBytes = big.NewInt(100)
			rawConfig.MaxTotalBytes = big.NewInt(400)
			old, err := efficiency.NewRawStore(rawConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer old.Close()
			pointer, err := old.PutMasked("home-only masked seed")
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := pointer.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			parentRef := metadata.Ref
			d, err := efficiency.NewDescriptor(efficiency.DefaultDescriptor("workspace-child", "1", efficiency.Observation))
			if err != nil {
				t.Fatal(err)
			}
			probe := &workspaceChildReducer{descriptor: d, observations: []string{}}
			var registry efficiency.Registry
			if _, err = registry.RegisterObservation(probe, nil); err != nil {
				t.Fatal(err)
			}
			rc := efficiency.DefaultRuntimeConfig()
			rc.DefaultMode = row.Mode
			rc.RawStore = old
			runtime, err := registry.Runtime(rc)
			if err != nil {
				t.Fatal(err)
			}
			var child *Session
			rounds := 0
			childRef := ""
			foreign, own := false, false
			provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
				rounds++
				for _, m := range r.Messages {
					blocks, _ := m.Content.Blocks()
					for _, b := range blocks {
						if result, ok := b.ToolResult(); ok {
							foreign = foreign || strings.Contains(result.Content, "home-only masked seed")
							own = own || strings.Contains(result.Content, "child-only masked seed")
						}
					}
				}
				if rounds == 1 {
					return fakeReply([]protocol.Block{protocol.NewToolUse("read", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "proof"}))}, protocol.StopToolUse), nil
				}
				if rounds == 2 {
					if _, ok := child.gate.catalog.Lookup(protocol.ToolReadTokenArtifact); ok {
						calls := []protocol.Block{protocol.NewToolUse("foreign", protocol.ReadTokenArtifactToolInput(protocol.ReadTokenArtifactInput{RawRef: parentRef}))}
						if childRef != "" {
							calls = append(calls, protocol.NewToolUse("own", protocol.ReadTokenArtifactToolInput(protocol.ReadTokenArtifactInput{RawRef: childRef})))
						}
						return fakeReply(calls, protocol.StopToolUse), nil
					}
				}
				return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
			})
			cfg := managerTestConfig(t.TempDir(), provider)
			cfg.Services.TokenEfficiency = runtime
			cfg.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { return home, nil })
			settings := DefaultManagerEfficiencyConfig()
			settings.Mode = row.Mode
			settings.PersistRaw = row.Persist
			settings.RawMinBytes = big.NewInt(1)
			settings.ArtifactTTLSeconds = 7
			settings.MaxArtifactBytes = big.NewInt(200)
			settings.MaxTotalBytes = big.NewInt(800)
			cfg.Efficiency = &settings
			cfg.Defaults.SubagentMaxRounds = 4
			if row.Selected != nil {
				cfg.Services.RoleToolPolicy = efficiencyChildPolicy(*row.Selected)
			}
			manager := makeManager(t, cfg)
			parent := createManaged(t, manager, CreateSessionRequest{Owner: "alice", PermissionMode: ModeAuto})
			settings.MaxArtifactBytes.SetInt64(1)
			if parent.core.observationRaw.StoreLimits.MaxArtifactBytes.Int64() != 200 {
				t.Fatal("manager settings aliased")
			}
			if err := parent.core.runtime.enterWorkspace(context.Background(), entered); err != nil {
				t.Fatal(err)
			}
			run, err := DefaultRunContext()
			if err != nil {
				t.Fatal(err)
			}
			pinned := parent.core.subagentParent(run)
			var childRun RunContext
			child, childRun, err = newSubagent(context.Background(), SubagentRequest{Parent: pinned, Role: row.Role, Prompt: "inspect", RunContext: run})
			if err != nil {
				t.Fatal(err)
			}
			pinned.observationRaw.StoreLimits.MaxArtifactBytes.SetInt64(2)
			if child.observationRaw.StoreLimits.MaxArtifactBytes.Int64() != 200 || parent.core.observationRaw.StoreLimits.MaxArtifactBytes.Int64() != 200 {
				t.Fatal("child settings aliased")
			}
			raw := child.tokenEfficiency.RawStore()
			if raw != nil {
				defer raw.Close()
				p, err := raw.PutMasked("child-only masked seed")
				if err != nil {
					t.Fatal(err)
				}
				m, err := p.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				childRef = m.Ref
			}
			got := row
			got.Permission = child.permissionMode()
			got.EnteredWorkspace = child.executionRoot() == parent.core.executionRoot()
			got.FreshStore = raw != nil && raw != old
			got.StoreWorkspaceMatches = raw != nil && raw.Workspace() == child.executionRoot()
			_, got.RecoveryTool = child.gate.catalog.Lookup(protocol.ToolReadTokenArtifact)
			got.Limits = nil
			if raw != nil {
				limits := raw.Limits()
				got.Limits = []float64{limits.TTLSeconds, float64(limits.MaxArtifactBytes.Int64()), float64(limits.MaxTotalBytes.Int64())}
			}
			got.Output, err = child.RunWithContext(context.Background(), "inspect", childRun)
			if err != nil {
				t.Fatal(err)
			}
			got.Observations = probe.observations
			got.ForeignRecovered, got.OwnRecovered = foreign, own
			content, err := old.GetMasked(parentRef)
			if err != nil {
				t.Fatal(err)
			}
			got.ParentSeedRetained = content == "home-only masked seed"
			got.ParentStoreOpen = !old.Closed()
			got.ChildStoreOpen = raw != nil && !raw.Closed()
			// Closing the parent must fence this distinct child runtime clone as well.
			if _, err := runtime.Close(context.Background(), efficiency.LifecycleCloseOptions{}); err != nil {
				t.Fatal(err)
			}
			o, err := efficiency.NewObservation(efficiency.DefaultObservation("closed probe"))
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := child.tokenEfficiency.ReduceObservation(context.Background(), o, efficiency.ReductionOptions{}, false)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := outcome.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			got.SharedRoot = len(snapshot.Receipts) == 1 && snapshot.Receipts[0].Reason == "runtime_closed"
			if !reflect.DeepEqual(got, row) {
				t.Fatalf("got %+v\nwant %+v", got, row)
			}
		})
	}
}
