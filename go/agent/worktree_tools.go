package agent

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/tasks"
	"github.com/luoyjx/mini-loop/go/workspace"
	"github.com/luoyjx/mini-loop/go/worktrees"
)

// WorkspaceBashExecutor exposes the execution root needed to validate an entry.
// A custom factory must return this bound seam; the legacy unbound BashExecutor
// remains valid for ordinary sessions, but cannot prove a workspace switch.
type WorkspaceBashExecutor interface {
	BashExecutor
	Workspace() string
}

// The lifecycle workspace stays immutable for Info, task HTTP reads, trajectory
// attribution and scratch reclamation. Only the serialized execution path moves.
func (s *Session) executionRoot() string {
	if s.executionWorkspace != "" {
		return s.executionWorkspace
	}
	return s.workspace
}

func (h *runtimeHandler) executeWorktree(ctx context.Context, input protocol.ToolInput) (string, error) {
	if h.worktrees == nil {
		return "Error: worktree repository is not configured", nil
	}
	switch input.Name() {
	case protocol.ToolCreateWorktree:
		v, _ := input.CreateWorktree()
		// Source initializes the board even for an unbound creation; once built,
		// that board stays pinned when the execution workspace later changes.
		if h.taskStore == nil {
			var err error
			h.taskStore, err = tasks.New(tasks.Config{Workspace: h.binding.Workspace, Secrets: h.session.secrets})
			if err != nil {
				return "", err
			}
		}
		id := tasks.ID("")
		if v.TaskID != nil {
			id = tasks.ID(*v.TaskID)
		}
		return h.worktrees.Create(ctx, worktrees.Name(v.Name), id, h.taskStore)
	case protocol.ToolRemoveWorktree:
		v, _ := input.RemoveWorktree()
		discard := v.DiscardChanges != nil && *v.DiscardChanges
		return h.worktrees.Remove(ctx, worktrees.Name(v.Name), discard)
	case protocol.ToolKeepWorktree:
		v, _ := input.WorktreeName()
		return h.worktrees.Keep(ctx, worktrees.Name(v.Name))
	case protocol.ToolListWorktrees:
		return h.worktrees.List(ctx), ctx.Err()
	case protocol.ToolEnterWorktree:
		v, _ := input.WorktreeName()
		path, err := h.worktrees.PathFor(worktrees.Name(v.Name))
		if err != nil {
			return "", err
		}
		if _, err = os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return "Error: no worktree " + v.Name, nil
		} else if err != nil {
			return "", err
		}
		if err = h.enterWorkspace(ctx, path); err != nil {
			return "", err
		}
		return fmt.Sprintf("Entered worktree '%s' at %s", v.Name, path), nil
	}
	return "", errors.New("unsupported worktree input")
}

func rebindApproval(surface *ApprovalSurface, root string) (*ApprovalSurface, error) {
	binding := surface.binding
	binding.Workspace = root
	result, err := surface.broker.ForSession(binding, surface.sink)
	if err == nil {
		result.redactor = surface.redactor
	}
	return result, err
}

// Called only by the enter_worktree exclusive barrier, with the turn lock and
// handler lock held. Prepare every dependency before publishing any new binding.
func (h *runtimeHandler) enterWorkspace(ctx context.Context, path string) error {
	s := h.session
	if s == nil {
		return errors.New("workspace switch has no bound session")
	}
	files, err := workspace.NewFiles(path)
	if err != nil {
		return err
	}
	var executor BashExecutor
	if current, ok := s.bash.(*shell.Executor); ok {
		executor, err = current.WithWorkspace(files.Root())
	} else if h.workspaceBashFactory != nil {
		executor, err = h.workspaceBashFactory.BashFor(ctx, SessionBinding{s.id, s.owner, files.Root(), s.permissionMode()})
	} else {
		return errors.New("workspace switch requires a rebindable Bash executor or explicit workspace factory")
	}
	if err != nil {
		return err
	}
	if executor == nil {
		return errors.New("workspace Bash factory returned nil")
	}
	bound, ok := executor.(WorkspaceBashExecutor)
	if !ok {
		return errors.New("workspace Bash factory must return a workspace-bound executor")
	}
	root, err := workspace.ResolvePath(bound.Workspace())
	if err != nil || root != files.Root() {
		return errors.New("rebound Bash executor has a different workspace")
	}
	definitions := append([]ToolDefinition(nil), s.gate.catalog.ordered...)
	for i := range definitions {
		definition := &definitions[i]
		switch previous := definition.handler.(type) {
		case workspaceFileHandler:
			definition.handler = workspaceFileHandler{files, previous.name}
		case bashHandler:
			definition.handler = bashHandler{executor}
		}
		if _, bound := definition.verifier.(workspaceWriteVerifier); bound {
			definition.verifier = workspaceWriteVerifier{files}
		}
	}
	catalog, err := NewToolCatalog(definitions...)
	if err != nil {
		return err
	}
	approver := s.gate.policy.approver
	questions := h.questions
	questionRebound := false
	if current, ok := approver.(*ApprovalSurface); ok {
		next, err := rebindApproval(current, files.Root())
		if err != nil {
			return err
		}
		approver = next
		if questions == current {
			questions = next
			questionRebound = true
		}
	}
	if current, ok := questions.(*ApprovalSurface); ok && !questionRebound {
		next, err := rebindApproval(current, files.Root())
		if err != nil {
			return err
		}
		questions = next
	}
	policy, err := NewPermissionPolicy(s.gate.policy.rules, approver, s.gate.policy.denyCommands)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.background != nil {
		native, ok := executor.(*shell.Executor)
		if !ok {
			return errors.New("background workspace switch requires a native shell executor")
		}
		if err := s.background.rebind(ctx, native); err != nil {
			return err
		}
	}
	// Catalogue snapshots remain immutable. This gate keeps its hooks, journal,
	// diagnostics and live mode; later calls in this same batch use the new root.
	s.gate.catalog, s.gate.policy = catalog, policy
	s.executionWorkspace, s.files, s.bash = files.Root(), files, executor
	h.binding.Workspace = files.Root()
	h.questions, s.questions = questions, questions
	return nil
}
