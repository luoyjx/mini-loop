package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const TrajectorySchema = "mini-loop.trajectory.v1"

type TrajectoryID string
type TrajectoryStatus string

const (
	TrajectoryRunning     TrajectoryStatus = "running"
	TrajectoryInterrupted TrajectoryStatus = "interrupted"
	TrajectoryCompleted   TrajectoryStatus = "completed"
	TrajectoryCancelled   TrajectoryStatus = "cancelled"
	TrajectoryError       TrajectoryStatus = "error"
	TrajectoryDisabled    TrajectoryStatus = "disabled"
)

type TrajectoryMetadata struct {
	Model     *string  `json:"model,omitempty"`
	Workspace *string  `json:"workspace,omitempty"`
	Build     *string  `json:"build,omitempty"`
	Agent     *string  `json:"agent,omitempty"`
	System    *string  `json:"system,omitempty"`
	Tools     []string `json:"tools,omitempty"`
}
type TrajectoryStart struct {
	Session  SessionID
	Owner    OwnerID
	RunIndex int
	Input    string
	Metadata TrajectoryMetadata
}
type TrajectoryFinish struct {
	Status     TrajectoryStatus
	Output     *string
	Error      *string
	DurationMS *float64
}
type TrajectoryMetrics struct {
	EventCount int `json:"event_count"`
	ModelCalls int `json:"model_calls"`
	ToolCalls  int `json:"tool_calls"`
	ToolErrors int `json:"tool_errors"`
	Errors     int `json:"errors"`
}
type TrajectorySummary struct {
	ID           TrajectoryID      `json:"id"`
	TrajectoryID TrajectoryID      `json:"trajectory_id"`
	TraceID      TrajectoryID      `json:"trace_id"`
	GroupID      SessionID         `json:"group_id"`
	Session      SessionID         `json:"session"`
	Owner        *OwnerID          `json:"owner"`
	RunIndex     int               `json:"run_index"`
	Status       TrajectoryStatus  `json:"status"`
	StartedAt    float64           `json:"started_at"`
	EndedAt      *float64          `json:"ended_at"`
	DurationMS   *float64          `json:"duration_ms"`
	Metrics      TrajectoryMetrics `json:"metrics"`
	Partial      bool              `json:"partial"`
	InputPreview *string           `json:"input_preview"`
	Model        *string           `json:"model"`
	Workspace    *string           `json:"workspace"`
	Build        *string           `json:"build"`
}
type TrajectoryQuery struct {
	Session *SessionID
	Limit   int
}

// Writer accepts masked, concrete records. Reader returns encoded JSON only at
// the serialization boundary; no dynamic event payload is retained by a service.
type TrajectoryWriter interface {
	Start(TrajectoryStart) (TrajectoryID, error)
	Append(TrajectoryID, TrajectoryRecord) error
	Finish(TrajectoryID, TrajectoryFinish) error
	Count(SessionID) (int, error)
}
type TrajectoryReader interface {
	List(TrajectoryQuery) ([]TrajectorySummary, error)
	Summary(TrajectoryID) (TrajectorySummary, error)
	JSON(TrajectoryID, int64) ([]byte, error)
	ByteSize(TrajectoryID) (int64, error)
	Stream(context.Context, TrajectoryID, io.Writer) error
	DeleteForSession(SessionID) (int, error)
}
type TrajectoryStore interface {
	TrajectoryWriter
	TrajectoryReader
}
type TrajectoryStamp struct {
	ID      *TrajectoryID `json:"trajectory_id"`
	TraceID *TrajectoryID `json:"trace_id,omitempty"`
	GroupID *SessionID    `json:"group_id,omitempty"`
}
type TrajectoryDisabledState struct {
	StatePersisted bool    `json:"state_persisted"`
	PersistError   *string `json:"persist_error"`
}
type TrajectoryTerminal struct {
	*TrajectoryDisabledState
	Status         TrajectoryStatus `json:"trajectory_status"`
	DurationMS     *float64         `json:"duration_ms,omitempty"`
	Persisted      *bool            `json:"trajectory_persisted,omitempty"`
	RecordingError *string          `json:"trajectory_recording_error"`
}

