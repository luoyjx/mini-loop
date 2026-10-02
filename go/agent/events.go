package agent

import (
	"github.com/luoyjx/mini-loop/go/protocol"
	"sync"
)

const EventBacklog = 200

type SessionEventKind string

const EventTodo SessionEventKind = "todo"

// SessionEvent is a closed union. Stop and Todo return detached values only
// for their corresponding variant; there is no untyped event payload.
type SessionEvent struct {
	kind  SessionEventKind
	stop  ProviderStopEvent
	todos []protocol.TodoItem
}

func (event SessionEvent) Kind() SessionEventKind { return event.kind }
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
	return event
}

type EventSequence uint64

type SessionEventRecord struct {
	Sequence EventSequence
	Event    SessionEvent
}

type sessionEvents struct {
	mu      sync.Mutex
	next    EventSequence
	records []SessionEventRecord
}

func (events *sessionEvents) append(event SessionEvent) {
	events.mu.Lock()
	defer events.mu.Unlock()
	events.next++
	if len(events.records) == EventBacklog {
		copy(events.records, events.records[1:])
		events.records = events.records[:len(events.records)-1]
	}
	events.records = append(events.records, SessionEventRecord{events.next, event.clone()})
}
func (events *sessionEvents) snapshot() []SessionEventRecord {
	events.mu.Lock()
	defer events.mu.Unlock()
	records := make([]SessionEventRecord, len(events.records))
	for i, record := range events.records {
		records[i] = SessionEventRecord{record.Sequence, record.Event.clone()}
	}
	return records
}
