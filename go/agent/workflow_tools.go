package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
	"github.com/luoyjx/mini-loop/go/workspace"
)

func workflowToolTraits(name protocol.ToolName) ToolTraits {
	switch name {
	case protocol.ToolWorkflowStatus:
		return ToolTraits{Risk: RiskRead, Readonly: true}
	case protocol.ToolWorkflowCancel:
		return ToolTraits{Risk: RiskWrite}
	default:
		return ToolTraits{Risk: RiskExec}
	}
}

func requireWorkflowCapability(run RunContext, name protocol.ToolName) error {
	if run.Authority() != AuthorityExplicitHuman || run.Validate() != nil {
		return workflowServiceError(WorkflowPermissionError, "workflow operations require an explicit_human trusted local context")
	}
	capability := CapabilityWorkflowManage
	if name == protocol.ToolWorkflow {
		capability = CapabilityWorkflowLaunch
	}
	if !run.Allows(capability) {
		return workflowServiceError(WorkflowPermissionError, "workflow operation requires per-message "+string(capability)+" approval")
	}
	return nil
}

// Binding and capability checks precede cached journal results as well as effects.
// A replay must not expose a private projection through a foreign runtime binding.
type workflowAuthorityGuard struct{ handler *runtimeHandler }

func (g workflowAuthorityGuard) GuardTool(ctx context.Context, authority ToolAuthority, call ToolCall) (string, bool, error) {
	switch call.Name() {
	case protocol.ToolWorkflow, protocol.ToolWorkflowStatus, protocol.ToolWorkflowCancel:
	default:
		return "", false, nil
	}
	h := g.handler
	h.mu.Lock()
	binding := h.binding
	manager := h.workflowManager
	h.mu.Unlock()
	if authority.SessionID != binding.SessionID || authority.OwnerID != binding.OwnerID {
		return "Error: runtime handler identity does not match bound session", true, nil
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil || root != binding.Workspace {
		return "Error: runtime handler workspace does not match bound session", true, nil
	}
	if manager != nil {
		if _, err := manager.Get(binding.OwnerID, binding.SessionID); err != nil {
			return "Error: " + err.Error(), true, nil
		}
		if manager.State() != ManagerActive {
			return "Error: " + ErrManagerStopped.Error(), true, nil
		}
	}
	if err := requireWorkflowCapability(authority.RunContext, call.Name()); err != nil {
		return "Error: " + err.Error(), true, nil
	}
	return "", false, ctx.Err()
}

// Called under the runtime handler lock, after the immutable binding check.
func (h *runtimeHandler) executeWorkflow(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	if err := requireWorkflowCapability(authority.RunContext, input.Name()); err != nil {
		return "", err
	}
	if input.Name() == protocol.ToolWorkflow {
		if authority.ActionID == "" || authority.ToolUseID == "" || h.session == nil || h.session.gate.journal == nil {
			return "", workflowServiceError(WorkflowRuntimeError, "workflow launch requires a journaled tool action")
		}
		if h.workflowParent == nil {
			return "", workflowServiceError(WorkflowRuntimeError, "workflow launch requires a managed AgentSession")
		}
	}
	if h.workflows == nil {
		return "", workflowServiceError(WorkflowRuntimeError, "workflow service is not available")
	}
	switch input.Name() {
	case protocol.ToolWorkflow:
		value, ok := input.Workflow()
		if !ok {
			return "", errors.New("workflow launch requires typed input")
		}
		h.workflowParent.mu.Lock()
		turn := h.workflowParent.runCount
		h.workflowParent.mu.Unlock()
		result, err := h.workflows.Launch(ctx, WorkflowLaunchRequest{parent: h.workflowParent, SessionID: authority.SessionID, Input: value, Context: authority.RunContext, ActionID: authority.ActionID, ToolUseID: authority.ToolUseID, LaunchTurn: workflows.ParentTurn(turn), ActionInput: &value})
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return workflowToolJSON(data)
	case protocol.ToolWorkflowStatus:
		value, ok := input.WorkflowReference()
		if !ok {
			return "", errors.New("workflow status requires typed input")
		}
		session := workflows.SessionID(authority.SessionID)
		result, err := h.workflows.Status(value.RunID, &session)
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(result)
		if err != nil {
			return "", err
		}
		return workflowToolJSON(data)
	case protocol.ToolWorkflowCancel:
		value, ok := input.WorkflowReference()
		if !ok {
			return "", errors.New("workflow cancel requires typed input")
		}
		session := workflows.SessionID(authority.SessionID)
		result, err := h.workflows.Cancel(ctx, value.RunID, &session, "cancelled by trusted parent")
		if err != nil {
			return "", err
		}
		data, err := json.Marshal(struct {
			Run    workflows.RunID     `json:"run_id"`
			Status workflows.RunStatus `json:"status"`
		}{result.RunID, result.Status})
		if err != nil {
			return "", err
		}
		return workflowToolJSON(data)
	default:
		return "", errors.New("unsupported workflow tool")
	}
}

func workflowToolJSON(data []byte) (string, error) {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return "", err
	}
	return protocol.PythonJSON(value.Sorted(), false, false)
}
