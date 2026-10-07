package agent

import (
	"context"
	"fmt"
	"regexp"

	"github.com/luoyjx/mini-loop/go/tasks"
	"github.com/luoyjx/mini-loop/go/teams"
)

type SpawnTeammateRequest struct {
	Name         teams.MemberName
	Role, Prompt string
	RunContext   RunContext
}

type SpawnedTeammate struct {
	Name    teams.MemberName
	Session SessionID
}

func (value SpawnedTeammate) Render() string {
	return fmt.Sprintf("Spawned teammate '%s' (session %s); running concurrently.", value.Name, value.Session)
}

type TeamSpawnRefusal struct{ Detail string }

func (err *TeamSpawnRefusal) Error() string { return "Error: " + err.Detail }

type teammateRun struct {
	cancel      context.CancelFunc
	done        chan struct{}
	initialDone chan struct{}
}

type teammateSystemBuilder struct {
	name teams.MemberName
	role string
	base SystemBuilder
}

const teammateGuidance = "Coordinate with the team via send_message / read_inbox and the shared task board (list_tasks / claim_task / complete_task). Use submit_plan when the lead requests a plan, and wait for its correlated approval response before implementation. Report results to 'lead'."

func (builder teammateSystemBuilder) BuildSystem(value SystemContext) (string, error) {
	base, err := builder.base.BuildSystem(value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("You are teammate '%s' (role: %s) working in %s.\n%s\n\n%s", builder.name, builder.role, value.Workspace, teammateGuidance, base), nil
}

var teammateName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// SpawnTeammate publishes a fresh owned session in the parent's lifecycle root.
// Resource snapshots are inherited without consulting the resolver again. The
// initial run belongs to the session, independently of the spawning call's context.
func (manager *SessionManager) SpawnTeammate(ctx context.Context, owner OwnerID, parentID SessionID, request SpawnTeammateRequest) (spawn SpawnedTeammate, err error) {
	if err = ctx.Err(); err != nil {
		return spawn, err
	}
	parent, err := manager.Get(owner, parentID)
	if err != nil {
		return spawn, err
	}
	identity := parent.core.team
	if identity == nil {
		return spawn, &TeamSpawnRefusal{"parent has no team identity"}
	}
	member := teams.Identity{Team: identity.Team, Name: request.Name}
	manager.mu.Lock()
	if manager.state != ManagerActive {
		manager.mu.Unlock()
		return spawn, ErrManagerStopped
	}
	used := member.Name == teams.Lead || manager.teamReservations[member]
	for _, session := range manager.sessions {
		used = used || (session.core.team != nil && *session.core.team == member)
	}
	if used {
		manager.mu.Unlock()
		return spawn, &TeamSpawnRefusal{fmt.Sprintf("teammate name '%s' is already in use", request.Name)}
	}
	if !teammateName.MatchString(string(request.Name)) {
		manager.mu.Unlock()
		return spawn, &TeamSpawnRefusal{"teammate name must match [A-Za-z0-9._-]{1,64}"}
	}
	manager.teamReservations[member] = true
	manager.mu.Unlock()
	defer func() { manager.mu.Lock(); delete(manager.teamReservations, member); manager.mu.Unlock() }()
	id, err := manager.reserveID()
	if err != nil {
		return spawn, err
	}
	defer manager.finishCreate(id)
	manager.workspaceMu.Lock()
	defer manager.workspaceMu.Unlock()
	// Delete/stop may have revoked the parent while construction waited.
	manager.mu.Lock()
	valid := manager.sessions[parentID] == parent && manager.state == ManagerActive
	manager.mu.Unlock()
	if !valid {
		return spawn, ErrSessionNotFound
	}
	run := request.RunContext
	if run.MessageID() == "" {
		run, err = DefaultRunContext()
		if err != nil {
			return spawn, err
		}
	}
	peer, err := run.DeriveNamedPeerAgent(parent.core.label, ActorID(request.Name))
	if err != nil {
		return spawn, err
	}
	var pending *ManagedSession
	published := false
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("teammate construction panicked (%T)", fault)
			spawn = SpawnedTeammate{}
		}
		if !published && pending != nil {
			pending.StopAccepting("teammate construction failed")
			manager.recordCleanupError(id, "state", pending.core.persistence.delete())
		}
	}()
	path := parent.core.workspace
	bash, err := manager.config.Services.BashFactory.BashFor(ctx, SessionBinding{id, owner, path, ModeInteractive})
	if err != nil {
		return spawn, err
	}
	runtime := manager.baseManagedRuntimeConfig(id, owner, path, ModeInteractive, manager.config.Defaults.Model, nil, bash)
	runtime.Skills = parent.core.skills
	runtime.Memory = parent.core.memory
	runtime.UserResources = clonePointer(parent.core.ownerResources)
	runtime.Label = string(request.Name)
	runtime.team, runtime.teamMember = &member, true
	runtime.taskStore, err = tasks.New(tasks.Config{Workspace: path, Secrets: manager.config.Services.Secrets})
	if err != nil {
		return spawn, err
	}
	base := runtime.SystemBuilder
	if base == nil {
		base = DefaultSystemBuilder{}
	}
	runtime.SystemBuilder = teammateSystemBuilder{request.Name, request.Role, base}
	session, err := newManagedSession(runtime, true)
	if err != nil {
		return spawn, err
	}
	pending = session
	session.workspaceBound = parent.workspaceBound
	if err = session.core.persistence.initialize(); err != nil {
		return spawn, err
	}
	if err = ctx.Err(); err != nil {
		return spawn, err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.state != ManagerActive {
		return spawn, ErrManagerStopped
	}
	if manager.sessions[parentID] != parent {
		return spawn, ErrSessionNotFound
	}
	owned, cancel := context.WithCancel(context.Background())
	session.teamRun = &teammateRun{cancel: cancel, done: make(chan struct{}), initialDone: make(chan struct{})}
	manager.sessions[id] = session
	manager.order = append(manager.order, id)
	published = true
	go manager.initialTeammateRun(owned, session, request.Prompt, peer)
	return SpawnedTeammate{request.Name, id}, nil
}

func (manager *SessionManager) initialTeammateRun(ctx context.Context, session *ManagedSession, prompt string, run RunContext) {
	defer close(session.teamRun.done)
	defer session.teamRun.cancel()
	defer func() {
		if fault := recover(); fault != nil {
			session.emitFor(run, SessionEvent{kind: EventError, runError: RunErrorEvent{kind: ErrorRuntime, detail: fmt.Sprintf("teammate runner panicked (%T)", fault)}})
		}
	}()
	err := func() error {
		defer close(session.teamRun.initialDone)
		result, err := session.RunWithContext(ctx, prompt, run)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return manager.deliverTeammateResult(ctx, session, result, nil)
	}()
	if err == nil {
		err = manager.teammateIdleLoop(ctx, session)
	}
	if err != nil && ctx.Err() == nil {
		session.emitFor(run, SessionEvent{kind: EventError, runError: RunErrorEvent{kind: ErrorRuntime, detail: err.Error()}})
	}
}
