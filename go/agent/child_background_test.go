package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type childBackgroundCase struct {
	Name, Action         string
	Role                 AgentRole
	LiveParent           bool `json:"live_parent"`
	DefaultRole          bool `json:"default_role"`
	Summary, Output      string
	Tools                []protocol.ToolName
	ChildHasService      bool               `json:"child_has_service"`
	ChildDistinctService bool               `json:"child_distinct_service"`
	ChildLiveAfterReturn int                `json:"child_live_after_return"`
	OwnStatus            *background.Status `json:"own_status"`
	ParentStillRunning   bool               `json:"parent_still_running"`
	ParentLedgerExists   *bool              `json:"parent_ledger_exists"`
	ParentReportedOrphan bool               `json:"parent_reported_orphan"`
}

type checkOnlyRole struct{}

func (checkOnlyRole) Select(_ AgentRole, parent *ToolCatalog) (*ToolCatalog, error) {
	definition, ok := parent.Lookup(protocol.ToolCheckBackground)
	if !ok {
		return nil, errors.New("missing check tool")
	}
	return NewToolCatalog(definition)
}

type childBackgroundProvider struct {
	root          *Session
	caseData      childBackgroundCase
	stage         int
	state         *backgroundState
	id            background.ID
	output        string
	names         []protocol.ToolName
	sawCompletion bool
}

func (p *childBackgroundProvider) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.stage++
	if p.stage == 1 {
		for _, tool := range r.Tools {
			p.names = append(p.names, tool.Name)
		}
		p.root.background.mu.Lock()
		if n := len(p.root.background.children); n > 0 {
			p.state = p.root.background.children[n-1]
		}
		p.root.background.mu.Unlock()
		input := protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "printf child"})
		if p.caseData.LiveParent {
			input = protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "touch child-started; sleep 30"})
		}
		if p.caseData.Action == "bash" {
			yes := true
			input = protocol.BashToolInput(protocol.BashInput{Command: "printf child", RunInBackground: &yes})
		}
		if p.caseData.Action == "check" {
			input = protocol.CheckBackgroundToolInput(protocol.CheckBackgroundInput{})
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("child", input)}, protocol.StopToolUse), nil
	}
	for _, message := range r.Messages {
		if parts, ok := message.Content.Blocks(); ok {
			for _, part := range parts {
				if result, ok := part.ToolResult(); ok && result.ToolUseID == "child" {
					p.output = result.Content
				}
			}
		}
		if text, ok := message.Content.Plain(); ok && strings.Contains(text, "<task_notification") && strings.Contains(text, "\nchild\n") {
			p.sawCompletion = true
		}
	}
	if p.stage == 2 && strings.HasPrefix(p.output, "Started background task ") {
		p.id = background.ID(strings.TrimSuffix(strings.Fields(p.output)[3], ":"))
		manager, err := p.state.get(ctx, false)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		if p.caseData.LiveParent {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(filepath.Join(p.root.executionRoot(), "child-started")); err == nil {
					break
				}
				if err := ctx.Err(); err != nil {
					return protocol.ModelReply{}, err
				}
				time.Sleep(time.Millisecond)
			}
		} else {
			if err := manager.Wait(ctx, p.id); err != nil {
				return protocol.ModelReply{}, err
			}
			id := string(p.id)
			return fakeReply([]protocol.Block{protocol.NewToolUse("child-check", protocol.CheckBackgroundToolInput(protocol.CheckBackgroundInput{ID: &id}))}, protocol.StopToolUse), nil
		}
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("child done")}, protocol.StopEndTurn), nil
}

