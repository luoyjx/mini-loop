package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type ActionID string
type WorkflowRunID string
type InputHash string
type ActionStatus string

const (
	ActionStarted                ActionStatus = "started"
	ActionCompleted              ActionStatus = "completed"
	ActionFailed                 ActionStatus = "failed"
	ActionDenied                 ActionStatus = "denied"
	ActionCancelled              ActionStatus = "cancelled"
	ActionUnknown                ActionStatus = "unknown"
	MaxActionResultChars                      = 4000
	MaxDecisionActionResultChars              = 512 * 1024
	DefaultResultsRetained                    = 512
	MaxRetainedResultChars                    = DefaultResultsRetained * MaxActionResultChars
	ReportActionRecordsAbove                  = 50000
	ShedActionResult                          = "[result released; the action completed and was not re-run]"
	ReconciledActionResult                    = "[reconciled] This tool was dispatched before the process terminated, and a check confirms it already took effect. It has not been run again."
	NotRunActionResult                        = "[not run] This tool was awaiting approval when the process terminated. It was never executed and had no side effect. Ask again if it is still needed."
)

func (status ActionStatus) Terminal() bool {
	return status == ActionCompleted || status == ActionFailed || status == ActionDenied || status == ActionCancelled
}
func (status ActionStatus) settled() bool { return status.Terminal() || status == ActionUnknown }

type ActionRecord struct {
	ActionID      ActionID          `json:"action_id"`
	SessionID     SessionID         `json:"session_id"`
	MessageID     MessageID         `json:"message_id"`
	ToolUseID     string            `json:"tool_use_id"`
	ToolName      protocol.ToolName `json:"tool_name"`
	InputHash     InputHash         `json:"input_hash"`
	Status        ActionStatus      `json:"status"`
	Result        *string           `json:"result"`
	WorkflowRunID *WorkflowRunID    `json:"workflow_run_id"`
	CreatedAt     float64           `json:"created_at"`
	CompletedAt   *float64          `json:"completed_at"`
}

func (record ActionRecord) Clone() ActionRecord {
	if record.Result != nil {
		v := *record.Result
		record.Result = &v
	}
	if record.WorkflowRunID != nil {
		v := *record.WorkflowRunID
		record.WorkflowRunID = &v
	}
	if record.CompletedAt != nil {
		v := *record.CompletedAt
		record.CompletedAt = &v
	}
	return record
}

type ActionRequest struct {
	ActionID  ActionID
	SessionID SessionID
	MessageID MessageID
	ToolUseID string
	Input     protocol.ToolInput
}
type ActionSettlement struct {
	ActionID ActionID
	Status   ActionStatus
	Result   *string
}
type ActionJournal interface {
	Begin(context.Context, ActionRequest) (ActionRecord, error)
	Finish(context.Context, ActionSettlement) (ActionRecord, error)
	Get(context.Context, ActionID) (ActionRecord, bool, error)
	AttachWorkflow(context.Context, ActionID, WorkflowRunID) (ActionRecord, error)
}
type ActionReconciler interface {
	Reconcile(context.Context, ActionSettlement) (ActionRecord, error)
}

// ActionStore is the typed SQLite adapter boundary. A store owns persistence;
// StoredActionJournal supplies transitions, without promising cross-process claims.
type ActionStore interface {
	ReadAction(context.Context, ActionID) (ActionRecord, bool, error)
	WriteAction(context.Context, ActionRecord) error
	MarkInflightUnknown(context.Context, *SessionID) ([]ActionID, error)
}
type ActionJournalConflict struct {
	ActionID ActionID
	Detail   string
}

func (err *ActionJournalConflict) Error() string {
	return fmt.Sprintf("action %s %s", err.ActionID, err.Detail)
}

