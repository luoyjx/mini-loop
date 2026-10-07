package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/teams"
)

type teamEffectStep struct {
	Action    string
	Name      protocol.ToolName
	Actor     string
	InputJSON string `json:"input_json"`
	RowJSON   string `json:"row_json"`
	Repeat    int
	Output    string
	Error     string
	Shutdown  bool `json:"shutdown_requested"`
	Count     int  `json:"state_count"`
}
type teamEffectCase struct {
	Name         string
	Unconfigured bool
	Members      []string
	Steps        []teamEffectStep
}

// Source and native fixtures establish trusted identities before concurrent
// readers start. This is handler/manager binding evidence, not native spawning.
func teamTestFleet(t *testing.T, cfg ManagerConfig, names []string) (*SessionManager, map[string]*ManagedSession) {
	t.Helper()
	cfg.Services.TeamTools = true
	m := makeManager(t, cfg)
	sessions := map[string]*ManagedSession{}
	for _, name := range append([]string{"lead"}, names...) {
		s := createManaged(t, m, CreateSessionRequest{Owner: "alice", PermissionMode: ModeAuto})
		s.core.team = &teams.Identity{Team: "team", Name: teams.MemberName(name)}
		sessions[name] = s
	}
	return m, sessions
}
func teamDispatch(t *testing.T, s *Session, run RunContext, id string, input protocol.ToolInput) ToolOutcome {
	t.Helper()
	out, err := s.gate.Dispatch(context.Background(), ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: s.permissionMode(), RunContext: run}, ToolCall{ID: id, Input: input})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTeamEffectsMatchActualSourceHandlers(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-team-effects.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []teamEffectCase }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 11 {
		t.Fatal("incomplete corpus")
	}
	created := regexp.MustCompile(`("created_at": )[^,\n]+`)
	for _, sample := range fixture.Cases {
		t.Run(sample.Name, func(t *testing.T) {
			var m *SessionManager
			sessions := map[string]*Session{}
			if sample.Unconfigured {
				cfg := runtimeConfig(t.TempDir(), cronDoneProvider{})
				cfg.TeamTools = true
				cfg.Mode = ModeAuto
				s, err := NewRuntimeSession(cfg)
				if err != nil {
					t.Fatal(err)
				}
				sessions["lead"] = s
			} else {
				var fleet map[string]*ManagedSession
				m, fleet = teamTestFleet(t, managerTestConfig(t.TempDir(), cronDoneProvider{}), sample.Members)
				for name, s := range fleet {
					sessions[name] = s.core
				}
			}
			aliases := map[teams.RequestID]string{}
			run, _ := DefaultRunContext()
			for i, row := range sample.Steps {
				if row.Action == "inject" {
					path := filepath.Join(m.WorkspaceRoot(), ".teams", "team", "inboxes", row.Actor+".jsonl")
					f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					_, err = f.WriteString(row.RowJSON + "\n")
					closeErr := f.Close()
					if err != nil || closeErr != nil {
						t.Fatal(err, closeErr)
					}
					continue
				}
				raw := row.InputJSON
				for id, alias := range aliases {
					raw = strings.ReplaceAll(raw, alias, string(id))
				}
				input, err := protocol.DecodeToolInput(row.Name, []byte(raw))
				if err != nil {
					t.Fatal(err)
				}
				if row.Repeat > 1 {
					switch row.Name {
					case protocol.ToolBroadcast:
						v, _ := input.Broadcast()
						v.Content = strings.Repeat(v.Content, row.Repeat)
						input = protocol.BroadcastToolInput(v)
					case protocol.ToolSendMessage:
						v, _ := input.SendMessage()
						v.Content = strings.Repeat(v.Content, row.Repeat)
						input = protocol.SendMessageToolInput(v)
					default:
						t.Fatal("unsupported repeat")
					}
				}
				s := sessions[row.Actor]
				out := teamDispatch(t, s, run, fmt.Sprint(i), input)
				count := 0
				if m != nil {
					states := m.teamProtocols.Snapshot()
					count = len(states)
					for _, state := range states {
						if _, ok := aliases[state.RequestID]; !ok {
							aliases[state.RequestID] = fmt.Sprintf("<request%d>", len(aliases)+1)
						}
					}
				}
				got := created.ReplaceAllString(out.Output, "${1}0.0")
				for id, alias := range aliases {
					got = strings.ReplaceAll(got, string(id), alias)
				}
				if row.Error != "" {
					if !out.Failed {
						t.Fatalf("%d expected source fault %s, got %+v", i, row.Error, out)
					}
				} else if out.IsError() || got != row.Output {
					t.Fatalf("%d %s got %+v / %q; source %q", i, row.Name, out, got, row.Output)
				}
				if count != row.Count || s.teamShutdown.Load() != row.Shutdown {
					t.Fatalf("%d count/shutdown %d/%t source %d/%t", i, count, s.teamShutdown.Load(), row.Count, row.Shutdown)
				}
			}
		})
	}
}