func TestSelectedChildBackgroundContractsAndLiveParentLedgerGap(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-child-background.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []childBackgroundCase }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 6 {
		t.Fatal("missing actual source child cases")
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			p := &childBackgroundProvider{caseData: c}
			root := backgroundRuntime(t, t.TempDir(), p, true)
			p.root = root
			if !c.DefaultRole {
				root.rolePolicy = allRoleTools{}
			}
			if c.Action == "check" {
				root.rolePolicy = checkOnlyRole{}
			}
			if c.LiveParent {
				backgroundDispatch(t, root, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "touch parent-started; sleep 30"}))
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(filepath.Join(root.workspace, "parent-started")); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
			}
			if out, err := root.Delegate(context.Background(), "child", c.Role); err != nil || out != c.Summary {
				t.Fatal(out, err)
			}
			slices.Sort(p.names)
			want := slices.Clone(c.Tools)
			slices.Sort(want)
			if !slices.Equal(p.names, want) {
				t.Fatal(p.names, want)
			}
			output := p.output
			if p.id != "" {
				output = strings.ReplaceAll(output, string(p.id), "<child-id>")
			}
			if output != c.Output {
				t.Fatal(output, c.Output)
			}
			var service *background.Manager
			if p.state != nil {
				p.state.mu.Lock()
				service = p.state.manager
				p.state.mu.Unlock()
			}
			// Source's inherited injector mistakes the live parent's ledger for an
			// orphan even in a default role. Go's default role has no background state.
			wantService := c.ChildHasService && !c.DefaultRole
			if (service != nil) != wantService {
				t.Fatal("child service visibility differs", service != nil, wantService)
			}
			if service != nil {
				if service == root.background.manager {
					t.Fatal("child borrowed parent service")
				}
				if service.LiveCount() != c.ChildLiveAfterReturn {
					t.Fatal(service.LiveCount(), c.ChildLiveAfterReturn)
				}
				if c.OwnStatus != nil {
					record, ok := service.Get(p.id)
					if !ok || record.Status != *c.OwnStatus {
						t.Fatal(record, ok)
					}
				}
				if _, ok := service.Get("bg_0001"); ok {
					t.Fatal("child adopted live parent")
				}
				if p.id != "" && !strings.HasPrefix(string(p.id), "bg_"+string(p.state.scope)+"_") {
					t.Fatal("child task lacks immutable scope", p.id)
				}
				if c.OwnStatus != nil && *c.OwnStatus == background.Completed && !p.sawCompletion {
					t.Fatal("completion did not enter child model request")
				}
			}
			if c.LiveParent {
				if !c.ParentReportedOrphan || c.ParentLedgerExists == nil || *c.ParentLedgerExists {
					t.Fatal("source ledger gap missing from fixture")
				}
				if root.background.manager.LiveCount() != 1 {
					t.Fatal("parent task was borrowed or cancelled")
				}
				if _, err := os.Stat(filepath.Join(root.workspace, ".background/bg_0001.json")); err != nil {
					t.Fatal("live parent ledger lost", err)
				}
				if service != nil {
					if _, err := os.Stat(filepath.Join(root.workspace, ".background", string(p.id)+".json")); err != nil {
						t.Fatal("child ledger overwritten", err)
					}
				}
			}
			if err := root.CloseBackground(context.Background()); err != nil {
				t.Fatal(err)
			}
			if service != nil && service.LiveCount() != 0 {
				t.Fatal("child survived owner close")
			}
		})
	}
}

func TestManagedOwnerJoinsReturnedChildTaskWithNoParentTask(t *testing.T) {
	c := childBackgroundCase{Action: "run", LiveParent: true, Role: RoleWorker}
	p := &childBackgroundProvider{caseData: c}
	config := managerTestConfig(t.TempDir(), p)
	config.Services.BackgroundTools = true
	config.Services.RoleToolPolicy = allRoleTools{}
	m := makeManager(t, config)
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	p.root = s.core
	if _, err := s.core.Delegate(context.Background(), "child", RoleWorker); err != nil {
		t.Fatal(err)
	}
	service := p.state.manager
	if s.core.background.manager.LiveCount() != 0 || service.LiveCount() != 1 {
		t.Fatal("child did not outlive return independently")
	}
	if removed, err := m.Delete("owner", s.ID(), DeleteSessionOptions{}); !removed || err != nil {
		t.Fatal(removed, err)
	}
	if err := m.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if service.LiveCount() != 0 || service.Check(p.id) != "[cancelled] Cancelled" {
		t.Fatal("retired owner leaked child", service.Check(p.id))
	}
	if _, err := os.Stat(s.core.workspace); !os.IsNotExist(err) {
		t.Fatal("workspace retained after tree join", err)
	}
}

type nestedBackgroundProvider struct{ stage int }

func (p *nestedBackgroundProvider) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	p.stage++
	switch p.stage {
	case 1:
		role := protocol.AgentGeneralPurpose
		return fakeReply([]protocol.Block{protocol.NewToolUse("nested", protocol.TaskToolInput(protocol.TaskInput{Prompt: "grandchild", AgentType: &role}))}, protocol.StopToolUse), nil
	case 2:
		return fakeReply([]protocol.Block{protocol.NewToolUse("grand-bg", protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "sleep 30"}))}, protocol.StopToolUse), nil
	default:
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
}

func TestNestedBackgroundScopesRemainOwnedAndCloseCanResume(t *testing.T) {
	p := &nestedBackgroundProvider{}
	s := backgroundRuntime(t, t.TempDir(), p, true)
	s.rolePolicy = allRoleTools{}
	if _, err := s.Delegate(context.Background(), "child", RoleWorker); err != nil {
		t.Fatal(err)
	}
	first := s.background.children[0]
	grand := first.children[0]
	if first.scope == grand.scope || grand.manager.LiveCount() != 1 {
		t.Fatal("nested ownership lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = s.CloseBackground(ctx)
	if err := s.CloseBackground(context.Background()); err != nil {
		t.Fatal(err)
	}
	if grand.manager.LiveCount() != 0 {
		t.Fatal("grandchild survived close")
	}
}