func ToolActionID(session SessionID, run RunContext, call ToolCall) (ActionID, error) {
	if call.ID == "" {
		id, err := newMessageID()
		return ActionID("act_" + strings.TrimPrefix(string(id), "msg_")), err
	}
	if err := run.Validate(); err != nil {
		return "", err
	}
	bytes := sha256.Sum256([]byte(strings.Join([]string{string(session), string(run.MessageID()), call.ID, string(call.Name())}, "\x00")))
	return ActionID("act_" + hex.EncodeToString(bytes[:])), nil
}
func actionCandidate(request ActionRequest) (ActionRecord, error) {
	if request.ActionID == "" || request.SessionID == "" || request.MessageID == "" || request.Input.Name() == "" {
		return ActionRecord{}, errors.New("action_id, session_id, message_id, and tool_name are required")
	}
	encoded, err := request.Input.CanonicalJSON()
	if err != nil {
		return ActionRecord{}, err
	}
	sum := sha256.Sum256([]byte(encoded))
	return ActionRecord{ActionID: request.ActionID, SessionID: request.SessionID, MessageID: request.MessageID, ToolUseID: request.ToolUseID, ToolName: request.Input.Name(), InputHash: InputHash(hex.EncodeToString(sum[:])), Status: ActionStarted, CreatedAt: actionNow()}, nil
}
func actionNow() float64 { return float64(time.Now().UnixNano()) / 1e9 }
func compareAction(existing, candidate ActionRecord) error {
	if existing.SessionID != candidate.SessionID || existing.MessageID != candidate.MessageID || existing.ToolUseID != candidate.ToolUseID || existing.ToolName != candidate.ToolName || existing.InputHash != candidate.InputHash {
		return &ActionJournalConflict{candidate.ActionID, "was replayed with a different payload"}
	}
	return nil
}
func boundActionResult(result *string, tool protocol.ToolName) *string {
	if result == nil {
		return nil
	}
	limit := MaxActionResultChars
	if tool == "decision" {
		limit = MaxDecisionActionResultChars
	}
	value := *result
	length := utf8.RuneCountInString(value)
	if length > limit {
		marker := fmt.Sprintf("\n[action result truncated; original_chars=%d]", length)
		keep := limit - utf8.RuneCountInString(marker)
		if keep < 0 {
			keep = 0
		}
		end, seen := len(value), 0
		for offset := range value {
			if seen == keep {
				end = offset
				break
			}
			seen++
		}
		value = value[:end] + marker
	}
	return &value
}
func settleAction(record ActionRecord, settlement ActionSettlement) ActionRecord {
	now := actionNow()
	record.Status, record.Result, record.CompletedAt = settlement.Status, boundActionResult(settlement.Result, record.ToolName), &now
	return record
}
func attachAction(record ActionRecord, run WorkflowRunID) (ActionRecord, error) {
	if record.WorkflowRunID != nil && *record.WorkflowRunID != run {
		return ActionRecord{}, &ActionJournalConflict{record.ActionID, "is already bound to a different workflow run"}
	}
	record.WorkflowRunID = &run
	return record, nil
}

// InMemoryActionJournal retains every identity; only terminal result payloads
// are shed. This is process-local, and a started record is not a dispatch claim.
type InMemoryActionJournal struct {
	mu            sync.Mutex
	records       map[ActionID]ActionRecord
	completed     []ActionID
	retainedChars int
	maxResults    int
	problems      []string
}

