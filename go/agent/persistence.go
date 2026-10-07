package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const DefaultStateLeaseTTL = 120 * time.Second

// ErrSessionLeaseLost carries no foreign holder or process detail.
var ErrSessionLeaseLost = errors.New("session_lease_lost")

// ErrStateTranscript identifies a request-boundary invariant/query failure.
// Ordinary write degradation is reported separately and does not raise it.
var ErrStateTranscript = errors.New("state transcript guard failed")

// StatePersistenceStatus reports acceptance/faults of an injected backend. It
// does not identify that backend as SQLite, or attest to physical durability.
type StatePersistenceStatus struct {
	Configured bool
	Error      *string
	// Confirmation records that this process acquired the lease. Renewal loss
	// terminates a turn; this field alone is not a fresh holder query.
	LeaseConfirmed   bool
	Restored         bool
	RestorePending   bool
	RepairedToolUses []string
}

// State writes are serialized and must not reenter a persistence write method.
// Catch-up reads run without this lock; the backend owns concurrent read/write safety.
// History comes from immutable live snapshots, never from the core turn mutex.
type sessionPersistence struct {
	mu              sync.Mutex
	store           StateStore
	session         *ManagedSession
	owner           LeaseOwner
	ttl             time.Duration
	confirmed       bool
	restored        bool
	restoreIdentity *stateRestoreIdentity
	pendingRestore  bool
	repaired        []string
	disabled        bool
	epoch           TranscriptEpoch
	refs            []protocol.Message
	fault           *string
	lost            bool
	cancel          context.CancelCauseFunc
	turn            uint64
}

func stateFault(action func() error) (err error) {
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("state callback panicked (%T)", fault)
		}
	}()
	return action()
}

func (p *sessionPersistence) failLocked(err error) {
	if err == nil {
		return
	}
	value := maskedText(p.session.core.secrets, boundedError(err))
	p.fault = &value
}

func (p *sessionPersistence) snapshot() StatePersistenceStatus {
	if p == nil {
		return StatePersistenceStatus{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return StatePersistenceStatus{Configured: true, Error: clonePointer(p.fault), LeaseConfirmed: p.confirmed, Restored: p.restored, RestorePending: p.pendingRestore, RepairedToolUses: append([]string{}, p.repaired...)}
}

func (session *ManagedSession) PersistenceStatus() StatePersistenceStatus {
	return session.core.persistence.snapshot()
}

func (session *ManagedSession) stateRecord() SessionRecord {
	session.mu.Lock()
	status, runCount, createdAt := session.status, session.runCount, session.createdAt
	session.mu.Unlock()
	c := session.core.control
	c.mu.Lock()
	steering := append([]string{}, c.steering...)
	c.mu.Unlock()
	for i := range steering {
		steering[i] = maskedText(session.core.secrets, steering[i])
	}
	return SessionRecord{SessionID: session.ID(), Workspace: session.core.workspace,
		System: clonePointer(session.core.explicitSystem), CreatedAt: createdAt,
		RunCount: runCount, Status: status, Owner: session.Owner(),
		Todos: session.core.Todos(), PendingSteering: steering, WorkspaceBound: session.workspaceBound}
}

func (p *sessionPersistence) initialize() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := stateFault(func() error { return p.store.UpsertSession(context.Background(), p.session.stateRecord().Clone()) }); err != nil {
		p.failLocked(err)
	}
	if p.owner != "" {
		var acquired bool
		err := stateFault(func() error {
			var err error
			acquired, err = p.store.AcquireLease(context.Background(), p.session.ID(), p.owner, p.ttl)
			return err
		})
		if err != nil {
			return err
		}
		p.confirmed = acquired
	}
	return p.flushLocked(p.history())
}

func (p *sessionPersistence) history() []protocol.Message {
	view := p.session.core.live.Load()
	if view == nil {
		return nil
	}
	return view.Messages
}

func stateMessageProjection(message protocol.Message, mask TextMasker) (protocol.Message, error) {
	if mask == nil {
		return protocol.Message{Role: message.Role, Content: message.Content.Clone()}, nil
	}
	// Raw JSON is transient at this projection boundary. The source masker walks
	// string values and keys; invalid masked structure degrades, never writes raw.
	encoded, err := protocol.MaskedPythonJSON(message, mask.MaskText, false, true)
	if err != nil {
		return protocol.Message{}, err
	}
	var projected protocol.Message
	err = json.Unmarshal([]byte(encoded), &projected)
	return projected, err
}

func (p *sessionPersistence) loseLocked() error {
	p.lost = true
	if p.cancel != nil {
		p.cancel(ErrSessionLeaseLost)
	}
	return ErrSessionLeaseLost
}

func (p *sessionPersistence) flushLocked(history []protocol.Message) error {
	if p.disabled || len(history) == 0 {
		return nil
	}
	if p.lost {
		return ErrSessionLeaseLost
	}
	rewritten := len(history) < len(p.refs)
	if !rewritten {
		for i, old := range p.refs {
			if history[i].Role != old.Role || !history[i].Content.SameStorage(old.Content) {
				rewritten = true
				break
			}
		}
	}
	if rewritten {
		p.epoch++
		p.refs = nil
	}
	pending := history[len(p.refs):]
	if len(pending) == 0 {
		return nil
	}
	projected := make([]protocol.Message, len(pending))
	for i, message := range pending {
		var err error
		projected[i], err = stateMessageProjection(message, p.session.core.secrets)
		if err != nil {
			p.failLocked(err)
			return nil
		}
	}
	err := stateFault(func() error {
		_, err := p.store.AppendMessages(context.Background(), p.session.ID(), projected, p.epoch)
		return err
	})
	if err != nil {
		p.failLocked(err)
		return nil
	}
	p.refs = append([]protocol.Message(nil), history...)
	row := p.session.stateRecord().Clone()
	if err = stateFault(func() error { return p.store.UpsertSession(context.Background(), row) }); err != nil {
		p.failLocked(err)
		return nil
	}
	p.rememberRestoreProjectionLocked(row)
	if p.owner != "" {
		var renewed bool
		err = stateFault(func() error {
			var err error
			renewed, err = p.store.RenewLease(context.Background(), p.session.ID(), p.owner, p.ttl)
			return err
		})
		if err != nil {
			p.failLocked(err)
			return nil
		}
		if !renewed && p.confirmed {
			return p.loseLocked()
		}
	}
	return nil
}

func (p *sessionPersistence) capture(record *SessionEventRecord) error {
	if p == nil || record.Event.Ephemeral() {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled {
		return nil
	}
	if p.lost || p.pendingRestore {
		return ErrSessionLeaseLost
	}
	// The source stamps the event before its subsequent flush can open an epoch.
	record.TranscriptEpoch = int(p.epoch)
	stored := *record
	encoded, err := protocol.MaskedPythonJSON(stored, func(text string) string { return maskedText(p.session.core.secrets, text) }, false, true)
	if err != nil {
		p.failLocked(err)
		return nil
	}
	stored, err = DecodeStoredEvent([]byte(encoded))
	if err != nil {
		p.failLocked(err)
		return nil
	}
	if err := stateFault(func() error { _, err := p.store.AppendEvent(context.Background(), p.session.ID(), stored); return err }); err != nil {
		p.failLocked(err)
		return nil
	}
	return p.flushLocked(p.history())
}

func (p *sessionPersistence) currentEpoch() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return int(p.epoch)
}

func (p *sessionPersistence) guard(history []protocol.Message) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled {
		return nil
	}
	if p.lost || p.pendingRestore {
		return ErrSessionLeaseLost
	}
	if p.fault != nil {
		return nil
	} // Reported degradation matches the source.
	if err := p.flushLocked(history); err != nil {
		return err
	}
	if p.fault != nil {
		return nil
	}
	var count int
	err := stateFault(func() error {
		var err error
		count, err = p.store.MessageCount(context.Background(), p.session.ID(), nil)
		return err
	})
	if err != nil {
		return errors.Join(ErrStateTranscript, err)
	}
	if count != len(history) {
		return fmt.Errorf("%w: stored count %d, live count %d", ErrStateTranscript, count, len(history))
	}
	return nil
}

