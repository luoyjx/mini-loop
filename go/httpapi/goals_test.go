package httpapi

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"os"
	"testing"
)

func TestGoalViewMatchesSourceOwnershipAndEmptyState(t *testing.T) {
	b, e := os.ReadFile("../testdata/python-goals.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		HTTP []struct {
			Name, Token, Session string
			Status               int
			Body                 json.RawMessage
		}
	}
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	m := testManager(t, doneProvider{})
	s, e := m.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if e != nil {
		t.Fatal(e)
	}
	server := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	for _, row := range f.HTTP {
		id := row.Session
		if id == "session-fixture" {
			id = string(s.ID())
		}
		r := request(server, "GET", "/sessions/"+id+"/goal", "", row.Token)
		if r.Code != row.Status {
			t.Fatal(row.Name, r.Code, r.Body.String())
		}
		compareTrajectoryJSON(t, []byte(replaceSession(r.Body.String(), s.ID())), row.Body)
	}
	if s.GoalSnapshot().Armed || len(s.Messages()) != 0 {
		t.Fatal("read changed session")
	}
}

type goalViewProvider struct{ calls int }

func (p *goalViewProvider) Complete(ctx context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	p.calls++
	reply, err := (doneProvider{}).Complete(ctx, req)
	if err != nil {
		return reply, err
	}
	if p.calls == 1 {
		reply.Content = []protocol.Block{protocol.NewToolUse("create", protocol.CreateGoalToolInput(protocol.CreateGoalInput{Objective: "private-goal"}))}
		reply.StopReason = protocol.StopToolUse
	}
	return reply, nil
}
func TestGoalOwnedViewMasksStateWithoutArmingFromHTTP(t *testing.T) {
	masker := secrets.New(secrets.Config{})
	masker.RegisterValue("KEY", "private-goal")
	provider := &goalViewProvider{}
	m, e := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Defaults: agent.SessionDefaults{PermissionMode: agent.ModeAuto, MaxRounds: 3}, Services: agent.ManagerServices{Provider: provider, GoalTools: true, Secrets: masker, StopHooks: []agent.StopHook{}}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Stop(context.Background()) })
	s, e := m.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if e != nil {
		t.Fatal(e)
	}
	server := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	r := request(server, "POST", "/sessions/"+string(s.ID())+"/messages", `{"message":"create goal"}`, "token-a")
	if r.Code != 200 || s.GoalSnapshot().Goal != nil {
		t.Fatal("HTTP turn armed goal", r.Body.String())
	}
	provider.calls = 0
	run, e := agent.ExplicitHumanRunContext(agent.HumanRunConfig{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RunWithContext(context.Background(), "create", run); e != nil {
		t.Fatal(e)
	}
	r = request(server, "GET", "/sessions/"+string(s.ID())+"/goal", "", "token-a")
	v := decode[GoalResponse](t, r.Body.Bytes())
	if r.Code != 200 || v.Goal == nil || v.Goal.Objective == "private-goal" || !v.GoalArmed || s.GoalSnapshot().Goal.Objective != "private-goal" {
		t.Fatal(r.Body.String(), s.Messages())
	}
}
