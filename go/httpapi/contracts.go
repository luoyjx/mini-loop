package httpapi

import (
	"context"
	"github.com/luoyjx/mini-loop/go/agent"
)

type principalKey struct{}

func withPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func principal(ctx context.Context) Principal {
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}

type ErrorResponse struct {
	Detail string `json:"detail"`
}
type CreateRequest struct {
	System    *string               `json:"system"`
	Model     *string               `json:"model"`
	Mode      *agent.PermissionMode `json:"mode"`
	Workspace *string               `json:"workspace"`
}
type MessageRequest struct {
	Message *string `json:"message"`
}
type ApprovalDecision string

const DecisionAllow ApprovalDecision = "allow"
const DecisionDeny ApprovalDecision = "deny"

type ApprovalRequest struct {
	Decision ApprovalDecision `json:"decision"`
	Answer   *string          `json:"answer"`
	Remember bool             `json:"remember"`
}

// Workflow projection remains disabled; trajectory state comes from the manager.
type SessionInfo struct {
	agent.SessionInfo
	Workflows [0]struct{} `json:"workflows"`
}

func info(v agent.SessionInfo) SessionInfo { return SessionInfo{SessionInfo: v} }

type MessageResponse struct {
	Session agent.SessionID `json:"session"`
	Final   string          `json:"final"`
	Info    SessionInfo     `json:"info"`
}
type CancelResponse struct {
	Session   agent.SessionID `json:"session"`
	Cancelled bool            `json:"cancelled"`
	Info      SessionInfo     `json:"info"`
}
type DeleteResponse struct {
	Deleted agent.SessionID `json:"deleted"`
}
type ApprovalsResponse struct {
	Session   agent.SessionID          `json:"session"`
	Approvals []agent.ApprovalSnapshot `json:"approvals"`
}
type ApprovalResponse struct {
	ID       agent.ApprovalID `json:"approval_id"`
	Decision ApprovalDecision `json:"decision"`
}
type HealthResponse struct {
	Status                string                 `json:"status"`
	Model                 string                 `json:"model"`
	FakeLLM               bool                   `json:"fake_llm"`
	Features              bool                   `json:"features"`
	ModelConcurrency      agent.ConcurrencyLimit `json:"max_concurrent_llm"`
	ToolConcurrency       agent.ConcurrencyLimit `json:"max_concurrent_tools"`
	ExperimentalWorkflows bool                   `json:"experimental_workflows"`
	Authenticated         bool                   `json:"authenticated"`
	Build                 string                 `json:"build"`
	PID                   int                    `json:"pid"`
	Started               float64                `json:"started_at"`
	Uptime                float64                `json:"uptime_s"`
	Trajectories          bool                   `json:"trajectories"`
	WorkspaceBinding      bool                   `json:"workspace_binding"`
	Sessions              int                    `json:"sessions"`
}

type ModeRequest struct {
	Mode agent.PermissionMode `json:"mode"`
}
type ModeResponse struct {
	Session        agent.SessionID      `json:"session"`
	PermissionMode agent.PermissionMode `json:"permission_mode"`
}
