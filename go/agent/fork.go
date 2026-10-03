package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/protocol"
)

var ErrForkBusy = errors.New("cannot fork a busy session: its transcript tail is an open turn, not a completed boundary. Wait for the turn to finish or cancel it.")

type ForkLineage struct {
	Session      SessionID `json:"session"`
	MessageCount int       `json:"message_count"`
}

const EventSessionForked SessionEventKind = "session_forked"

type SessionForkedEvent struct {
	Child        SessionID `json:"child"`
	MessageCount int       `json:"message_count"`
}

func (event SessionEvent) SessionForked() (SessionForkedEvent, bool) {
	return event.sessionForked, event.kind == EventSessionForked
}

type forkSnapshot struct {
	lineage  ForkLineage
	messages []protocol.Message
}

// Fork copies one completed boundary into a fresh scratch session. It inherits
// owner, explicit system and current mode, but uses the manager's default model
// and fresh tool/control state, just like Python. No state store is implied.
func (manager *SessionManager) Fork(ctx context.Context, owner OwnerID, id SessionID) (*ManagedSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := manager.Get(owner, id)
	if err != nil {
		return nil, err
	}
	source.mu.Lock()
	if !source.accepting {
		source.mu.Unlock()
		return nil, ErrSessionNotFound
	}
	select {
	case <-source.admission:
	default:
		source.mu.Unlock()
		return nil, ErrForkBusy
	}
	// Admission also covers the interval between a terminal event and release.
	// Pin only the copy: later turns and HTTP idle steering must remain usable
	// while a workspace factory provisions the detached child. Keep mu until
	// release so SubmitSteering cannot mistake a fork reservation for a live turn.
	seed, request, err := func() (forkSnapshot, CreateSessionRequest, error) {
		defer func() { source.admission <- struct{}{}; source.mu.Unlock() }()
		return source.forkSnapshot()
	}()
	if err != nil {
		return nil, err
	}
	child, err := manager.create(ctx, request, &seed)
	if err != nil {
		return nil, err
	}
	source.emitFor(RunContext{}, SessionEvent{kind: EventSessionForked, sessionForked: SessionForkedEvent{child.ID(), seed.lineage.MessageCount}})
	return child, nil
}

func (source *ManagedSession) forkSnapshot() (forkSnapshot, CreateSessionRequest, error) {
	source.core.mu.Lock()
	defer source.core.mu.Unlock()
	if len(source.core.messages) > 0 {
		if err := protocol.ValidateTranscript(source.core.messages); err != nil {
			return forkSnapshot{}, CreateSessionRequest{}, err
		}
	}
	seed := forkSnapshot{lineage: ForkLineage{source.ID(), len(source.core.messages)}, messages: make([]protocol.Message, len(source.core.messages))}
	for i, message := range source.core.messages {
		seed.messages[i] = protocol.Message{Role: message.Role, Content: message.Content.Clone()}
	}
	return seed, CreateSessionRequest{Owner: source.Owner(), PermissionMode: source.core.permissionMode(), System: clonePointer(source.core.explicitSystem)}, nil
}
