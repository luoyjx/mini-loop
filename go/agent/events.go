package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const EventBacklog = 200

type SessionEventKind string

const EventTodo SessionEventKind = "todo"
const EventCompact SessionEventKind = "compact"
const EventError SessionEventKind = "error"

type RunErrorKind string

const ErrorRoundExhaustion RunErrorKind = "round_exhaustion"
const ErrorProvider RunErrorKind = "provider"
const ErrorRuntime RunErrorKind = "runtime"

type RunErrorEvent struct {
	kind   RunErrorKind
	rounds int
	detail string
}

func (event RunErrorEvent) Kind() RunErrorKind { return event.kind }
func (event RunErrorEvent) Error() string {
	if event.detail != "" {
		return event.detail
	}
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

const EventStuck SessionEventKind = "stuck"

type StuckEvent struct {
	signal     StuckSignal
	halted     bool
	nudgesUsed int
}

func (event StuckEvent) Signal() StuckSignal { return event.signal.clone() }
func (event StuckEvent) Halted() bool        { return event.halted }
func (event StuckEvent) NudgesUsed() int     { return event.nudgesUsed }

// SessionEvent is a closed union. Accessors return detached values only
// for their corresponding variant; there is no untyped event payload.
type SessionEvent struct {
	sessionForked     SessionForkedEvent
	steeringDelivered SteeringDeliveredEvent
	postureUpdate     PostureUpdateEvent
	kind              SessionEventKind
	stop              ProviderStopEvent
	todos             []protocol.TodoItem
	compact           CompactionEvent
	subagent          SubagentEvent
	runError          RunErrorEvent
	approval          ApprovalEvent
	stuck             StuckEvent
	modelStart        ModelStartEvent
	modelEnd          ModelEndEvent
	assistantText     AssistantTextEvent
	delta             AssistantDeltaEvent
	streamStart       StreamStartEvent
	toolUse           ToolUseEvent
	toolResult        ToolResultEvent
	toolCatalog       ToolCatalogEvent
	systemPrompt      SystemPromptEvent
	capabilityPlan    CapabilityPlanEvent
	activity          ActivityUpdateEvent
	reconcile         ReconcileEvent
	status            StatusEvent
	done              DoneEvent
	cancelled         CancelledEvent
	recovery          RecoveryEvent
}

func (event SessionEvent) Stuck() (StuckEvent, bool) {
	value := event.stuck
	value.signal = value.signal.clone()
	return value, event.kind == EventStuck
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
	event.stuck.signal = event.stuck.signal.clone()
	event.modelStart = event.modelStart.clone()
	event.modelEnd = event.modelEnd.clone()
	event.toolCatalog = event.toolCatalog.clone()
	event.systemPrompt.Cache = clonePointer(event.systemPrompt.Cache)
	if event.toolResult.CommandResult != nil {
		v := event.toolResult.CommandResult.Clone()
		event.toolResult.CommandResult = &v
	}
	event.cancelled.RepairedToolUses = append([]string{}, event.cancelled.RepairedToolUses...)
	return event
}

type EventSequence uint64

type SessionEventRecord struct {
	Sequence        EventSequence
	Event           SessionEvent
	Scope           EventScope
	Timestamp       float64
	SessionID       SessionID
	TranscriptEpoch int
}
type EventScope struct {
	Label      string
	Depth      int
	RunContext RunContext
}

func (scope EventScope) clone() EventScope { scope.RunContext = scope.RunContext.clone(); return scope }

type sessionEvents struct {
	mu          sync.Mutex
	next        EventSequence
	records     []SessionEventRecord
	scope       EventScope
	parent      *sessionEvents
	secrets     TextMasker
	sessionID   SessionID
	epoch       int
	emitMu      sync.Mutex
	subscribers map[*EventSubscription]struct{}
	sink        EventSink
	sinkError   string
	history     func() []protocol.Message
	historyRefs []protocol.Message
}

func (events *sessionEvents) setScope(scope EventScope) {
	events.mu.Lock()
	defer events.mu.Unlock()
	events.scope = scope.clone()
}

func (events *sessionEvents) append(event SessionEvent) {
	events.emitMu.Lock()
	defer events.emitMu.Unlock()
	events.mu.Lock()
	scope := events.scope.clone()
	events.mu.Unlock()
	event, scope = maskedEvent(events.secrets, event), maskedScope(events.secrets, scope)
	events.mu.Lock()
	record := events.appendLocked(event, scope)
	events.mu.Unlock()
	events.notifySink(record)
	if events.parent != nil {
		events.parent.appendScoped(event, scope)
	}
}
func (events *sessionEvents) appendScoped(event SessionEvent, scope EventScope) {
	events.emitMu.Lock()
	defer events.emitMu.Unlock()
	events.mu.Lock()
	record := events.appendLocked(event, scope)
	events.mu.Unlock()
	events.notifySink(record)
	if events.parent != nil {
		events.parent.appendScoped(event, scope)
	}
}
func (events *sessionEvents) appendLocked(event SessionEvent, scope EventScope) SessionEventRecord {
	if !event.Ephemeral() && events.history != nil {
		history := events.history()
		rewritten := len(history) < len(events.historyRefs)
		if !rewritten {
			for i, old := range events.historyRefs {
				if history[i].Role != old.Role || !history[i].Content.SameStorage(old.Content) {
					rewritten = true
					break
				}
			}
		}
		if rewritten {
			events.epoch++
		}
		events.historyRefs = history
	}
	events.next++
	record := SessionEventRecord{Sequence: events.next, Event: event.clone(), Scope: scope.clone(), Timestamp: float64(time.Now().UnixMicro()) / 1e6, SessionID: SessionID(maskedText(events.secrets, string(events.sessionID))), TranscriptEpoch: events.epoch}
	if !event.Ephemeral() {
		if len(events.records) == EventBacklog {
			copy(events.records, events.records[1:])
			events.records = events.records[:len(events.records)-1]
		}
		events.records = append(events.records, record)
	}
	for subscriber := range events.subscribers {
		subscriber.offer(record.clone())
	}
	return record
}
func (events *sessionEvents) snapshot() []SessionEventRecord {
	events.mu.Lock()
	defer events.mu.Unlock()
	records := make([]SessionEventRecord, len(events.records))
	for i, record := range events.records {
		records[i] = record.clone()
	}
	return records
}
