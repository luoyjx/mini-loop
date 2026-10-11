package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

// The core turn lock owns the append. Delivery is installed only for manager-bound
// parents, after user injectors and before steering/posture, matching Source.
func (core *Session) injectWorkflow(ctx context.Context) error {
	if core.runtime == nil || core.runtime.workflowManager == nil || core.runtime.workflows == nil || core.runtime.workflowParent == nil {
		return nil
	}
	parent := core.runtime.workflowParent
	parent.mu.Lock()
	turn := workflows.ParentTurn(parent.runCount)
	parent.mu.Unlock()
	_, err := core.runtime.workflows.Views().DeliverNotifications(ctx, workflows.SessionID(core.id), turn, workflowParentAppender{core, parent, turn})
	return err
}

type workflowParentAppender struct {
	core   *Session
	parent *ManagedSession
	turn   workflows.ParentTurn
}

func (appender workflowParentAppender) AppendWorkflowNotifications(ctx context.Context, batch workflows.NotificationAppend) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	core, parent := appender.core, appender.parent
	if batch.SessionID != workflows.SessionID(core.id) || batch.ParentTurn != appender.turn || parent.ID() != core.id || parent.Owner() != core.owner {
		return errors.New("workflow notification does not match the bound parent turn")
	}
	manager := core.runtime.workflowManager
	current, err := manager.Get(core.owner, core.id)
	if err != nil {
		return err
	}
	if current != parent || manager.State() != ManagerActive {
		return ErrManagerStopped
	}
	message := protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(batch.Content)}
	if err := message.Validate(); err != nil {
		return err
	}
	core.appendMessages(message)
	return nil
}
