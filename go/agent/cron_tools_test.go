package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/cron"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCronGateRewriteMaskReplayAndForeignAuthority(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("KEY", "cron-private-secret")
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.CronTools = true
	cfg.Services.Secrets = registry
	cfg.Services.Hooks.Before = []BeforeHook{beforeHookFunc(func(_ context.Context, _ ToolAuthority, c ToolCall) (BeforeDecision, error) {
		if c.Name() == protocol.ToolScheduleCron {
			v, _ := c.Input.ScheduleCron()
			v.Prompt = "rewritten cron-private-secret"
			return RewriteToolCall(protocol.ScheduleCronToolInput(v)), nil
		}
		return KeepToolCall(), nil
	})}
	m := makeManager(t, cfg)
	a := createManaged(t, m, CreateSessionRequest{Owner: "alice", PermissionMode: ModeAuto})
	run, _ := DefaultRunContext()
	authority := ToolAuthority{SessionID: a.ID(), OwnerID: "alice", Workspace: a.core.executionRoot(), Mode: ModeAuto, RunContext: run}
	call := ToolCall{ID: "same", Input: protocol.ScheduleCronToolInput(protocol.ScheduleCronInput{Cron: "0 0 31 2 *", Prompt: "original"})}
	first, err := a.core.gate.Dispatch(context.Background(), authority, call)
	if err != nil || first.IsError() || strings.Contains(first.Output, "cron-private-secret") {
		t.Fatal(first, err)
	}
	second, err := a.core.gate.Dispatch(context.Background(), authority, call)
	if err != nil || !second.Replayed || second.Output != first.Output {
		t.Fatal(second, err)
	}
	jobs, _ := m.CronJobs("alice", a.ID())
	if len(jobs) != 1 || jobs[0].Prompt != "rewritten cron-private-secret" {
		t.Fatal(jobs)
	}
	stored, err := os.ReadFile(m.WorkspaceRoot() + "/.cron.json")
	if err != nil || strings.Contains(string(stored), "cron-private-secret") {
		t.Fatal(string(stored), err)
	}
	authority.OwnerID = "bob"
	call.ID = "foreign"
	if out, err := a.core.gate.Dispatch(context.Background(), authority, call); err == nil && !out.Failed {
		t.Fatal("foreign runtime authority admitted", out)
	}
	if len(m.cron.Jobs()) != 1 {
		t.Fatal("foreign mutation")
	}
}

type cronSurfaceFixture struct {
	Schemas  []protocol.ToolSchema
	Metadata []struct {
		Name         protocol.ToolName
		Risk         ToolRisk
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Capabilities []Capability
	}
	Variants []struct {
		Name                protocol.ToolName
		Input               json.RawMessage
		Candidate, Proposed []string
	}
	Tools []struct {
		Name  string
		Steps []struct {
			Name   protocol.ToolName
			Input  json.RawMessage
			Output string
			Jobs   int
		}
	}
}

