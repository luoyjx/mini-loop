package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/workflows"
)

func normalizeManagerWorkflows(services *ManagerServices) error {
	services.WorkflowCaps = clonePointer(services.WorkflowCaps)
	if services.WorkflowAttemptPool != nil && !services.WorkflowAttemptPool.Initialized() {
		return errors.New("workflow attempt pool is uninitialized")
	}
	if injected := services.WorkflowService; injected != nil {
		if services.WorkflowAttemptPool != nil && services.WorkflowAttemptPool != injected.config.AttemptPool {
			return errors.New("workflow_attempt_semaphore conflicts with the injected WorkflowService")
		}
		services.WorkflowTools = true
		services.WorkflowAttemptPool = injected.config.AttemptPool
		services.ActionJournal = injected.config.Journal
		return nil
	}
	if !services.WorkflowTools {
		return nil
	}
	caps := workflows.DefaultDefinitionCaps()
	if services.WorkflowCaps != nil {
		caps = *services.WorkflowCaps
	}
	if _, err := workflows.NewDefinitionAdmission(caps); err != nil {
		return err
	}
	if !validWorkflowWallTime(caps.WallTimeSeconds) {
		return errors.New("workflow wall-time policy exceeds Go duration range")
	}
	services.WorkflowCaps = &caps
	return nil
}

func (manager *SessionManager) initializeWorkflows() error {
	services := manager.config.Services
	manager.workflows = services.WorkflowService
	if !services.WorkflowTools || manager.workflows != nil {
		return nil
	}
	worker := WorkflowRunnerConfig{
		Provider: services.Provider, Model: manager.config.Defaults.Model,
		MaxTokens: manager.config.Defaults.MaxTokens, TokenThreshold: manager.config.Defaults.TokenThreshold,
		Skills: services.Skills, Secrets: services.Secrets, RolePolicy: services.RoleToolPolicy,
		Recovery: services.Recovery, CachePolicy: services.CachePolicy, StuckDetector: services.StuckDetector,
		ModelLimiter: services.ModelLimiter, ToolLimiter: services.ToolLimiter,
	}
	var err error
	manager.workflows, err = NewWorkflowService(WorkflowServiceConfig{
		Caps: *services.WorkflowCaps, Journal: services.ActionJournal,
		ResolveParent: manager.workflowParent, AttemptPool: services.WorkflowAttemptPool, Worker: worker,
	})
	if err == nil {
		manager.config.Services.WorkflowAttemptPool = manager.workflows.config.AttemptPool
	}
	return err
}

// Workflows is an operator seam; it does not establish caller owner admission.
func (manager *SessionManager) Workflows() *WorkflowService { return manager.workflows }

func (manager *SessionManager) workflowParent(id SessionID) (WorkflowParent, bool) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	session := manager.sessions[id]
	if manager.state != ManagerActive || session == nil {
		return WorkflowParent{}, false
	}
	return WorkflowParent{Owner: session.Owner(), Workspace: session.core.workspace}, true
}

// The background deletion owns this wait even when the initiating caller leaves.
// CancelSession's admission barrier accounts for launches already publishing a task.
func (manager *SessionManager) beginWorkflowDeletion(id SessionID) <-chan error {
	if manager.workflows == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- manager.workflows.CancelSession(context.Background(), workflows.SessionID(id)) }()
	return done
}

func (manager *SessionManager) finishWorkflowDeletion(session *ManagedSession, done <-chan error, reclaim bool) bool {
	if done == nil {
		return true
	}
	err := <-done
	id := session.ID()
	if err == nil && (manager.workflows.HasActive(workflows.SessionID(id)) || len(manager.workflows.sessionTasks(workflows.SessionID(id))) != 0) {
		err = errors.New("cancellation completed but workflow remains active")
	}
	if err == nil {
		return true
	}
	manager.recordCleanupError(id, "workflows", err)
	if reclaim {
		manager.mu.Lock()
		if manager.workflowRetentions == nil {
			manager.workflowRetentions = make(map[SessionID]string)
		}
		manager.workflowRetentions[id] = session.core.workspace
		manager.mu.Unlock()
	}
	return false
}

func (manager *SessionManager) retryWorkflowRetentions() {
	if manager.workflows == nil {
		return
	}
	manager.workspaceMu.Lock()
	defer manager.workspaceMu.Unlock()
	manager.mu.Lock()
	retained := make(map[SessionID]string, len(manager.workflowRetentions))
	for id, path := range manager.workflowRetentions {
		retained[id] = path
	}
	manager.mu.Unlock()
	for id, path := range retained {
		if manager.workflows.HasActive(workflows.SessionID(id)) || len(manager.workflows.sessionTasks(workflows.SessionID(id))) != 0 {
			continue
		}
		manager.mu.Lock()
		shared := false
		for _, session := range manager.sessions {
			if session.core.workspace == path {
				shared = true
				break
			}
		}
		if !shared {
			delete(manager.workflowRetentions, id)
		}
		manager.mu.Unlock()
		if !shared {
			manager.reclaimUnusedWorkspace(id, path)
		}
	}
}
