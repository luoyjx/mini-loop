package agent

import (
	"context"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// SessionRecord is the schema-v7 session projection. EventCursor is derived
// from the event table, not a column in sessions. Model, mode, lineage and human
// authority are deliberately absent: the Python session row does not store them.
type SessionRecord struct {
	SessionID       SessionID           `json:"session_id"`
	Workspace       string              `json:"workspace"`
	System          *string             `json:"system"`
	CreatedAt       float64             `json:"created_at"`
	RunCount        int                 `json:"run_count"`
	Status          SessionStatus       `json:"status"`
	EventCursor     EventOrdinal        `json:"event_cursor"`
	Todos           []protocol.TodoItem `json:"todos"`
	Owner           OwnerID             `json:"owner"`
	PendingSteering []string            `json:"pending_steering"`
	WorkspaceBound  bool                `json:"workspace_bound"`
}

func (record SessionRecord) Clone() SessionRecord {
	record.System = clonePointer(record.System)
	record.Todos = append([]protocol.TodoItem{}, record.Todos...)
	record.PendingSteering = append([]string{}, record.PendingSteering...)
	return record
}

// SessionStore is owned by the fleet consumer. Deleting operational state must
// retain action and approval audit rows, matching SQLiteStateStore.delete_session.
// A store alone grants no owner authorization or live session admission.
type SessionStore interface {
	UpsertSession(context.Context, SessionRecord) error
	LoadSessions(context.Context) ([]SessionRecord, error)
	DeleteSession(context.Context, SessionID) error
}

// TranscriptEpoch distinguishes a rewritten transcript from an append. Message
// ordinals remain globally increasing across epochs within one session.
type TranscriptEpoch int

// TranscriptStore uses nil to select the highest stored epoch. An empty append
// returns the count of the requested epoch, not the latest global ordinal.
type TranscriptStore interface {
	AppendMessages(context.Context, SessionID, []protocol.Message, TranscriptEpoch) (int, error)
	LoadMessages(context.Context, SessionID, *TranscriptEpoch) ([]protocol.Message, error)
	MessageCount(context.Context, SessionID, *TranscriptEpoch) (int, error)
	TranscriptEpoch(context.Context, SessionID) (TranscriptEpoch, error)
}

// LeaseOwner identifies a process instance, independently of the tenant OwnerID.
// Acquire is a conditional update of an existing session row; Renew must not
// reclaim a lease that expired or was taken by another process.
type LeaseOwner string

type LeaseStore interface {
	AcquireLease(context.Context, SessionID, LeaseOwner, time.Duration) (bool, error)
	RenewLease(context.Context, SessionID, LeaseOwner, time.Duration) (bool, error)
	ReleaseLease(context.Context, SessionID, LeaseOwner) error
	LeaseHolder(context.Context, SessionID) (LeaseOwner, bool, error)
}

// ApprovalReader is the restart consumer, separate from the live write seam.
// nil status selects all records. Read records do not recreate human grants.
type ApprovalReader interface {
	ReadApprovals(context.Context, SessionID, *ApprovalStatus) ([]ApprovalRecord, error)
}

// EventOrdinal is a physical stored row position, separate from the live
// EventSequence carried by SSE. Ephemeral events consume sequences, not ordinals.
type EventOrdinal uint64

// EventStore persists known typed variants. after is an exclusive ordinal;
// nil limit reads the complete tail. Reading historical events grants no live
// execution authority and must never reinstall remembered approval grants.
// Implementations must allow concurrent context-owned reads and live writes.
type EventStore interface {
	AppendEvent(context.Context, SessionID, SessionEventRecord) (EventOrdinal, error)
	LoadEvents(context.Context, SessionID, EventOrdinal, *int) ([]SessionEventRecord, error)
	EventCursor(context.Context, SessionID) (EventOrdinal, error)
}

// StateStore is the explicitly configured fleet backend. Small interfaces above
// remain the consumers' contracts; this composition supplies the SQLite feature
// set without runtime probing or an untyped storage payload. The caller owns
// backend close. Supplying an implementation does not prove SQLite durability.
type StateStore interface {
	SessionStore
	TranscriptStore
	EventStore
	LeaseStore
	ActionStore
	ApprovalStore
	ApprovalReader
}