func TestTeamCatalogMatchesSourceTraitsAndExplicitSelection(t *testing.T) {
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	m := makeManager(t, cfg)
	bare := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	for _, schema := range protocol.TeamSchemas() {
		if _, ok := bare.core.gate.catalog.Lookup(schema.Name); ok {
			t.Fatal("unexpected default", schema.Name)
		}
	}
	data, err := os.ReadFile("../testdata/python-team-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Tools []struct {
			Schema       protocol.ToolSchema
			Risk         ToolRisk
			Readonly     bool
			ParallelSafe bool `json:"parallel_safe"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	cfg.Services.TeamTools = true
	m = makeManager(t, cfg)
	s := createManaged(t, m, CreateSessionRequest{Owner: "alice"})
	if len(fixture.Tools) != 10 {
		t.Fatal("incomplete source traits")
	}
	for _, row := range fixture.Tools {
		def, ok := s.core.gate.catalog.Lookup(row.Schema.Name)
		if !ok || def.Risk() != row.Risk || def.Readonly() != row.Readonly || def.ParallelSafe() != row.ParallelSafe {
			t.Fatal(row, def)
		}
		got, _ := json.Marshal(def.schema)
		want, _ := json.Marshal(row.Schema)
		if string(got) != string(want) {
			t.Fatalf("schema %s / %s", got, want)
		}
	}
	selected := createManaged(t, m, CreateSessionRequest{Owner: "alice", ToolSelection: SelectTools(protocol.ToolReadInbox, protocol.ToolSpawnTeammate)})
	if names := selected.core.gate.CatalogNames(); len(names) != 2 {
		t.Fatal(names)
	}
	fork, err := m.Fork(context.Background(), "alice", s.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fork.core.gate.catalog.Lookup(protocol.ToolReadInbox); !ok || fork.core.team.Team == s.core.team.Team {
		t.Fatal("fork team/catalog")
	}
}

func TestTeamGateRewriteMaskReplayAndOwnerChecks(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("KEY", "team-private-secret")
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.Secrets = registry
	cfg.Services.Hooks.Before = []BeforeHook{beforeHookFunc(func(_ context.Context, _ ToolAuthority, c ToolCall) (BeforeDecision, error) {
		if c.Name() == protocol.ToolSendMessage {
			v, _ := c.Input.SendMessage()
			v.To = "bob"
			v.Content = "rewritten team-private-secret"
			return RewriteToolCall(protocol.SendMessageToolInput(v)), nil
		}
		return KeepToolCall(), nil
	})}
	m, fleet := teamTestFleet(t, cfg, []string{"bob"})
	s := fleet["lead"].core
	run, _ := DefaultRunContext()
	input := protocol.SendMessageToolInput(protocol.SendMessageInput{To: "foreign", Content: "original"})
	first := teamDispatch(t, s, run, "same", input)
	second := teamDispatch(t, s, run, "same", input)
	if first.IsError() || !second.Replayed || first.Output != second.Output {
		t.Fatal(first, second)
	}
	rows, err := m.teams.Peek(context.Background(), "team/bob")
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	text, _ := rows[0].Data().Lookup("content")
	content, _ := text.Text()
	if strings.Contains(content, "team-private-secret") || !strings.Contains(content, "rewritten") {
		t.Fatal(content)
	}
	// Read a private inbox result, then try to retrieve that exact settled action
	// through foreign owner/session/workspace authority. Guards precede replay.
	kind := teams.MessageText
	_, err = m.teams.Send(context.Background(), teams.SendRequest{From: "team/bob", To: "team/lead", Content: "private inbox", Type: &kind})
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ID: "read", Input: protocol.ReadInboxToolInput()}
	authority := ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: ModeAuto, RunContext: run}
	good, err := s.gate.Dispatch(context.Background(), authority, call)
	if err != nil || good.IsError() || !strings.Contains(good.Output, "private inbox") {
		t.Fatal(good, err)
	}
	for _, change := range []string{"owner", "session", "workspace"} {
		foreign := authority
		switch change {
		case "owner":
			foreign.OwnerID = "bob"
		case "session":
			foreign.SessionID = fleet["bob"].ID()
		case "workspace":
			foreign.Workspace = fleet["bob"].core.executionRoot()
		}
		out, err := s.gate.Dispatch(context.Background(), foreign, call)
		if err != nil || !out.Denied || out.Replayed || strings.Contains(out.Output, "private inbox") {
			t.Fatal(change, out, err)
		}
	}
	if _, err := m.Delete("alice", s.id, DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := s.gate.Dispatch(context.Background(), authority, call)
	if err != nil || !out.Denied || out.Replayed {
		t.Fatal("deleted replay", out, err)
	}
}

func TestReadonlyTeamConsumptionStillRoutesSourceShutdownAcknowledgment(t *testing.T) {
	m, fleet := teamTestFleet(t, managerTestConfig(t.TempDir(), cronDoneProvider{}), []string{"bob"})
	run, _ := DefaultRunContext()
	lead, bob := fleet["lead"].core, fleet["bob"].core
	if out := teamDispatch(t, lead, run, "shutdown", protocol.RequestShutdownToolInput(protocol.RequestShutdownInput{Target: "bob"})); out.IsError() {
		t.Fatal(out)
	}
	if _, err := fleet["bob"].ChangePermissionMode(ModeReadonly); err != nil {
		t.Fatal(err)
	}
	denied := teamDispatch(t, bob, run, "send", protocol.SendMessageToolInput(protocol.SendMessageInput{To: "lead", Content: "denied"}))
	if !denied.Denied {
		t.Fatal(denied)
	}
	read := teamDispatch(t, bob, run, "read", protocol.ReadInboxToolInput())
	if read.IsError() || !bob.teamShutdown.Load() {
		t.Fatal(read)
	}
	states := m.teamProtocols.Snapshot()
	if len(states) != 1 || states[0].Status != teams.ProtocolPending {
		t.Fatal(states)
	}
	if out := teamDispatch(t, lead, run, "read", protocol.ReadInboxToolInput()); out.IsError() {
		t.Fatal(out)
	}
	if states = m.teamProtocols.Snapshot(); states[0].Status != teams.ProtocolApproved {
		t.Fatal(states)
	}
}

type teamChildProvider struct {
	called bool
	output string
}

func (p *teamChildProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if !p.called {
		p.called = true
		return protocol.ModelReply{ID: "team-child", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewToolUse("send", protocol.SendMessageToolInput(protocol.SendMessageInput{To: "lead", Content: "child"}))}, StopReason: protocol.StopToolUse}, nil
	}
	for _, message := range request.Messages {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if result, ok := block.ToolResult(); ok {
				p.output = result.Content
			}
		}
	}
	return cronDoneProvider{}.Complete(context.Background(), request)
}
func TestSelectedTeamChildRebindsGuardWithoutParentManagerAuthority(t *testing.T) {
	provider := &teamChildProvider{}
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.Services.TeamTools = true
	cfg.Services.RoleToolPolicy = allRoleTools{}
	m := makeManager(t, cfg)
	parent := createManaged(t, m, CreateSessionRequest{Owner: "alice", ToolSelection: SelectTools(protocol.ToolSendMessage)})
	if _, err := parent.core.Delegate(context.Background(), "work", RoleGeneralPurpose); err != nil {
		t.Fatal(err)
	}
	if provider.output != "Error: message bus not available" {
		t.Fatal("inherited manager authority or wrong guard", provider.output)
	}
	view, err := m.PeekTeam(context.Background(), "alice", parent.ID())
	if err != nil || len(view.Inbox) != 0 {
		t.Fatal(view, err)
	}
}