func cronSurfaces(t *testing.T) cronSurfaceFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-cron-surfaces.json")
	if err != nil {
		t.Fatal(err)
	}
	var f cronSurfaceFixture
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
func TestCronToolsMatchActualSourceGateAndOwnerBinding(t *testing.T) {
	f := cronSurfaces(t)
	if len(f.Tools) != 3 || len(f.Metadata) != 3 || !reflect.DeepEqual(f.Schemas, protocol.CronSchemas()) {
		t.Fatal("incomplete/different source schema")
	}
	for _, sample := range f.Tools {
		t.Run(sample.Name, func(t *testing.T) {
			var m *SessionManager
			var s *Session
			if sample.Name == "unconfigured" {
				cfg := runtimeConfig(t.TempDir(), cronDoneProvider{})
				cfg.CronTools = true
				cfg.Mode = ModeAuto
				s, _ = NewRuntimeSession(cfg)
			} else {
				cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
				cfg.Services.CronTools = true
				m = makeManager(t, cfg)
				mode := ModeAuto
				if sample.Name == "readonly" {
					mode = ModeReadonly
				}
				s = createManaged(t, m, CreateSessionRequest{Owner: "alice", PermissionMode: mode}).core
			}
			if s == nil {
				t.Fatal("no runtime")
			}
			for _, row := range f.Metadata {
				def, ok := s.gate.catalog.Lookup(row.Name)
				if !ok || def.Risk() != row.Risk || def.Readonly() != row.Readonly || def.ParallelSafe() != row.ParallelSafe || !slices.Equal(def.Capabilities(), row.Capabilities) {
					t.Fatal(row)
				}
			}
			for _, row := range f.Variants {
				v, e := protocol.DecodeToolInput(row.Name, row.Input)
				if e != nil {
					t.Fatal(e)
				}
				if !slices.Equal(DefaultGrantCandidate(v).Tokens(), row.Candidate) || !slices.Equal(ProposedGrantCandidate(v).Tokens(), row.Proposed) {
					t.Fatal(row.Name)
				}
			}
			aliases := map[cron.ID]string{}
			for i, row := range sample.Steps {
				raw := string(row.Input)
				for id, alias := range aliases {
					raw = strings.ReplaceAll(raw, alias, string(id))
				}
				v, e := protocol.DecodeToolInput(row.Name, []byte(raw))
				if e != nil {
					t.Fatal(e)
				}
				run, e := DefaultRunContext()
				if e != nil {
					t.Fatal(e)
				}
				out, e := s.gate.Dispatch(context.Background(), ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: s.permissionMode(), RunContext: run}, ToolCall{ID: fmt.Sprint(i), Input: v})
				if e != nil {
					t.Fatal(e)
				}
				count := 0
				if m != nil {
					jobs := m.cron.Jobs()
					count = len(jobs)
					for _, job := range jobs {
						if _, ok := aliases[job.ID]; !ok {
							aliases[job.ID] = fmt.Sprintf("<job%d>", len(aliases)+1)
						}
					}
				}
				got := out.Output
				for id, alias := range aliases {
					got = strings.ReplaceAll(got, string(id), alias)
				}
				if got != row.Output || count != row.Jobs {
					t.Fatalf("step %d: %q / %q; jobs %d / %d", i, got, row.Output, count, row.Jobs)
				}
			}
		})
	}
}

func TestCronModelTurnForkAndSelectedChildKeepSessionScope(t *testing.T) {
	p := &cronModelProvider{}
	cfg := managerTestConfig(t.TempDir(), p)
	cfg.Services.CronTools = true
	cfg.Services.RoleToolPolicy = allRoleTools{}
	m := makeManager(t, cfg)
	a := createManaged(t, m, CreateSessionRequest{Owner: "alice", PermissionMode: ModeAuto})
	if _, err := a.Run(context.Background(), "schedule"); err != nil {
		t.Fatal(err)
	}
	jobs, _ := m.CronJobs("alice", a.ID())
	if len(jobs) != 1 || jobs[0].Prompt != "from model" {
		t.Fatal(jobs)
	}
	fork, err := m.Fork(context.Background(), "alice", a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := m.CronJobs("alice", fork.ID()); len(rows) != 0 {
		t.Fatal("copied jobs")
	}
	if _, ok := fork.core.gate.catalog.Lookup(protocol.ToolScheduleCron); !ok {
		t.Fatal("fork lost activation")
	}
	// A selected child gets fresh state, even under a policy selecting all tools.
	p.child = true
	if _, err := a.core.Delegate(context.Background(), "child", RoleExplore); err != nil {
		t.Fatal(err)
	}
	if p.childOutput != "Error: cron not available" {
		t.Fatal("child borrowed manager cron", p.childOutput)
	}
	if rows, _ := m.CronJobs("alice", a.ID()); len(rows) != 1 {
		t.Fatal("child changed parent jobs")
	}
}

type cronModelProvider struct {
	stage       int
	child       bool
	childOutput string
}

func (p *cronModelProvider) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	if !p.child && p.stage == 0 {
		p.stage++
		return protocol.ModelReply{ID: "cron-model", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: r.Model, Content: []protocol.Block{protocol.NewToolUse("schedule", protocol.ScheduleCronToolInput(protocol.ScheduleCronInput{Cron: "0 0 31 2 *", Prompt: "from model"}))}, StopReason: protocol.StopToolUse}, nil
	}
	if p.child && p.stage == 1 {
		p.stage++
		return protocol.ModelReply{ID: "cron-child", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: r.Model, Content: []protocol.Block{protocol.NewToolUse("child-list", protocol.ListCronsToolInput())}, StopReason: protocol.StopToolUse}, nil
	}
	if p.child {
		for _, msg := range r.Messages {
			blocks, _ := msg.Content.Blocks()
			for _, block := range blocks {
				if v, ok := block.ToolResult(); ok {
					p.childOutput = v.Content
				}
			}
		}
	}
	return cronDoneProvider{}.Complete(context.Background(), r)
}