const EventTrajectoryStart SessionEventKind = "trajectory_start"
const EventTrajectoryEnd SessionEventKind = "trajectory_end"

type TrajectoryLifecycle struct {
	RunIndex   int
	Status     TrajectoryStatus
	DurationMS float64
}

// Private details are a closed variant, never part of replay or live SSE.
type trajectoryDetails struct {
	kind    SessionEventKind
	request protocol.ModelRequest
	reply   []protocol.Block
	text    string
}
type TrajectoryRecord struct {
	Record  SessionEventRecord
	details trajectoryDetails
	masker  TextMasker
}

func (record TrajectoryRecord) MarshalJSON() ([]byte, error) {
	live := record.Record
	switch record.details.kind {
	case EventToolResult:
		live.Event.toolResult.Output = record.details.text
	case EventSubagentStart:
		live.Event.subagent.prompt = record.details.text
	case EventSubagentEnd:
		live.Event.subagent.summary = record.details.text
	}
	payload, err := json.Marshal(live)
	if err != nil {
		return nil, err
	}
	// RawMessage is local to the encoded boundary, not a domain/service field.
	var members map[string]json.RawMessage
	if err = json.Unmarshal(payload, &members); err != nil {
		return nil, err
	}
	members["record_type"] = json.RawMessage(`"event"`)
	if record.Record.Terminal != nil {
		delete(members, "trajectory_persisted")
		delete(members, "trajectory_recording_error")
	}
	if record.details.kind == EventModelStart {
		input, err := record.details.request.Wire()
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(encoded, &fields); err != nil {
			return nil, err
		}
		delete(fields, "model")
		encoded, err = json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		members["model_input"] = encoded
	}
	if record.details.kind == EventModelEnd {
		encoded, err := json.Marshal(record.details.reply)
		if err != nil {
			return nil, err
		}
		members["model_output"] = encoded
	}
	payload, err = json.Marshal(members)
	if err != nil {
		return nil, err
	}
	if record.masker != nil {
		masked, err := protocol.MaskedPythonJSON(json.RawMessage(payload), record.masker.MaskText, false, true)
		return []byte(masked), err
	}
	return payload, nil
}

type trajectoryRun struct {
	mu             sync.Mutex
	store          TrajectoryWriter
	masker         TextMasker
	active         *TrajectoryID
	count          int
	recordingError *string
	hadError       bool
	started        time.Time
}

func trajectoryFault(action func() error) (err error) {
	defer func() {
		if fault := recover(); fault != nil {
			err = fmt.Errorf("trajectory backend panic: %T", fault)
		}
	}()
	return action()
}
func (run *trajectoryRun) fail(err error) {
	if err != nil {
		text := maskedText(run.masker, boundedError(err))
		run.mu.Lock()
		run.recordingError = &text
		run.mu.Unlock()
	}
}
func (run *trajectoryRun) snapshot() (*TrajectoryID, int, *string) {
	run.mu.Lock()
	defer run.mu.Unlock()
	return clonePointer(run.active), run.count, clonePointer(run.recordingError)
}
func (run *trajectoryRun) begin(start TrajectoryStart) {
	run.mu.Lock()
	run.active = nil
	run.recordingError = nil
	run.hadError = false
	run.started = time.Now()
	run.mu.Unlock()
	if run.store == nil {
		return
	}
	var id TrajectoryID
	err := trajectoryFault(func() error { var err error; id, err = run.store.Start(start); return err })
	if err == nil && id == "" {
		err = errors.New("trajectory store returned an empty id")
	}
	if err != nil {
		run.fail(err)
		return
	}
	run.mu.Lock()
	run.active = &id
	run.count++
	run.mu.Unlock()
}
func (run *trajectoryRun) capture(record *SessionEventRecord, details trajectoryDetails, masker TextMasker) {
	if record.Event.Ephemeral() {
		return
	}
	run.mu.Lock()
	id := clonePointer(run.active)
	if record.Event.kind == EventError {
		run.hadError = true
	}
	run.mu.Unlock()
	if id == nil {
		return
	}
	session := record.SessionID
	record.Trajectory = &TrajectoryStamp{ID: id, TraceID: clonePointer(id), GroupID: &session}
	run.fail(trajectoryFault(func() error {
		return run.store.Append(*id, TrajectoryRecord{Record: *record, details: details, masker: masker})
	}))
}
func (run *trajectoryRun) duration() float64 {
	run.mu.Lock()
	defer run.mu.Unlock()
	return float64(time.Since(run.started).Microseconds()) / 1000
}
func (run *trajectoryRun) outcome(status TrajectoryStatus) TrajectoryStatus {
	run.mu.Lock()
	defer run.mu.Unlock()
	if status == TrajectoryCompleted && run.hadError {
		return TrajectoryError
	}
	return status
}
func (run *trajectoryRun) finish(record *SessionEventRecord, finish TrajectoryFinish) {
	id, _, _ := run.snapshot()
	if id == nil {
		record.Trajectory = &TrajectoryStamp{}
		record.Terminal = &TrajectoryTerminal{Status: TrajectoryDisabled, TrajectoryDisabledState: &TrajectoryDisabledState{StatePersisted: true}}
		_, _, record.Terminal.RecordingError = run.snapshot()
		return
	}
	run.fail(trajectoryFault(func() error { return run.store.Finish(*id, finish) }))
	run.mu.Lock()
	run.active = nil
	fault := clonePointer(run.recordingError)
	run.mu.Unlock()
	persisted := fault == nil
	record.Terminal = &TrajectoryTerminal{Status: finish.Status, DurationMS: finish.DurationMS, Persisted: &persisted, RecordingError: fault}
}

