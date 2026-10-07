package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/cron"
	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/teams"
	"github.com/luoyjx/mini-loop/go/userresources"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type ManagerState string

const (
	ManagerActive   ManagerState = "active"
	ManagerStopping ManagerState = "stopping"
	ManagerStopped  ManagerState = "stopped"
)

// SessionManager owns process-local session handles, shared services, cron and
// scratch reclamation. Cron files and trajectory evidence do not contain session
// transcripts or leases; state is restored through the injected store consumer.
// Public lookups require an already established owner identity.
type SessionManager struct {
	teamReservations               map[teams.Identity]bool
	teamProtocols                  *teams.Coordinator
	teams                          *teams.Bus
	improvements                   *improvement.Archive
	skillDrafts                    *userresources.DraftStore
	restoreLifetime                context.Context
	restoreCancel                  context.CancelFunc
	restoreTurn                    chan struct{}
	leaseOwner                     LeaseOwner
	mu                             sync.Mutex
	cronMu                         sync.Mutex
	cron                           *cron.Scheduler
	workspaceMu                    sync.Mutex
	config                         ManagerConfig
	state                          ManagerState
	sessions                       map[SessionID]*ManagedSession
	retiring                       map[SessionID]*ManagedSession
	order                          []SessionID
	reservations                   map[SessionID]bool
	owners                         map[SessionID]OwnerID
	ownerOrder                     []SessionID
	creating, cleaning             int
	createsDrained, cleanupDrained chan struct{}
	stopped                        chan struct{}
	cleanupErrors                  []CleanupError
}

func closedSignal() chan struct{} { signal := make(chan struct{}); close(signal); return signal }