func NewInMemoryActionJournal(maxResults int) (*InMemoryActionJournal, error) {
	if maxResults < 0 {
		return nil, errors.New("result retention cannot be negative")
	}
	return &InMemoryActionJournal{records: make(map[ActionID]ActionRecord), maxResults: maxResults}, nil
}
func (journal *InMemoryActionJournal) Begin(ctx context.Context, request ActionRequest) (ActionRecord, error) {
	if err := ctx.Err(); err != nil {
		return ActionRecord{}, err
	}
	candidate, err := actionCandidate(request)
	if err != nil {
		return ActionRecord{}, err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if prior, exists := journal.records[request.ActionID]; exists {
		return prior.Clone(), compareAction(prior, candidate)
	}
	journal.records[request.ActionID] = candidate
	return candidate.Clone(), nil
}
func (journal *InMemoryActionJournal) Finish(ctx context.Context, settlement ActionSettlement) (ActionRecord, error) {
	if err := ctx.Err(); err != nil {
		return ActionRecord{}, err
	}
	if !settlement.Status.settled() {
		return ActionRecord{}, fmt.Errorf("unsupported action status: %s", settlement.Status)
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	prior, exists := journal.records[settlement.ActionID]
	if !exists {
		return ActionRecord{}, fmt.Errorf("action %s was not started", settlement.ActionID)
	}
	if prior.Status != ActionStarted {
		return prior.Clone(), nil
	}
	updated := settleAction(prior, settlement)
	journal.records[settlement.ActionID] = updated
	if updated.Result != nil {
		journal.retainedChars += utf8.RuneCountInString(*updated.Result)
	}
	journal.completed = append(journal.completed, settlement.ActionID)
	journal.shed()
	return updated.Clone(), nil
}
func (journal *InMemoryActionJournal) shed() {
	dropped := 0
	for len(journal.completed) > journal.maxResults || journal.retainedChars > MaxRetainedResultChars {
		id := journal.completed[0]
		journal.completed[0] = ""
		journal.completed = journal.completed[1:]
		record := journal.records[id]
		if record.Result == nil {
			continue
		}
		journal.retainedChars -= utf8.RuneCountInString(*record.Result)
		result := ShedActionResult
		record.Result = &result
		journal.records[id] = record
		dropped++
	}
	if dropped > 0 {
		journal.problem(fmt.Sprintf("released %d action result(s) to retain at most %d results and %d characters; status and identity are kept", dropped, journal.maxResults, MaxRetainedResultChars))
	}
	if len(journal.records) > ReportActionRecordsAbove {
		journal.problem(fmt.Sprintf("action journal holds %d records; it is not evicted because a replayed action that reads as absent runs twice", len(journal.records)))
	}
}
func (journal *InMemoryActionJournal) problem(value string) {
	for _, prior := range journal.problems {
		if prior == value {
			return
		}
	}
	if len(journal.problems) == maxGateProblems {
		copy(journal.problems, journal.problems[1:])
		journal.problems = journal.problems[:len(journal.problems)-1]
	}
	journal.problems = append(journal.problems, value)
}
func (journal *InMemoryActionJournal) Problems() []string {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return append([]string(nil), journal.problems...)
}
func (journal *InMemoryActionJournal) Get(ctx context.Context, id ActionID) (ActionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return ActionRecord{}, false, err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	record, exists := journal.records[id]
	return record.Clone(), exists, nil
}
func (journal *InMemoryActionJournal) AttachWorkflow(ctx context.Context, id ActionID, run WorkflowRunID) (ActionRecord, error) {
	if err := ctx.Err(); err != nil {
		return ActionRecord{}, err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	prior, exists := journal.records[id]
	if !exists {
		return ActionRecord{}, fmt.Errorf("action %s was not started", id)
	}
	updated, err := attachAction(prior, run)
	if err != nil {
		return ActionRecord{}, err
	}
	journal.records[id] = updated
	return updated.Clone(), nil
}

type StoredActionJournal struct {
	mu    sync.Mutex
	store ActionStore
}

func NewStoredActionJournal(store ActionStore) (*StoredActionJournal, error) {
	if store == nil {
		return nil, errors.New("action journal requires a store")
	}
	return &StoredActionJournal{store: store}, nil
}
func (journal *StoredActionJournal) Begin(ctx context.Context, request ActionRequest) (ActionRecord, error) {
	candidate, err := actionCandidate(request)
	if err != nil {
		return ActionRecord{}, err
	}
	journal.mu.Lock()
	defer journal.mu.Unlock()
	prior, exists, err := journal.store.ReadAction(ctx, request.ActionID)
	if err != nil {
		return ActionRecord{}, err
	}
	if exists {
		return prior.Clone(), compareAction(prior, candidate)
	}
	if err := journal.store.WriteAction(ctx, candidate.Clone()); err != nil {
		return ActionRecord{}, err
	}
	return candidate.Clone(), nil
}
func (journal *StoredActionJournal) Finish(ctx context.Context, settlement ActionSettlement) (ActionRecord, error) {
	if !settlement.Status.settled() {
		return ActionRecord{}, fmt.Errorf("unsupported action status: %s", settlement.Status)
	}
	return journal.transition(ctx, settlement, ActionStarted)
}
func (journal *StoredActionJournal) Reconcile(ctx context.Context, settlement ActionSettlement) (ActionRecord, error) {
	if !settlement.Status.Terminal() {
		return ActionRecord{}, fmt.Errorf("cannot reconcile to %q", settlement.Status)
	}
	return journal.transition(ctx, settlement, ActionUnknown)
}
func (journal *StoredActionJournal) transition(ctx context.Context, settlement ActionSettlement, from ActionStatus) (ActionRecord, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	prior, exists, err := journal.store.ReadAction(ctx, settlement.ActionID)
	if err != nil {
		return ActionRecord{}, err
	}
	if !exists {
		return ActionRecord{}, fmt.Errorf("action %s was not started", settlement.ActionID)
	}
	if prior.Status != from {
		return prior.Clone(), nil
	}
	updated := settleAction(prior, settlement)
	if err := journal.store.WriteAction(ctx, updated.Clone()); err != nil {
		return ActionRecord{}, err
	}
	return updated.Clone(), nil
}
func (journal *StoredActionJournal) Get(ctx context.Context, id ActionID) (ActionRecord, bool, error) {
	record, exists, err := journal.store.ReadAction(ctx, id)
	return record.Clone(), exists, err
}
func (journal *StoredActionJournal) AttachWorkflow(ctx context.Context, id ActionID, run WorkflowRunID) (ActionRecord, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	prior, exists, err := journal.store.ReadAction(ctx, id)
	if err != nil {
		return ActionRecord{}, err
	}
	if !exists {
		return ActionRecord{}, fmt.Errorf("action %s was not started", id)
	}
	updated, err := attachAction(prior, run)
	if err != nil {
		return ActionRecord{}, err
	}
	if err := journal.store.WriteAction(ctx, updated.Clone()); err != nil {
		return ActionRecord{}, err
	}
	return updated.Clone(), nil
}
func (journal *StoredActionJournal) MarkInflightUnknown(ctx context.Context, session *SessionID) ([]ActionID, error) {
	return journal.store.MarkInflightUnknown(ctx, session)
}
