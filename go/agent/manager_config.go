package agent

import (
	"context"
	"errors"
	"time"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/spill"
	"github.com/luoyjx/mini-loop/go/userresources"
	"github.com/luoyjx/mini-loop/go/worktrees"
)

const (
	DefaultModelConcurrency ConcurrencyLimit = 8
	DefaultSessionMaxRounds                  = 50
	MaxRememberedOwners                      = 10000
	MaxCleanupErrors                         = 100
	DefaultShutdownGrace                     = 250 * time.Millisecond
	DefaultDeleteGrace                       = 5 * time.Second
)

// Identity/workspace are stamped by the manager, not accepted in a template.
type SessionDefaults struct {
	Model                                string
	PermissionMode                       PermissionMode
	MaxRounds, MaxTokens, TokenThreshold int
	SubagentMaxDepth, SubagentMaxRounds  int
	System                               *string
}

type SessionBinding struct {
	ID        SessionID
	Owner     OwnerID
	Workspace string
	Mode      PermissionMode
}

type WorkspaceFactory interface {
	WorkspaceFor(context.Context, SessionID) (string, error)
}
type BashFactory interface {
	BashFor(context.Context, SessionBinding) (BashExecutor, error)
}

// Services must be concurrency-safe when shared across the fleet. Factories
// may inspect the manager, but cannot recursively create/delete/stop it.
type ManagerServices struct {
	UserResources             *userresources.Resolver
	DecisionTools             bool
	DecisionProvider          decisions.Provider
	DecisionLLM               DecisionLLMConfig
	GoalTools                 bool
	PlanModeTools             bool
	PlanApprover              PlanApprover
	StateStore                StateStore
	CronTools                 bool
	BackgroundTools           bool
	WorktreeTools             bool
	Worktrees                 *worktrees.Manager
	TaskTools                 bool
	Trajectories              TrajectoryStore
	Build                     string
	Spill                     spill.Store
	Provider                  Provider
	Recovery                  Recovery
	StreamProgress            StreamProgressConfig
	BashFactory               BashFactory
	Skills                    SkillSource
	Approvals                 *ApprovalBroker
	ActionJournal             ActionJournal
	Secrets                   ApprovalRedactor
	Hooks                     GateHooks
	SystemBuilder             SystemBuilder
	Compactor                 Compactor
	Subagents                 SubagentProvider
	RoleToolPolicy            RoleToolPolicy
	CachePolicy               CachePolicy
	StuckDetector             StuckDetector
	StopHooks                 []StopHook
	UserPromptHooks           []UserPromptHook
	Injectors                 []MessageInjector
	EventSink                 EventSink
	ModelLimiter, ToolLimiter *ConcurrencyLimiter
}

type ManagerConfig struct {
	StateLeaseTTL                     time.Duration
	WorkspaceRoot                     string
	BindableRoots                     []string
	WorkspaceFactory                  WorkspaceFactory
	Defaults                          SessionDefaults
	Services                          ManagerServices
	ModelConcurrency, ToolConcurrency ConcurrencyLimit
	ApprovalTimeout                   time.Duration
	ShutdownGrace, DeleteGrace        time.Duration
}

type CreateSessionRequest struct {
	Owner          OwnerID
	Model          *string
	PermissionMode PermissionMode
	System         *string
	Workspace      *string
}

var ErrManagerStopped = errors.New("session manager is stopped")
var ErrSessionNotFound = errors.New("session not found")

type WorkspaceBindingStatus int

const (
	BindingForbidden WorkspaceBindingStatus = 403
	BindingInvalid   WorkspaceBindingStatus = 400
)

type WorkspaceBindingError struct {
	Status WorkspaceBindingStatus
	Detail string
}

func (err *WorkspaceBindingError) Error() string { return err.Detail }

// PreserveWorkspace retains manager-owned scratch. Bound workspaces are always retained.
type DeleteSessionOptions struct {
	PreserveWorkspace  bool
	RemoveTrajectories bool
}

type CleanupError struct {
	SessionID SessionID `json:"session_id"`
	Workspace string    `json:"workspace"`
	Error     string    `json:"error"`
}

type hostBashFactory struct{}

func (hostBashFactory) BashFor(ctx context.Context, binding SessionBinding) (BashExecutor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return shell.New(shell.Config{Workspace: binding.Workspace})
}
