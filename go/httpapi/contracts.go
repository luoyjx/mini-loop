package httpapi

import (
	"context"
	"github.com/luoyjx/mini-loop/go/agent"
)

type principalKey struct{}

type requestAdmission struct {
	Principal Principal
	Queries   scalarQueryParameters
}

func withAdmission(ctx context.Context, p Principal, query scalarQueryParameters) context.Context {
	// Clear the fallback bearer before carrying query observations into handlers.
	query.AccessToken = scalarQueryValue{}
	if !query.Limit.Present && !query.Envelope.Present && !query.SessionID.Present && !query.Format.Present && !query.Epoch.Present {
		return context.WithValue(ctx, principalKey{}, p)
	}
	return context.WithValue(ctx, principalKey{}, requestAdmission{Principal: p, Queries: query})
}
func principal(ctx context.Context) Principal {
	switch admission := ctx.Value(principalKey{}).(type) {
	case Principal:
		return admission
	case requestAdmission:
		return admission.Principal
	default:
		return Principal{}
	}
}
func admittedQueries(ctx context.Context) scalarQueryParameters {
	admission, _ := ctx.Value(principalKey{}).(requestAdmission)
	return admission.Queries
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

// Session projections, including typed workflow summaries, come from the manager.
type SessionInfo struct {
	agent.SessionInfo
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
	Posture               agent.RuntimePosture   `json:"posture"`
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
