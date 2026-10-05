package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

var ErrStateRestore = errors.New("state restore failed")
var ErrStateRestoreConflict = errors.New("session identity is reserved or retired")

const restoreInterruptedText = "[Turn interrupted: process stopped mid-generation]"

// RestoreSessions is an operator library operation over the explicitly supplied
// backend. It never starts a turn, arms jobs, installs historical grants, or
// opens/closes a backend. Earlier published handles remain in the returned slice
// if a later row fails. There is no whole-fleet transaction claim.
func (manager *SessionManager) RestoreSessions(ctx context.Context) (restored []*ManagedSession, err error) {
	ctx, finish, err := manager.beginRestore(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	defer func() {
		if err != nil && errors.Is(context.Cause(ctx), ErrManagerStopped) {
			err = errors.Join(ErrManagerStopped, err)
		}
	}()
	restored = []*ManagedSession{}
	store := manager.config.Services.StateStore
	if store == nil {
		return restored, nil
	}
	var rows []SessionRecord
	if err = stateFault(func() error { var e error; rows, e = store.LoadSessions(ctx); return e }); err != nil {
		return restored, err
	}
	for _, input := range rows {
		if err = ctx.Err(); err != nil {
			return restored, err
		}
		var session *ManagedSession
		session, err = manager.restoreSession(ctx, input.Clone())
		if err != nil {
			return restored, err
		}
		if session != nil {
			restored = append(restored, session)
		}
	}
	return restored, nil
}

// beginRestore owns the whole inventory/construction interval for shutdown.
func (manager *SessionManager) beginRestore(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(manager.restoreLifetime, func() { cancel(ErrManagerStopped) })
	select {
	case <-ctx.Done():
		cancel(context.Canceled)
		stop()
		return nil, nil, context.Cause(ctx)
	case <-manager.restoreTurn:
	}
	manager.mu.Lock()
	if manager.state != ManagerActive {
		manager.mu.Unlock()
		manager.restoreTurn <- struct{}{}
		cancel(ErrManagerStopped)
		stop()
		return nil, nil, ErrManagerStopped
	}
	if manager.creating == 0 {
		manager.createsDrained = make(chan struct{})
	}
	manager.creating++
	manager.mu.Unlock()
	return ctx, func() { stop(); cancel(context.Canceled); manager.finishCreate(""); manager.restoreTurn <- struct{}{} }, nil
}

type restorationKind uint8

const (
	recordedRestoration restorationKind = iota
	scheduledRestoration
)

func (manager *SessionManager) restoreSession(ctx context.Context, row SessionRecord) (*ManagedSession, error) {
	return manager.restoreSelected(ctx, row.SessionID, &row, recordedRestoration)
}

// RestoreScheduledSession is privileged operator resolution for a stable cron ID.
// It returns a live handle unchanged; saved scratch uses the current factory,
// saved bound state retains its recorded workspace, and no row means anonymous.
// Like Python, scheduled construction uses the current system builder, rather
// than a recorded explicit system. Human authority/activation is never restored.
func (manager *SessionManager) RestoreScheduledSession(ctx context.Context, id SessionID) (session *ManagedSession, err error) {
	if id == "" {
		return nil, fmt.Errorf("%w: empty scheduled identity", ErrStateRestore)
	}
	manager.mu.Lock()
	if manager.state != ManagerActive {
		manager.mu.Unlock()
		return nil, ErrManagerStopped
	}
	live := manager.sessions[id]
	manager.mu.Unlock()
	if live != nil {
		return live, nil
	}
	ctx, finish, err := manager.beginRestore(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	defer func() {
		if err != nil && errors.Is(context.Cause(ctx), ErrManagerStopped) {
			err = errors.Join(ErrManagerStopped, err)
		}
	}()
	manager.mu.Lock()
	live = manager.sessions[id]
	manager.mu.Unlock()
	if live != nil {
		return live, nil
	}
	var rows []SessionRecord
	if store := manager.config.Services.StateStore; store != nil {
		if err = stateFault(func() error { var e error; rows, e = store.LoadSessions(ctx); return e }); err != nil {
			return nil, err
		}
	}
	for _, input := range rows {
		if input.SessionID == id {
			row := input.Clone()
			return manager.restoreSelected(ctx, id, &row, scheduledRestoration)
		}
	}
	return manager.restoreSelected(ctx, id, nil, scheduledRestoration)
}

func (manager *SessionManager) restoreSelected(ctx context.Context, id SessionID, row *SessionRecord, kind restorationKind) (session *ManagedSession, err error) {
	if row != nil {
		if err = validateRestoreRecord(*row); err != nil {
			return nil, err
		}
	}

	manager.mu.Lock()
	if manager.state != ManagerActive {
		manager.mu.Unlock()
		return nil, ErrManagerStopped
	}
	if live := manager.sessions[id]; live != nil {
		manager.mu.Unlock()
		if kind == scheduledRestoration {
			return live, nil
		}
		return nil, nil
	}
	if manager.reservations[id] || manager.retiring[id] != nil || manager.owners[id] != "" {
		manager.mu.Unlock()
		return nil, ErrStateRestoreConflict
	}
	manager.reservations[id] = true
	manager.mu.Unlock()
	defer func() { manager.mu.Lock(); delete(manager.reservations, id); manager.mu.Unlock() }()
	manager.workspaceMu.Lock()
	defer manager.workspaceMu.Unlock()
	published := false
	var pending *ManagedSession
	defer func() {
		if fault := recover(); fault != nil {
			session = nil
			err = fmt.Errorf("%w: construction panicked (%T)", ErrStateRestore, fault)
		}
		if !published && pending != nil {
			err = errors.Join(err, pending.core.persistence.release())
		}
	}()
	bound := row != nil && row.WorkspaceBound
	owner := OwnerID("anonymous")
	var system *string
	if row != nil {
		owner = row.Owner
		if kind == recordedRestoration {
			system = row.System
		}
	}
	var path string
	if kind == recordedRestoration || bound {
		path = row.Workspace
	} else {
		path = filepath.Join(manager.config.WorkspaceRoot, string(id))
		if factory := manager.config.WorkspaceFactory; factory != nil {
			path, err = factory.WorkspaceFor(ctx, id)
		}
	}
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("workspace factory returned an empty path")
	}
	path, err = workspace.ResolvePath(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	services := manager.config.Services
	bash, err := services.BashFactory.BashFor(ctx, SessionBinding{id, owner, path, ModeInteractive})
	if err != nil {
		return nil, err
	}
	session, err = newManagedSession(manager.managedRuntimeConfig(id, owner, path, ModeInteractive, manager.config.Defaults.Model, system, bash), true)
	if err != nil {
		return nil, err
	}
	pending = session
	session.workspaceBound = bound
	session.core.explicitSystem = clonePointer(system)
	if row != nil {
		err = session.core.persistence.restore(ctx, *row)
	} else {
		err = session.core.persistence.restoreMissing(ctx)
	}
	if err != nil {
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

func validateRestoreRecord(row SessionRecord) error {
	if row.SessionID == "" || row.Owner == "" || row.Workspace == "" || row.RunCount < 0 || math.IsNaN(row.CreatedAt) || math.IsInf(row.CreatedAt, 0) {
		return fmt.Errorf("%w: invalid session record", ErrStateRestore)
	}
	switch row.Status {
	case StatusIdle, StatusRunning, StatusError:
	default:
		return fmt.Errorf("%w: invalid session status", ErrStateRestore)
	}
	return nil
}

type restoredState struct {
	record   SessionRecord
	messages []protocol.Message
	epoch    TranscriptEpoch
	next     EventSequence
}

func (p *sessionPersistence) loadRestoreLocked(ctx context.Context, row SessionRecord) (restoredState, error) {
	var snapshot restoredState
	if err := validateRestoreRecord(row); err != nil {
		return snapshot, err
	}
	path, err := workspace.ResolvePath(row.Workspace)
	if err != nil {
		return snapshot, err
	}
	identity := p.restoreIdentity
	if identity == nil {
		identity = &stateRestoreIdentity{p.session.Owner(), p.session.core.workspace, p.session.workspaceBound, p.session.core.explicitSystem}
	}
	if row.SessionID != p.session.ID() || row.Owner != identity.owner || path != identity.workspace || row.WorkspaceBound != identity.bound || !sameSystem(row.System, identity.system) {
		return snapshot, fmt.Errorf("%w: session identity or binding changed", ErrStateRestore)
	}
	snapshot.record = row.Clone()
	var messages []protocol.Message
	if err = stateFault(func() error { var e error; messages, e = p.store.LoadMessages(ctx, p.session.ID(), nil); return e }); err != nil {
		return snapshot, err
	}
	snapshot.messages = make([]protocol.Message, len(messages))
	for i, m := range messages {
		snapshot.messages[i] = protocol.Message{Role: m.Role, Content: m.Content.Clone()}
	}
	// Permit only the known crash-tail shape. Validate a detached preview before
	// changing approvals or transcript rows; malformed earlier history fails closed.
	preview, _ := repairRestoredMessages(snapshot.messages, nil)
	if len(preview) > 0 {
		if err = protocol.ValidateTranscript(preview); err != nil {
			return snapshot, fmt.Errorf("%w: transcript: %w", ErrStateRestore, err)
		}
	}
	if err = stateFault(func() error { var e error; snapshot.epoch, e = p.store.TranscriptEpoch(ctx, p.session.ID()); return e }); err != nil {
		return snapshot, err
	}
	snapshot.epoch = max(1, snapshot.epoch)
	if err = stateFault(func() error { var e error; snapshot.next, e = p.store.EventCursor(ctx, p.session.ID()); return e }); err != nil {
		return snapshot, err
	}
	var events []SessionEventRecord
	if err = stateFault(func() error { var e error; events, e = p.store.LoadEvents(ctx, p.session.ID(), 0, nil); return e }); err != nil {
		return snapshot, err
	}
	for _, event := range events {
		// Unknown variants are unsupported; they are never silently projected away.
		encoded, e := json.Marshal(event)
		if e != nil {
			return snapshot, fmt.Errorf("%w: event: %w", ErrStateRestore, e)
		}
		if event.SessionID != p.session.ID() {
			return snapshot, fmt.Errorf("%w: event read crossed its session scope", ErrStateRestore)
		}
		if _, e = DecodeStoredEvent(encoded); e != nil {
			return snapshot, fmt.Errorf("%w: event: %w", ErrStateRestore, e)
		}
		snapshot.next = max(snapshot.next, event.Sequence)
	}
	return snapshot, nil
}
func sameSystem(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (p *sessionPersistence) applyRestoreLocked(snapshot restoredState) {
	s := p.session
	s.mu.Lock()
	s.createdAt = snapshot.record.CreatedAt
	s.runCount = snapshot.record.RunCount
	s.status = snapshot.record.Status
	s.mu.Unlock()
	s.core.messages = snapshot.messages
	s.core.publishLive()
	p.refs = append([]protocol.Message(nil), snapshot.messages...)
	p.epoch = snapshot.epoch
	s.core.todos.mu.Lock()
	s.core.todos.items = append([]protocol.TodoItem{}, snapshot.record.Todos...)
	s.core.todos.mu.Unlock()
	s.core.control.mu.Lock()
	s.core.control.steering = append([]string{}, snapshot.record.PendingSteering...)
	s.core.control.mu.Unlock()
	s.core.events.mu.Lock()
	s.core.events.epoch = int(snapshot.epoch)
	s.core.events.next = max(s.core.events.next, snapshot.next)
	s.core.events.mu.Unlock()
	p.restored = true
}

func (p *sessionPersistence) restore(ctx context.Context, row SessionRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	path, err := workspace.ResolvePath(row.Workspace)
	if err != nil {
		return err
	}
	p.restoreIdentity = &stateRestoreIdentity{row.Owner, path, row.WorkspaceBound, clonePointer(row.System)}
	var acquired bool
	err = stateFault(func() error {
		var e error
		acquired, e = p.store.AcquireLease(ctx, p.session.ID(), p.owner, p.ttl)
		return e
	})
	if err != nil {
		return err
	}
	p.confirmed = acquired
	p.pendingRestore = true
	if acquired {
		// Re-read after the conditional claim: a prior writer may have advanced
		// the transcript since the fleet inventory was taken.
		return p.reloadPendingLocked(ctx)
	}
	snapshot, err := p.loadRestoreLocked(ctx, row)
	if err != nil {
		return err
	}
	p.applyRestoreLocked(snapshot)
	return nil // Expose recorded facts; repair waits for our lease.

}

func (p *sessionPersistence) finishRestoreLocked(ctx context.Context) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	pending := ApprovalPending
	var approvals []ApprovalRecord
	if err := stateFault(func() error {
		var e error
		approvals, e = p.store.ReadApprovals(ctx, p.session.ID(), &pending)
		return e
	}); err != nil {
		return err
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	overrides := map[string]string{}
	for _, input := range approvals {
		row := input.Clone()
		if row.SessionID != p.session.ID() || row.Status != ApprovalPending {
			return fmt.Errorf("%w: approval read crossed its session/status scope", ErrStateRestore)
		}
		now := float64(time.Now().UnixMicro()) / 1e6
		row.Status = ApprovalExpired
		row.ResolvedAt = &now
		if err := stateFault(func() error { return p.store.WriteApproval(ctx, row) }); err != nil {
			return err
		}
		if row.ToolUseID != "" {
			overrides[row.ToolUseID] = NotRunActionResult
		}
	}
	messages, repaired := repairRestoredMessages(p.session.core.messages, overrides)
	p.repaired = append([]string{}, repaired...)
	if len(repaired) > 0 || len(messages) != len(p.session.core.messages) {
		p.session.core.messages = messages
		p.session.core.publishLive()
		if err := p.flushLocked(messages); err != nil {
			return err
		}
		// Restore must not publish an unlogged repair, even though ordinary live
		// event writes degrade. Partial database effects are not rolled back here.
		if p.fault != nil {
			return fmt.Errorf("%w: %s", ErrStateRestore, *p.fault)
		}
	}
	p.pendingRestore = false
	return nil
}

func repairRestoredMessages(history []protocol.Message, overrides map[string]string) ([]protocol.Message, []string) {
	if len(history) == 0 {
		return history, nil
	}
	last := history[len(history)-1]
	blocks, _ := last.Content.Blocks()
	ids := []string{}
	results := []protocol.Block{}
	if last.Role == protocol.RoleAssistant {
		for _, block := range blocks {
			if use, ok := block.ToolUse(); ok {
				ids = append(ids, use.ID)
				text := unknownToolResult
				if value, ok := overrides[use.ID]; ok {
					text = value
				}
				results = append(results, protocol.NewToolResult(use.ID, text, false))
			}
		}
	}
	if len(ids) > 0 {
		return append(history, protocol.Message{Role: protocol.RoleUser, Content: protocol.BlockContent(results...)}), ids
	}
	if last.Role == protocol.RoleUser {
		for _, block := range blocks {
			if _, ok := block.ToolResult(); ok {
				return history, nil
			}
		}
		return append(history, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewTextBlock(restoreInterruptedText))}), nil
	}
	return history, nil
}

func (p *sessionPersistence) reloadPendingLocked(ctx context.Context) error {
	var rows []SessionRecord
	if err := stateFault(func() error { var e error; rows, e = p.store.LoadSessions(ctx); return e }); err != nil {
		return err
	}
	var record *SessionRecord
	for _, row := range rows {
		if row.SessionID == p.session.ID() {
			value := row.Clone()
			record = &value
			break
		}
	}
	if record == nil {
		return fmt.Errorf("%w: session row disappeared", ErrStateRestore)
	}
	snapshot, err := p.loadRestoreLocked(ctx, *record)
	if err != nil {
		return err
	}
	p.applyRestoreLocked(snapshot)
	return p.finishRestoreLocked(ctx)
}
func (p *sessionPersistence) steeringReady() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingRestore {
		return ErrSessionLeaseLost
	}
	return nil
}

// A missing SQL row cannot be acquired. Publish an unconfirmed anonymous handle,
// matching source resolution; no unconditional upsert may overwrite a racing row.
// A later claim must re-read/validate before admitting a turn.
func (p *sessionPersistence) restoreMissing(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var acquired bool
	err := stateFault(func() error {
		var e error
		acquired, e = p.store.AcquireLease(ctx, p.session.ID(), p.owner, p.ttl)
		return e
	})
	if err != nil {
		return err
	}
	p.confirmed = acquired
	p.pendingRestore = true
	if acquired {
		return p.reloadPendingLocked(ctx)
	}
	return nil
}

type stateRestoreIdentity struct {
	owner     OwnerID
	workspace string
	bound     bool
	system    *string
}

func (p *sessionPersistence) rememberRestoreProjectionLocked(row SessionRecord) {
	if p.restoreIdentity != nil {
		p.restoreIdentity = &stateRestoreIdentity{row.Owner, row.Workspace, row.WorkspaceBound, clonePointer(row.System)}
	}
}