func NewSessionManager(config ManagerConfig) (*SessionManager, error) {
	if !config.Services.SelfAuditView.valid() {
		return nil, errors.New("invalid self-audit visibility")
	}
	if config.Services.Provider == nil {
		return nil, errors.New("session manager requires a provider")
	}
	if config.TeamIdlePoll < 0 || config.TeamIdleTimeout < 0 {
		return nil, errors.New("team idle durations cannot be negative")
	}
	if config.TeamIdlePoll == 0 {
		config.TeamIdlePoll = time.Second
	}
	if config.TeamIdleTimeout == 0 {
		config.TeamIdleTimeout = time.Minute
	}
	if config.Services.StateStore == nil && config.StateLeaseTTL != 0 {
		return nil, errors.New("state lease TTL requires a state store")
	}
	defaults := &config.Defaults
	if defaults.PermissionMode == "" {
		defaults.PermissionMode = ModeInteractive
	}
	if !defaults.PermissionMode.Valid() || defaults.MaxRounds < 0 || defaults.MaxTokens < 0 || defaults.TokenThreshold < 0 || defaults.SubagentMaxDepth < 0 || defaults.SubagentMaxRounds < 0 {
		return nil, errors.New("invalid session defaults")
	}
	if defaults.Model == "" {
		defaults.Model = DefaultModel
	}
	if defaults.MaxRounds == 0 {
		defaults.MaxRounds = DefaultSessionMaxRounds
	}
	defaults.System = clonePointer(defaults.System)
	config.Services.MemoryAuto = clonePointer(config.Services.MemoryAuto)
	if config.ModelConcurrency < 0 || config.ToolConcurrency < 0 || config.ApprovalTimeout < 0 || config.ShutdownGrace < 0 || config.DeleteGrace < 0 || config.StateLeaseTTL < 0 {
		return nil, errors.New("manager limits cannot be negative")
	}
	if config.ShutdownGrace == 0 {
		config.ShutdownGrace = DefaultShutdownGrace
	}
	if config.DeleteGrace == 0 {
		config.DeleteGrace = DefaultDeleteGrace
	}
	services := &config.Services
	decisionLLM, err := services.DecisionLLM.normalized()
	if err != nil {
		return nil, err
	}
	services.DecisionLLM = decisionLLM
	services.StreamProgress = services.StreamProgress.clone()
	for _, pool := range []*ConcurrencyLimiter{services.ModelLimiter, services.ToolLimiter} {
		if pool != nil && pool.slots == nil {
			return nil, errors.New("uninitialized concurrency limiter")
		}
	}
	for _, hook := range services.UserPromptHooks {
		if hook == nil {
			return nil, errors.New("user prompt hook cannot be nil")
		}
	}
	for _, injector := range services.Injectors {
		if injector == nil {
			return nil, errors.New("message injector cannot be nil")
		}
	}
	if config.ModelConcurrency == 0 {
		config.ModelConcurrency = DefaultModelConcurrency
	}
	if config.ToolConcurrency == 0 {
		config.ToolConcurrency = DefaultToolConcurrency
	}
	if services.ModelLimiter == nil {
		services.ModelLimiter, _ = NewConcurrencyLimiter(config.ModelConcurrency)
	}
	if services.ToolLimiter == nil {
		services.ToolLimiter, _ = NewConcurrencyLimiter(config.ToolConcurrency)
	}
	if services.Approvals == nil {
		services.Approvals, _ = NewApprovalBroker(ApprovalBrokerConfig{Timeout: config.ApprovalTimeout, Redactor: services.Secrets, Store: services.StateStore})
	}
	if services.ActionJournal == nil {
		if services.StateStore != nil {
			journal, err := NewStoredActionJournal(services.StateStore)
			if err != nil {
				return nil, err
			}
			// Explicit manager recovery policy, never an implicit store-open effect.
			if err := stateFault(func() error { _, err := journal.MarkInflightUnknown(context.Background(), nil); return err }); err != nil {
				return nil, err
			}
			services.ActionJournal = journal
		} else {
			services.ActionJournal, _ = NewInMemoryActionJournal(DefaultResultsRetained)
		}
	}
	if services.BashFactory == nil {
		services.BashFactory = hostBashFactory{}
	}
	if services.Skills == nil {
		services.Skills = skills.EmptyCatalog()
	}
	services.Hooks = GateHooks{Before: append([]BeforeHook(nil), services.Hooks.Before...), Guards: append([]GuardHook(nil), services.Hooks.Guards...), After: append([]AfterHook(nil), services.Hooks.After...), Observers: append([]ResultObserver(nil), services.Hooks.Observers...)}
	if services.StopHooks != nil {
		services.StopHooks = append([]StopHook{}, services.StopHooks...)
	}
	services.UserPromptHooks = append([]UserPromptHook(nil), services.UserPromptHooks...)
	services.Injectors = append([]MessageInjector(nil), services.Injectors...)
	if config.WorkspaceRoot == "" {
		config.WorkspaceRoot = "./workspaces"
	}
	root, err := expandedWorkspacePath(config.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	config.WorkspaceRoot = root
	roots := make([]string, 0, len(config.BindableRoots))
	for _, path := range config.BindableRoots {
		resolved, err := expandedWorkspacePath(path)
		if err != nil {
			return nil, err
		}
		roots = append(roots, resolved)
	}
	config.BindableRoots = roots
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if services.Memory == nil {
		services.Memory, err = memory.NewStore(context.Background(), filepath.Join(root, ".memory"), services.Secrets)
		if err != nil {
			return nil, err
		}
	}
	manager := &SessionManager{teamReservations: make(map[teams.Identity]bool), restoreTurn: make(chan struct{}, 1), config: config, state: ManagerActive, sessions: make(map[SessionID]*ManagedSession), retiring: make(map[SessionID]*ManagedSession), reservations: make(map[SessionID]bool), owners: make(map[SessionID]OwnerID), createsDrained: closedSignal(), cleanupDrained: closedSignal(), stopped: make(chan struct{})}
	teamRoot := filepath.Join(root, ".teams")
	manager.teams = teams.New(teams.Config{Root: &teamRoot, Masker: services.Secrets})
	manager.teamProtocols, err = teams.NewCoordinator(teams.CoordinatorConfig{Bus: manager.teams, Members: managerTeamDirectory{manager}})
	if err != nil {
		return nil, err
	}
	manager.improvements = improvement.NewArchive(filepath.Join(root, ".improvements"), services.Secrets)
	manager.skillDrafts, err = userresources.NewDraftStore(userresources.DefaultDraftStoreConfig())
	if err != nil {
		return nil, err
	}
	manager.restoreLifetime, manager.restoreCancel = context.WithCancel(context.Background())
	manager.restoreTurn <- struct{}{}
	if services.StateStore != nil {
		name, err := newSpan("process_", 16)
		if err != nil {
			return nil, err
		}
		manager.leaseOwner = LeaseOwner(fmt.Sprintf("%d_%s", os.Getpid(), name))
	}
	manager.cron, err = cron.New(cron.Config{Resolver: managerCronResolver{manager}, DurablePath: filepath.Join(root, ".cron.json"), Secrets: cronMasker{services.Secrets}})
	if err != nil {
		return nil, err
	}
	return manager, nil
}

func (manager *SessionManager) State() ManagerState {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.state
}
func (manager *SessionManager) WorkspaceRoot() string { return manager.config.WorkspaceRoot }
func (manager *SessionManager) WorkspaceBindingEnabled() bool {
	return len(manager.config.BindableRoots) > 0
}
func (manager *SessionManager) Approvals() *ApprovalBroker { return manager.config.Services.Approvals }

// RecordingMasker shares the configured projection boundary with embedding services.
func (manager *SessionManager) RecordingMasker() TextMasker { return manager.config.Services.Secrets }

type ManagerSummary struct {
	Model                             string
	ModelConcurrency, ToolConcurrency ConcurrencyLimit
	Sessions                          int
	WorkspaceBinding                  bool
}

func (manager *SessionManager) Summary() ManagerSummary {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return ManagerSummary{manager.config.Defaults.Model, ConcurrencyLimit(cap(manager.config.Services.ModelLimiter.slots)), ConcurrencyLimit(cap(manager.config.Services.ToolLimiter.slots)), len(manager.sessions), len(manager.config.BindableRoots) > 0}
}

// ListRecent bounds expensive Info projections before evaluating session state.
func (manager *SessionManager) ListRecent(owner OwnerID, limit int) []SessionInfo {
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	manager.mu.Lock()
	handles := make([]*ManagedSession, 0, limit)
	for i := len(manager.order) - 1; i >= 0 && len(handles) < limit; i-- {
		s := manager.sessions[manager.order[i]]
		if s != nil && owner != "" && s.Owner() == owner {
			handles = append(handles, s)
		}
	}
	manager.mu.Unlock()
	result := make([]SessionInfo, 0, len(handles))
	for _, s := range handles {
		result = append(result, s.Info())
	}
	return result
}

func (manager *SessionManager) reserveID() (SessionID, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.state != ManagerActive {
		return "", ErrManagerStopped
	}
	for attempt := 0; attempt < 8; attempt++ {
		bytes := make([]byte, 6)
		if _, err := rand.Read(bytes); err != nil {
			return "", err
		}
		id := SessionID(hex.EncodeToString(bytes))
		if manager.sessions[id] != nil || manager.reservations[id] {
			continue
		}
		manager.reservations[id] = true
		if manager.creating == 0 {
			manager.createsDrained = make(chan struct{})
		}
		manager.creating++
		return id, nil
	}
	return "", errors.New("session identity collision budget exhausted")
}
func (manager *SessionManager) finishCreate(id SessionID) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	delete(manager.reservations, id)
	manager.creating--
	if manager.creating == 0 {
		close(manager.createsDrained)
	}
}

