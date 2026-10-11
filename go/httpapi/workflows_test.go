package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

func TestWorkflowHTTPDisabledAndValidationOrder(t *testing.T) {
	s := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t)})
	row := create(t, s, "token-a")
	base := "/sessions/" + string(row.ID) + "/workflows"
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
		detail                    string
	}{
		{"GET", base, "", "token-a", 200, ""},
		{"GET", base + "/missing", "", "token-a", 404, "workflows are not enabled"},
		{"POST", base + "/missing/cancel", "{}", "token-a", 404, "workflows are not enabled"},
		{"GET", base, "", "token-b", 404, "No session '" + string(row.ID) + "'"},
		{"POST", base + "/missing/cancel", "{}", "token-b", 404, "No session '" + string(row.ID) + "'"},
		{"POST", base + "/missing/cancel", `{"reason":false}`, "token-b", 422, ""},
		{"POST", base + "/missing/cancel", `{"reason":false}`, "wrong", 401, ""},
	} {
		got := request(s, tc.method, tc.path, tc.body, tc.token)
		if got.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, got.Code, got.Body.String())
		}
		if tc.detail != "" && decode[ErrorResponse](t, got.Body.Bytes()).Detail != tc.detail {
			t.Fatal(got.Body.String())
		}
		if tc.status == 200 && got.Body.String() != "{\"enabled\":false,\"runs\":[]}" {
			t.Fatal(got.Body.String())
		}
	}
	for _, tc := range []struct {
		body     string
		code     RequestValidationCode
		location int
	}{
		{"", validationMissing, 1}, {"null", validationMissing, 1}, {"[]", validationModel, 1},
		{`{"reason":null}`, validationString, 2}, {`{"reason":123}`, validationString, 2},
		{`{"reason":[]}`, validationString, 2}, {`{"reason":{}}`, validationString, 2},
	} {
		got := request(s, "POST", "/sessions/missing/workflows/missing/cancel", tc.body, "token-a")
		if got.Code != 422 {
			t.Fatal(tc.body, got.Code, got.Body.String())
		}
		issue := decode[struct {
			Detail []struct {
				Type     RequestValidationCode `json:"type"`
				Location []string              `json:"loc"`
			} `json:"detail"`
		}](t, got.Body.Bytes()).Detail
		if len(issue) != 1 || issue[0].Type != tc.code || len(issue[0].Location) != tc.location {
			t.Fatal(tc.body, got.Body.String())
		}
	}
}

func TestWorkflowHTTPScopesRunsAndJoinsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider := &blockedProvider{entered: make(chan struct{})}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: provider, WorkflowTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	row, other := create(t, s, "token-a"), create(t, s, "token-a")
	base := "/sessions/" + string(row.ID) + "/workflows"
	empty := request(s, "GET", base, "", "token-a")
	if empty.Code != 200 || empty.Body.String() != "{\"enabled\":true,\"runs\":[]}" {
		t.Fatal(empty.Code, empty.Body.String())
	}
	definition, err := jsonvalue.Decode(`{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	human, err := agent.ExplicitHumanRunContext(agent.HumanRunConfig{ApprovedCapabilities: []agent.RunCapability{agent.CapabilityWorkflowLaunch}})
	if err != nil {
		t.Fatal(err)
	}
	launched, err := manager.Workflows().Launch(ctx, agent.WorkflowLaunchRequest{SessionID: row.ID, Input: protocol.WorkflowInput{Definition: definition, Args: jsonvalue.ObjectValue(nil)}, Context: human, ActionID: "http-cancel", ToolUseID: "u"})
	if err != nil {
		t.Fatal(err)
	}
	receive(t, provider.entered)
	path := base + "/" + string(launched.RunID)
	list := request(s, "GET", base, "", "token-a")
	runs := decode[WorkflowListResponse](t, list.Body.Bytes())
	if list.Code != 200 || !runs.Enabled || len(runs.Runs) != 1 || runs.Runs[0].RunID != launched.RunID {
		t.Fatal(list.Body.String())
	}
	for _, tc := range []struct{ method, path, body, token string }{
		{"GET", path, "", "token-b"},
		{"GET", "/sessions/" + string(other.ID) + "/workflows/" + string(launched.RunID), "", "token-a"},
		{"POST", "/sessions/" + string(other.ID) + "/workflows/" + string(launched.RunID) + "/cancel", "{}", "token-a"},
		{"GET", base + "/missing", "", "token-a"},
	} {
		got := request(s, tc.method, tc.path, tc.body, tc.token)
		if got.Code != 404 {
			t.Fatal(got.Code, got.Body.String())
		}
	}
	got := request(s, "POST", path+"/cancel", `{"reason":"操作员停止","ignored":true}`, "token-a")
	result := decode[WorkflowCancelResponse](t, got.Body.Bytes())
	if got.Code != 200 || result.RunID != launched.RunID || result.Status != workflows.RunCancelled {
		t.Fatal(got.Code, got.Body.String())
	}
	detail := request(s, "GET", path, "", "token-a")
	status := decode[workflows.RunStatusView](t, detail.Body.Bytes())
	if detail.Code != 200 || status.Status != workflows.RunCancelled || len(status.ActiveNodeIDs) != 0 || status.CancelReason == nil || *status.CancelReason != "操作员停止" {
		t.Fatal(detail.Body.String())
	}
	if _, err := manager.Workflows().Wait(ctx, launched.RunID); err != nil {
		t.Fatal(err)
	}
	repeat := request(s, "POST", path+"/cancel", "{}", "token-a")
	if repeat.Code != 200 || decode[WorkflowCancelResponse](t, repeat.Body.Bytes()).Status != workflows.RunCancelled {
		t.Fatal(repeat.Code, repeat.Body.String())
	}
}
