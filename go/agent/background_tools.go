package agent

import (
	"context"
	"errors"
	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// backgroundState is owned by one session. No injected/shared manager can borrow
// another session's task IDs, credential scope or completion queue.
type backgroundState struct {
	mu       sync.Mutex
	executor *shell.Executor
	manager  *background.Manager
}

func (state *backgroundState) get(ctx context.Context, orphanOnly bool) (*background.Manager, error) {
	if state == nil {
		return nil, errors.New("background service is not configured")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if state.manager != nil {
		return state.manager, nil
	}
	if orphanOnly {
		entries, err := os.ReadDir(filepath.Join(state.executor.Workspace(), ".background"))
		if err != nil || len(entries) == 0 {
			return nil, nil
		}
	}
	manager, err := background.NewWithExecutor(state.executor)
	if err != nil {
		return nil, err
	}
	state.manager = manager
	return manager, nil
}
func (state *backgroundState) rebind(ctx context.Context, executor *shell.Executor) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.manager != nil {
		if err := state.manager.RebindExecutor(ctx, executor); err != nil {
			return err
		}
	}
	state.executor = executor
	return nil
}
func (s *Session) backgroundLive() int {
	if s.background == nil {
		return 0
	}
	s.background.mu.Lock()
	manager := s.background.manager
	s.background.mu.Unlock()
	if manager == nil {
		return 0
	}
	return manager.LiveCount()
}

// CloseBackground joins already-created task ownership. The embedding caller
// must first quiesce turn admission; this method does not close the session.
func (s *Session) CloseBackground(ctx context.Context) error {
	if s.background == nil {
		return nil
	}
	s.background.mu.Lock()
	manager := s.background.manager
	s.background.mu.Unlock()
	if manager == nil {
		return nil
	}
	return manager.Close(ctx)
}
func (s *ManagedSession) CloseBackground(ctx context.Context) error {
	return s.core.CloseBackground(ctx)
}
func (handler *runtimeHandler) executeBackground(ctx context.Context, input protocol.ToolInput) (string, error) {
	manager, err := handler.background.get(ctx, false)
	if err != nil {
		return "", err
	}
	if value, ok := input.CheckBackground(); ok {
		id := background.ID("")
		if value.ID != nil {
			id = background.ID(*value.ID)
		}
		return manager.Check(id), nil
	}
	value, ok := input.BackgroundRun()
	if !ok {
		return "", errors.New("unsupported background input")
	}
	var timeout *time.Duration
	if value.Timeout != nil {
		const maxSeconds = int64(1<<63-1) / int64(time.Second)
		seconds := int64(*value.Timeout)
		if seconds > maxSeconds || seconds < -maxSeconds {
			return "", errors.New("background timeout is outside the native duration range")
		}
		duration := time.Duration(seconds) * time.Second
		timeout = &duration
	}
	started, err := manager.Run(ctx, background.Request{Command: value.Command, Timeout: timeout})
	if err != nil {
		return "", err
	}
	return started.Render(), nil
}
func (handler *runtimeHandler) executeDetailedTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, *shell.Result, error) {
	if input.Name() != protocol.ToolBash {
		out, err := handler.ExecuteTool(ctx, authority, input)
		return out, nil, err
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if authority.SessionID != handler.binding.SessionID || authority.OwnerID != handler.binding.OwnerID || authority.Workspace != handler.binding.Workspace {
		return "", nil, errors.New("background Bash authority does not match bound session")
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if handler.session == nil {
		return "", nil, errors.New("background Bash has no bound session")
	}
	value, _ := input.Bash()
	explicit := value.RunInBackground != nil && *value.RunInBackground
	if handler.background != nil && background.ShouldRunBackground(value.Command, explicit) {
		manager, err := handler.background.get(ctx, false)
		if err != nil {
			return "", nil, err
		}
		started, err := manager.Run(ctx, background.Request{Command: value.Command})
		if err != nil {
			return "", nil, err
		}
		return started.Render(), nil, nil
	}
	return (bashHandler{handler.session.bash}).executeDetailedTool(ctx, authority, input)
}

type backgroundBashClassifier struct{}

func (backgroundBashClassifier) ClassifyExecution(call ToolCall) (ExecutionMode, error) {
	value, ok := call.Input.Bash()
	if ok && value.RunInBackground != nil && *value.RunInBackground {
		return ExecutionParallel, nil
	}
	return ExecutionExclusive, nil
}

const EventBackgroundResult SessionEventKind = "background_result"

type BackgroundResultEvent struct {
	Count   int `json:"count"`
	Dropped int `json:"dropped"`
}

func (event SessionEvent) BackgroundResult() (BackgroundResultEvent, bool) {
	return event.backgroundResult, event.kind == EventBackgroundResult
}
func (s *Session) injectBackground(ctx context.Context) error {
	if s.background == nil {
		return nil
	}
	manager, err := s.background.get(ctx, true)
	if err != nil {
		return err
	}
	if manager == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	batch := manager.DrainBatch()
	if len(batch.Notifications) == 0 {
		return nil
	}
	s.events.append(SessionEvent{kind: EventBackgroundResult, backgroundResult: BackgroundResultEvent{len(batch.Notifications), batch.Dropped}})
	s.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(batch.Render())})
	return nil
}