func (manager *SessionManager) Create(ctx context.Context, request CreateSessionRequest) (session *ManagedSession, err error) {
	return manager.create(ctx, request, nil)
}

// seed is installed before the handle becomes visible to Get/List or shutdown.
func (manager *SessionManager) create(ctx context.Context, request CreateSessionRequest, seed *forkSnapshot) (session *ManagedSession, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if request.Owner == "" {
		return nil, errors.New("owner must be a non-empty string")
	}
	defaults := manager.config.Defaults
	mode := request.PermissionMode
	if mode == "" {
		mode = defaults.PermissionMode
	}
	if !mode.Valid() {
		return nil, fmt.Errorf("unknown permission mode %q", mode)
	}
	id, err := manager.reserveID()
	if err != nil {
		return nil, err
	}
	defer manager.finishCreate(id)
	manager.workspaceMu.Lock()
	defer manager.workspaceMu.Unlock()
	path := ""
	scratch := request.Workspace == nil
	published := false
	var pending *ManagedSession
	allocated := false
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("session construction panicked (%T)", fault)
			session = nil
		}
		if !published && pending != nil {
			manager.recordCleanupError(id, "state", pending.core.persistence.delete())
		}
		if !published && scratch && allocated && path != "" {
			manager.reclaimUnusedWorkspace(id, path)
		}
	}()
	if !scratch {
		path, err = manager.bindWorkspace(*request.Workspace)
	} else {
		path = filepath.Join(manager.config.WorkspaceRoot, string(id))
		if factory := manager.config.WorkspaceFactory; factory != nil {
			path, err = factory.WorkspaceFor(ctx, id)
		}
		if err == nil && path == "" {
			err = errors.New("workspace factory returned an empty path")
		}
		if err == nil {
			path, err = workspace.ResolvePath(path)
		}
		if err == nil {
			err = os.MkdirAll(path, 0700)
			allocated = err == nil
		}
	}
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	services := manager.config.Services
	bash, err := services.BashFactory.BashFor(ctx, SessionBinding{id, request.Owner, path, mode})
	if err != nil {
		return nil, err
	}
	model := defaults.Model
	if request.Model != nil {
		model = *request.Model
		if model == "" {
			return nil, errors.New("model cannot be empty")
		}
	}
	system := defaults.System
	if request.System != nil {
		system = request.System
	}
	runtime, err := manager.managedRuntimeConfig(ctx, id, request.Owner, path, mode, model, system, bash)
	if err != nil {
		return nil, err
	}
	runtime.ToolSelection = request.ToolSelection
	session, err = newManagedSession(runtime, true)
	if err != nil {
		return nil, err
	}
	session.workspaceBound = !scratch
	pending = session
	session.core.explicitSystem = clonePointer(system)
	if seed != nil {
		session.core.messages = seed.messages
		session.core.forkedFrom = clonePointer(&seed.lineage)
		session.core.publishLive()
	}
	if err = session.core.persistence.initialize(); err != nil {
		return nil, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.state != ManagerActive {
		session.StopAccepting("session manager stopped")
		return nil, ErrManagerStopped
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	manager.sessions[id] = session
	manager.order = append(manager.order, id)
	published = true
	return session, nil
}

// One composition map serves both new and restored handles. Metadata and history
// are installed before initialization/publication by the respective consumer.
func (manager *SessionManager) baseManagedRuntimeConfig(id SessionID, owner OwnerID, path string, mode PermissionMode, model string, system *string, bash BashExecutor) RuntimeConfig {
	services := manager.config.Services
	defaults := manager.config.Defaults
	builder := services.SystemBuilder
	if system != nil {
		builder = FixedSystem(*system)
	}
	runtime := RuntimeConfig{DecisionTools: services.DecisionTools, DecisionProvider: services.DecisionProvider, DecisionLLM: services.DecisionLLM, GoalTools: services.GoalTools, PlanModeTools: services.PlanModeTools, PlanApprover: services.PlanApprover, StateStore: services.StateStore, StateLeaseOwner: manager.leaseOwner, StateLeaseTTL: manager.config.StateLeaseTTL, CronTools: services.CronTools, Cron: manager, BackgroundTools: services.BackgroundTools, WorktreeTools: services.WorktreeTools, Worktrees: services.Worktrees, WorkspaceBashFactory: services.BashFactory, TaskTools: services.TaskTools, Trajectories: services.Trajectories, Build: services.Build, ID: id, Owner: owner, Provider: services.Provider, Recovery: services.Recovery, Spill: services.Spill, StreamProgress: services.StreamProgress, Bash: bash, Workspace: path, Mode: mode, MaxRounds: defaults.MaxRounds, Skills: services.Skills, Approvals: services.Approvals, ActionJournal: services.ActionJournal, Secrets: services.Secrets, Hooks: services.Hooks, Model: model, MaxTokens: defaults.MaxTokens, TokenThreshold: defaults.TokenThreshold, SubagentMaxDepth: defaults.SubagentMaxDepth, SubagentMaxRounds: defaults.SubagentMaxRounds, SystemBuilder: builder, Compactor: services.Compactor, Subagents: services.Subagents, RoleToolPolicy: services.RoleToolPolicy, CachePolicy: services.CachePolicy, StuckDetector: services.StuckDetector, StopHooks: services.StopHooks, UserPromptHooks: services.UserPromptHooks, Injectors: services.Injectors, EventSink: services.EventSink, ModelLimiter: services.ModelLimiter, ToolLimiter: services.ToolLimiter}
	runtime.team = &teams.Identity{Team: teams.TeamID(id), Name: "lead"}
	runtime.TeamTools = services.TeamTools
	runtime.teamManager = manager
	runtime.SelfAuditTools = services.SelfAuditTools
	runtime.SelfAuditObserver = manager
	runtime.SelfAuditView = services.SelfAuditView
	runtime.skillDrafts = manager.skillDrafts
	runtime.MemoryTools = services.MemoryTools
	runtime.MemoryAuto = services.MemoryAuto
	return runtime
}

func (manager *SessionManager) managedRuntimeConfig(ctx context.Context, id SessionID, owner OwnerID, path string, mode PermissionMode, model string, system *string, bash BashExecutor) (RuntimeConfig, error) {
	runtime := manager.baseManagedRuntimeConfig(id, owner, path, mode, model, system, bash)
	services := manager.config.Services
	if resolver := services.UserResources; resolver != nil {
		resources, err := resolver.ForOwner(ctx, userresources.OwnerID(owner))
		if err != nil {
			return RuntimeConfig{}, err
		}
		runtime.UserResources = &resources
	} else if store := services.Memory; store != nil {
		bound, err := memory.Bind(store, memory.OwnerID(owner))
		if err != nil {
			return RuntimeConfig{}, err
		}
		runtime.Memory = bound
	}
	return runtime, nil
}

func (manager *SessionManager) Get(owner OwnerID, id SessionID) (*ManagedSession, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	session := manager.sessions[id]
	if owner == "" || session == nil || session.Owner() != owner {
		return nil, ErrSessionNotFound
	}
	return session, nil
}
func (manager *SessionManager) List(owner OwnerID) []SessionInfo {
	manager.mu.Lock()
	sessions := make([]*ManagedSession, 0)
	for _, id := range manager.order {
		session := manager.sessions[id]
		if session != nil && owner != "" && session.Owner() == owner {
			sessions = append(sessions, session)
		}
	}
	manager.mu.Unlock()
	result := make([]SessionInfo, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, session.Info())
	}
	return result
}
func (manager *SessionManager) RememberedOwners() map[SessionID]OwnerID {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	snapshot := make(map[SessionID]OwnerID, len(manager.owners))
	for id, owner := range manager.owners {
		snapshot[id] = owner
	}
	return snapshot
}
func (manager *SessionManager) rememberOwnerLocked(session *ManagedSession) {
	id := session.ID()
	manager.owners[id] = session.Owner()
	manager.ownerOrder = append(manager.ownerOrder, id)
	if len(manager.ownerOrder) > MaxRememberedOwners {
		delete(manager.owners, manager.ownerOrder[0])
		manager.ownerOrder = manager.ownerOrder[1:]
	}
}
func (manager *SessionManager) Cancel(ctx context.Context, owner OwnerID, id SessionID, reason string) (bool, error) {
	session, err := manager.Get(owner, id)
	if err != nil {
		return false, err
	}
	return session.Cancel(ctx, reason)
}

