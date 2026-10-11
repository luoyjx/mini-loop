package httpapi

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type workflowArtifactProvider struct{}

func (workflowArtifactProvider) Complete(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	reply := protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: req.Model, StopReason: protocol.StopEndTurn, Content: []protocol.Block{protocol.NewTextBlock("done")}}
	if len(req.Messages) == 1 {
		reply.StopReason = protocol.StopToolUse
		reply.Content = []protocol.Block{protocol.NewToolUse("a", protocol.ReturnArtifactToolInput(jsonvalue.ObjectValue(nil)))}
	}
	return reply, nil
}

func TestOwnedWorkflowSummaryAndSSEProjectionRemainOwnerScoped(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: workflowArtifactProvider{}, WorkflowTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	row := create(t, s, "token-a")
	if health := decode[HealthResponse](t, request(s, "GET", "/healthz", "", "token-a").Body.Bytes()); !health.ExperimentalWorkflows {
		t.Fatal("enabled workflow service hidden by health projection")
	}
	parent, err := manager.Get("alice", row.ID)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := jsonvalue.Decode(`{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	runContext, err := agent.ExplicitHumanRunContext(agent.HumanRunConfig{ApprovedCapabilities: []agent.RunCapability{agent.CapabilityWorkflowLaunch}})
	if err != nil {
		t.Fatal(err)
	}
	launch, err := manager.Workflows().Launch(ctx, agent.WorkflowLaunchRequest{SessionID: row.ID, Input: protocol.WorkflowInput{Definition: definition, Args: jsonvalue.ObjectValue(nil)}, Context: runContext, ActionID: "action", ToolUseID: "u", LaunchTurn: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Workflows().Wait(ctx, launch.RunID); err != nil {
		t.Fatal(err)
	}
	response := request(s, "GET", "/sessions/"+string(row.ID), "", "token-a")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	projected := decode[SessionInfo](t, response.Body.Bytes())
	if len(projected.Workflows) != 1 || projected.Workflows[0].RunID != launch.RunID || projected.Workflows[0].Status != workflows.RunCompleted {
		t.Fatal(projected.Workflows)
	}
	if foreign := request(s, "GET", "/sessions/"+string(row.ID), "", "token-b"); foreign.Code != 404 {
		t.Fatal(foreign.Code, foreign.Body.String())
	}
	records := parent.Events()
	last := records[len(records)-1]
	if last.Event.Kind() != agent.SessionEventKind(agent.WorkflowResultEnqueued) {
		t.Fatal(last.Event.Kind())
	}
	server := httptest.NewServer(s)
	defer server.Close()
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, _ := http.NewRequestWithContext(streamCtx, "GET", server.URL+"/sessions/"+string(row.ID)+"/events", nil)
	req.Header.Set("Authorization", "Bearer token-a")
	req.Header.Set("Last-Event-ID", strconv.FormatUint(uint64(last.Sequence-1), 10))
	stream, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	scanner := bufio.NewScanner(stream.Body)
	var frame strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		frame.WriteString(line + "\n")
		if line == "" && strings.Contains(frame.String(), "data:") {
			break
		}
	}
	frames := parseFrames(t, frame.String())
	if len(frames) != 1 || frames[0].Event != string(agent.WorkflowResultEnqueued) || frames[0].Seq != int64(last.Sequence) {
		t.Fatal(frame.String())
	}
	if !strings.Contains(frame.String(), `"sequence":`+strconv.FormatUint(uint64(last.Sequence), 10)) || !strings.Contains(frame.String(), string(launch.RunID)) {
		t.Fatal(frame.String())
	}
	cancel()
	stream.Body.Close()
	wait(t, func() bool { return parent.Info().Subscribers == 0 })
}