func (session *ManagedSession) beginTrajectory(prompt string) {
	run := session.core.events.trajectory
	if run.store == nil {
		run.begin(TrajectoryStart{})
		return
	}
	metadata := TrajectoryMetadata{}
	mask := func(text string) *string { value := maskedText(session.core.secrets, text); return &value }
	metadata.Model = mask(session.core.model)
	metadata.Workspace = mask(session.core.workspace)
	metadata.Build = mask(session.build)
	metadata.Agent = mask(session.core.label)
	// Build metadata through the same system seam, without applying prompt hooks or
	// admitting a second turn. A failure degrades recording, not the requested run.
	request, _, err := session.core.buildRequest()
	if err != nil {
		run.mu.Lock()
		run.active = nil
		run.recordingError = nil
		run.hadError = false
		run.started = time.Now()
		run.mu.Unlock()
		run.fail(err)
		return
	}
	if request.System != nil {
		metadata.System = mask(*request.System)
	}
	metadata.Tools = make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		metadata.Tools = append(metadata.Tools, maskedText(session.core.secrets, string(tool.Name)))
	}
	session.mu.Lock()
	index := session.runCount
	session.mu.Unlock()
	run.begin(TrajectoryStart{Session: session.ID(), Owner: session.Owner(), RunIndex: index, Input: maskedText(session.core.secrets, prompt), Metadata: metadata})
	id, _, _ := run.snapshot()
	if id != nil {
		session.emitFor(RunContext{}, SessionEvent{kind: EventTrajectoryStart, trajectory: TrajectoryLifecycle{RunIndex: index}})
	}
}
func (session *ManagedSession) finishTrajectory(runContext RunContext, event SessionEvent, status TrajectoryStatus, output, detail *string) {
	run := session.core.events.trajectory
	status = run.outcome(status)
	duration := run.duration()
	id, _, _ := run.snapshot()
	if id != nil {
		session.emitFor(runContext, SessionEvent{kind: EventTrajectoryEnd, trajectory: TrajectoryLifecycle{Status: status, DurationMS: duration}})
	}
	if output != nil {
		value := maskedText(session.core.secrets, *output)
		output = &value
	}
	if detail != nil {
		value := maskedText(session.core.secrets, *detail)
		detail = &value
	}
	event = maskedEvent(session.core.secrets, event)
	finish := TrajectoryFinish{Status: status, Output: output, Error: detail, DurationMS: &duration}
	session.core.events.appendScopedRecorded(event, EventScope{}, trajectoryDetails{}, &finish)
}