func (manager *SessionManager) beginCleanupLocked() {
	if manager.cleaning == 0 {
		manager.cleanupDrained = make(chan struct{})
	}
	manager.cleaning++
}
func (manager *SessionManager) finishCleanup(id SessionID) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	delete(manager.retiring, id)
	manager.cleaning--
	if manager.cleaning == 0 {
		close(manager.cleanupDrained)
	}
}
func (manager *SessionManager) Delete(owner OwnerID, id SessionID, options DeleteSessionOptions) (bool, error) {
	manager.cronMu.Lock()
	manager.mu.Lock()
	session := manager.sessions[id]
	if owner == "" || session == nil || session.Owner() != owner {
		manager.mu.Unlock()
		manager.cronMu.Unlock()
		return false, ErrSessionNotFound
	}
	session.StopAccepting("session deleted")
	delete(manager.sessions, id)
	manager.rememberOwnerLocked(session)
	for i, known := range manager.order {
		if known == id {
			manager.order = append(manager.order[:i], manager.order[i+1:]...)
			break
		}
	}
	manager.retiring[id] = session
	manager.beginCleanupLocked()
	manager.mu.Unlock()
	manager.recordCleanupError(id, "state", session.core.persistence.delete())
	if _, err := manager.cron.CancelForSession(cron.SessionID(id)); err != nil {
		manager.recordCleanupError(id, filepath.Join(manager.config.WorkspaceRoot, ".cron.json"), err)
	}
	manager.cronMu.Unlock()
	manager.config.Services.Approvals.CancelSession(id)
	cleanup := func() {
		manager.drainSession(session, "session deleted", manager.config.DeleteGrace)
		if options.RemoveTrajectories && manager.config.Services.Trajectories != nil {
			if err := trajectoryFault(func() error { _, err := manager.config.Services.Trajectories.DeleteForSession(id); return err }); err != nil {
				manager.recordCleanupError(id, "trajectories", err)
			}
		}
		manager.workspaceMu.Lock()
		defer manager.workspaceMu.Unlock()
		// Retire this reference before allowing another cleanup to check for
		// survivors. Otherwise two drained handles can both skip reclamation.
		defer manager.finishCleanup(id)
		if !options.PreserveWorkspace && !session.workspaceBound {
			manager.reclaimUnusedWorkspace(id, session.core.workspace)
		}
	}
	if session.teamRun != nil || session.Info().Busy || session.hasPersonalSkillOperation() || session.core.backgroundInitialized() {
		go cleanup()
	} else {
		cleanup()
	}
	return true, nil
}
func (manager *SessionManager) WaitCleanup(ctx context.Context) error {
	for {
		manager.mu.Lock()
		done := manager.cleanupDrained
		manager.mu.Unlock()
		select {
		case <-done:
			manager.mu.Lock()
			idle := manager.cleaning == 0
			manager.mu.Unlock()
			if idle {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (manager *SessionManager) CleanupErrors() []CleanupError {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return append([]CleanupError(nil), manager.cleanupErrors...)
}
func (manager *SessionManager) recordCleanupError(id SessionID, path string, err error) {
	if err == nil {
		return
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.cleanupErrors = append(manager.cleanupErrors, CleanupError{id, path, truncateRunes(maskedText(manager.config.Services.Secrets, err.Error()), 500)})
	if len(manager.cleanupErrors) > MaxCleanupErrors {
		manager.cleanupErrors = manager.cleanupErrors[len(manager.cleanupErrors)-MaxCleanupErrors:]
	}
}

// Called with workspaceMu held: no concurrent factory/publication can race the
// surviving-reference check with removal. RemoveAll never follows a symlink.
func (manager *SessionManager) reclaimUnusedWorkspace(id SessionID, path string) {
	manager.mu.Lock()
	shared := false
	for _, session := range manager.sessions {
		if session.core.workspace == path {
			shared = true
			break
		}
	}
	for retiringID, session := range manager.retiring {
		if retiringID != id && session.core.workspace == path {
			shared = true
			break
		}
	}
	manager.mu.Unlock()
	if shared {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		manager.recordCleanupError(id, path, err)
	}
}
func (manager *SessionManager) drainSession(session *ManagedSession, reason string, grace time.Duration) {
	// Admission was revoked by Delete/Stop. A turn can still create its lazy
	// service during the grace window, so inspect ownership only after it drains.
	defer func() {
		if err := session.CloseBackground(context.Background()); err != nil {
			manager.recordCleanupError(session.ID(), filepath.Join(session.core.workspace, ".background"), err)
		}
	}()
	defer session.drainPersonalSkillOperation(grace)
	if run := session.teamRun; run != nil {
		run.cancel()
		<-run.done
	}
	session.mu.Lock()
	active := session.active
	session.mu.Unlock()
	if active == nil {
		return
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-active.done:
		return
	case <-timer.C:
	}
	if _, err := session.Cancel(context.Background(), reason); err != nil {
		manager.recordCleanupError(session.ID(), session.core.workspace, err)
	}
	// A finishing turn cannot be cancelled; its synchronous final sink still owns
	// the turn and must drain before workspace reclamation.
	<-active.done
}
func (manager *SessionManager) Stop(ctx context.Context) error {
	manager.cronMu.Lock()
	manager.mu.Lock()
	if manager.state == ManagerActive {
		manager.state = ManagerStopping
		manager.restoreCancel()
		sessions := make([]*ManagedSession, 0, len(manager.order))
		for _, id := range manager.order {
			session := manager.sessions[id]
			session.StopAccepting("session manager stopped")
			sessions = append(sessions, session)
		}
		creating := manager.createsDrained
		go manager.shutdown(sessions, creating)
	}
	done := manager.stopped
	manager.mu.Unlock()
	manager.cronMu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (manager *SessionManager) shutdown(sessions []*ManagedSession, creating <-chan struct{}) {
	manager.config.Services.Approvals.CancelAll()
	<-creating
	var group sync.WaitGroup
	for _, session := range sessions {
		group.Add(1)
		go func() {
			defer group.Done()
			manager.drainSession(session, "session manager stopped", manager.config.ShutdownGrace)
			manager.recordCleanupError(session.ID(), "state lease", session.core.persistence.release())
		}()
	}
	group.Wait()
	if err := manager.cron.Stop(context.Background()); err != nil {
		manager.recordCleanupError("", filepath.Join(manager.config.WorkspaceRoot, ".cron.json"), err)
	}
	manager.WaitCleanup(context.Background())
	manager.mu.Lock()
	manager.state = ManagerStopped
	close(manager.stopped)
	manager.mu.Unlock()
}

func (manager *SessionManager) Trajectories() TrajectoryReader {
	return manager.config.Services.Trajectories
}