func (p *sessionPersistence) requireLease(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled {
		return errors.New("session persistence is closed")
	}
	if p.owner == "" {
		return nil
	}
	var acquired bool
	err := stateFault(func() error {
		var err error
		acquired, err = p.store.AcquireLease(ctx, p.session.ID(), p.owner, p.ttl)
		return err
	})
	if err != nil {
		return err
	}
	if !acquired {
		return ErrSessionLeaseLost
	}
	p.confirmed, p.lost = true, false
	if p.pendingRestore {
		p.fault = nil
		if err := p.reloadPendingLocked(ctx); err != nil {
			p.confirmed = false
			p.pendingRestore = true
			return errors.Join(err, stateFault(func() error { return p.store.ReleaseLease(context.Background(), p.session.ID(), p.owner) }))
		}
	}
	return nil
}

// Verified tasks run child transcripts without growing the parent's transcript.
// Check the existing lease before their effect/event boundaries; reacquiring a
// missing lease here could turn another owner's work into this task's authority.
func (p *sessionPersistence) guardVerifiedLease(ctx context.Context) error {
	if p == nil {
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled || p.lost || p.pendingRestore {
		return ErrSessionLeaseLost
	}
	if p.owner == "" {
		return ctx.Err()
	}
	var renewed bool
	err := stateFault(func() error {
		var err error
		renewed, err = p.store.RenewLease(ctx, p.session.ID(), p.owner, p.ttl)
		return err
	})
	if err != nil {
		return err
	}
	if !renewed {
		return p.loseLocked()
	}
	return ctx.Err()
}

func (p *sessionPersistence) bindTurn(cancel context.CancelCauseFunc) func() {
	if p == nil {
		return func() {}
	}
	p.mu.Lock()
	p.turn++
	turn := p.turn
	p.cancel = cancel
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		if p.turn == turn {
			p.cancel = nil
		}
		p.mu.Unlock()
	}
}

func (p *sessionPersistence) refreshRecord() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled || p.lost || p.pendingRestore {
		return
	}
	p.failLocked(stateFault(func() error { return p.store.UpsertSession(context.Background(), p.session.stateRecord().Clone()) }))
}

func (p *sessionPersistence) delete() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disabled, p.confirmed = true, false
	owner := p.owner
	p.owner = ""
	err := stateFault(func() error { return p.store.DeleteSession(context.Background(), p.session.ID()) })
	if err != nil && owner != "" {
		err = errors.Join(err, stateFault(func() error { return p.store.ReleaseLease(context.Background(), p.session.ID(), owner) }))
	}
	return err
}

func (p *sessionPersistence) release() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	owner := p.owner
	p.owner, p.confirmed = "", false
	if owner == "" {
		return nil
	}
	return stateFault(func() error { return p.store.ReleaseLease(context.Background(), p.session.ID(), owner) })
}

func stateRunError(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), ErrSessionLeaseLost) {
		return ErrSessionLeaseLost
	}
	return ctx.Err()
}
