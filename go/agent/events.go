package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const EventBacklog = 200

type SessionEventKind string

const EventTodo SessionEventKind = "todo"
const EventCompact SessionEventKind = "compact"
const EventError SessionEventKind = "error"

type RunErrorKind string

const ErrorRoundExhaustion RunErrorKind = "round_exhaustion"

type RunErrorEvent struct {
	kind   RunErrorKind
	rounds int
}

func (event RunErrorEvent) Kind() RunErrorKind { return event.kind }
func (event RunErrorEvent) Error() string {
	return fmt.Sprintf("Hit max_rounds (%d) without finishing", event.rounds)
}

const (
	EventSubagentStart   SessionEventKind = "subagent_start"
	EventSubagentEnd     SessionEventKind = "subagent_end"
	EventSubagentRefused SessionEventKind = "subagent_refused"
)

type SubagentEvent struct {
	kind              SessionEventKind
	role              AgentRole
	prompt, summary   string
	childDepth, limit int
}

func (event SubagentEvent) Role() AgentRole { return event.role }
func (event SubagentEvent) Prompt() (string, bool) {
	return event.prompt, event.kind == EventSubagentStart
}
func (event SubagentEvent) Summary() (string, bool) {
	return event.summary, event.kind == EventSubagentEnd
}
func (event SubagentEvent) Refusal() (int, int, bool) {
	return event.childDepth, event.limit, event.kind == EventSubagentRefused
}

// SessionEvent is a closed union. Accessors return detached values only
// for their corresponding variant; there is no untyped event payload.
type SessionEvent struct {
	kind     SessionEventKind
	stop     ProviderStopEvent
	todos    []protocol.TodoItem
	compact  CompactionEvent
	subagent SubagentEvent
	runError RunErrorEvent
	approval ApprovalEvent
}

func (event SessionEvent) Approval() (ApprovalEvent, bool) {
	switch ApprovalEventKind(event.kind) {
	case ApprovalRequiredEvent, ApprovalTimeoutEvent, ApprovalGrantUsedEvent, ApprovalGrantRecordedEvent, ApprovalGrantRefusedEvent, ApprovalAutoReviewedEvent:
		return event.approval.clone(), true
	}
	return ApprovalEvent{}, false
}

type sessionApprovalSink struct{ events *sessionEvents }

func (sink sessionApprovalSink) EmitApproval(_ context.Context, event ApprovalEvent) error {
	sink.events.append(SessionEvent{kind: SessionEventKind(event.Kind()), approval: event.clone()})
	return nil
}

func (event SessionEvent) Kind() SessionEventKind { return event.kind }
func (event SessionEvent) RunError() (RunErrorEvent, bool) {
	return event.runError, event.kind == EventError
}
func (event SessionEvent) Subagent() (SubagentEvent, bool) {
	switch event.kind {
	case EventSubagentStart, EventSubagentEnd, EventSubagentRefused:
		return event.subagent, true
	}
	return SubagentEvent{}, false
}
func (event SessionEvent) Compaction() (CompactionEvent, bool) {
	return event.compact, event.kind == EventCompact
}
func (event SessionEvent) Stop() (ProviderStopEvent, bool) {
	switch event.kind {
	case SessionEventKind(EventTurnPaused), SessionEventKind(EventProviderRefusal), SessionEventKind(EventProviderStopUnhandled):
		return event.stop, true
	default:
		return ProviderStopEvent{}, false
	}
}
func (event SessionEvent) Todos() ([]protocol.TodoItem, bool) {
	if event.kind != EventTodo {
		return nil, false
	}
	return append([]protocol.TodoItem{}, event.todos...), true
}
func (event SessionEvent) clone() SessionEvent {
	event.todos = append([]protocol.TodoItem(nil), event.todos...)
	event.approval = event.approval.clone()
	return event
}

type EventSequence uint64

type SessionEventRecord struct {
	Sequence EventSequence
	Event    SessionEvent
	Scope    EventScope
}
type EventScope struct {
	Label      string
	Depth      int
	RunContext RunContext
}

func (scope EventScope) clone() EventScope { scope.RunContext = scope.RunContext.clone(); return scope }

type sessionEvents struct {
	mu      sync.Mutex
	next    EventSequence
	records []SessionEventRecord
	scope   EventScope
	parent  *sessionEvents
}

func (events *sessionEvents) setScope(scope EventScope) {
	events.mu.Lock()
	defer events.mu.Unlock()
	events.scope = scope.clone()
}

func (events *sessionEvents) append(event SessionEvent) {
	events.mu.Lock()
	scope := events.scope.clone()
	events.appendLocked(event, scope)
	events.mu.Unlock()
	if events.parent != nil {
		events.parent.appendScoped(event, scope)
	}
}
func (events *sessionEvents) appendScoped(event SessionEvent, scope EventScope) {
	events.mu.Lock()
	events.appendLocked(event, scope)
	events.mu.Unlock()
	if events.parent != nil {
		events.parent.appendScoped(event, scope)
	}
}
func (events *sessionEvents) appendLocked(event SessionEvent, scope EventScope) {
	events.next++
	if len(events.records) == EventBacklog {
		copy(events.records, events.records[1:])
		events.records = events.records[:len(events.records)-1]
	}
	events.records = append(events.records, SessionEventRecord{events.next, event.clone(), scope.clone()})
}
func (events *sessionEvents) snapshot() []SessionEventRecord {
	events.mu.Lock()
	defer events.mu.Unlock()
	records := make([]SessionEventRecord, len(events.records))
	for i, record := range events.records {
		records[i] = SessionEventRecord{record.Sequence, record.Event.clone(), record.Scope.clone()}
	}
	return records
}
