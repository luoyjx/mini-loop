package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const DefaultApprovalTimeout = 300 * time.Second
const ApprovalPreviewCap = 400
const GrantPrefixTokens = 2
const GrantProposalMaxTokens = 6
const grantRefusalReason = "prefix too broad to remember; this run was allowed, the next will ask again"

type ApprovalID string
type ApprovalKind string

const (
	ApprovalPermission ApprovalKind = "approval"
	ApprovalQuestion   ApprovalKind = "question"
)

type ApprovalStatus string

const (
	ApprovalPending      ApprovalStatus = "pending"
	ApprovalAllowed      ApprovalStatus = "allowed"
	ApprovalDenied       ApprovalStatus = "denied"
	ApprovalTimeout      ApprovalStatus = "timeout"
	ApprovalCancelled    ApprovalStatus = "cancelled"
	ApprovalExpired      ApprovalStatus = "expired"
	ApprovalAnswered     ApprovalStatus = "answered"
	ApprovalDeclined     ApprovalStatus = "declined"
	ApprovalGrantAllowed ApprovalStatus = "grant_allowed"
	ApprovalAutoAllowed  ApprovalStatus = "auto_allowed"
	ApprovalAutoDenied   ApprovalStatus = "auto_denied"
)

var grantBannedHeads = [...]string{"bash", "sh", "zsh", "dash", "ksh", "python", "python3", "node", "deno", "perl", "ruby", "php", "sudo", "doas", "su", "rm", "eval", "exec", "env", "xargs", "find", "curl", "wget", "nc", "chmod", "chown", "dd", "mkfs"}

// GrantCandidate is a closed tool-or-shell-prefix variant. The zero value is
// absent; accessors and serialization detach its token slice.
type GrantCandidate struct {
	tool   protocol.ToolName
	prefix []string
}

func (candidate GrantCandidate) Tokens() []string {
	if candidate.tool == "" {
		return nil
	}
	return append([]string{string(candidate.tool)}, candidate.prefix...)
}
func (candidate GrantCandidate) clone() GrantCandidate {
	candidate.prefix = append([]string(nil), candidate.prefix...)
	return candidate
}
func (candidate GrantCandidate) Banned() bool {
	return len(candidate.prefix) > 0 && slices.Contains(grantBannedHeads[:], candidate.prefix[0])
}
func (candidate GrantCandidate) MarshalJSON() ([]byte, error) {
	value, err := protocol.PythonJSON(candidate.Tokens(), false, true)
	return []byte(value), err
}
func shellTokens(command string) []string { return strings.FieldsFunc(command, pytext.IsSpace) }
func DefaultGrantCandidate(input protocol.ToolInput) GrantCandidate {
	if value, ok := input.Bash(); ok {
		tokens := shellTokens(value.Command)
		if len(tokens) < GrantPrefixTokens {
			return GrantCandidate{}
		}
		return GrantCandidate{input.Name(), append([]string(nil), tokens[:GrantPrefixTokens]...)}
	}
	return GrantCandidate{tool: input.Name()}
}
func ProposedGrantCandidate(input protocol.ToolInput) GrantCandidate {
	value, ok := input.Bash()
	if !ok || value.ApprovalPrefix == nil {
		return GrantCandidate{}
	}
	proposed, tokens := *value.ApprovalPrefix, shellTokens(value.Command)
	if len(proposed) < GrantPrefixTokens || len(proposed) > GrantProposalMaxTokens || len(tokens) < len(proposed) || !slices.Equal(proposed, tokens[:len(proposed)]) {
		return GrantCandidate{}
	}
	candidate := GrantCandidate{input.Name(), append([]string(nil), proposed...)}
	if candidate.Banned() {
		return GrantCandidate{}
	}
	return candidate
}

type ApprovalSnapshot struct {
	ApprovalID     ApprovalID        `json:"approval_id"`
	SessionID      SessionID         `json:"session_id"`
	Tool           protocol.ToolName `json:"tool"`
	ToolUseID      string            `json:"tool_use_id"`
	Rule           string            `json:"rule"`
	Message        string            `json:"message"`
	InputPreview   string            `json:"input_preview"`
	CreatedAt      float64           `json:"created_at"`
	Kind           ApprovalKind      `json:"kind"`
	GrantCandidate GrantCandidate    `json:"grant_candidate"`
	GrantProposed  bool              `json:"grant_proposed"`
}

func (snapshot ApprovalSnapshot) Clone() ApprovalSnapshot {
	snapshot.GrantCandidate = snapshot.GrantCandidate.clone()
	return snapshot
}

type ApprovalRecord struct {
	ApprovalID   ApprovalID        `json:"approval_id"`
	SessionID    SessionID         `json:"session_id"`
	ToolUseID    string            `json:"tool_use_id"`
	ToolName     protocol.ToolName `json:"tool_name"`
	Rule         string            `json:"rule"`
	Message      string            `json:"message"`
	InputPreview string            `json:"input_preview"`
	Status       ApprovalStatus    `json:"status"`
	CreatedAt    float64           `json:"created_at"`
	ResolvedAt   *float64          `json:"resolved_at"`
	Kind         ApprovalKind      `json:"kind"`
	Answer       *string           `json:"answer"`
}

func (record ApprovalRecord) Clone() ApprovalRecord {
	if record.ResolvedAt != nil {
		v := *record.ResolvedAt
		record.ResolvedAt = &v
	}
	if record.Answer != nil {
		v := *record.Answer
		record.Answer = &v
	}
	return record
}

type ApprovalStore interface {
	WriteApproval(context.Context, ApprovalRecord) error
}

// ApprovalRedactor masks concrete inputs before JSON escaping, and durable
// answers before storage. A nil redactor is the source Null-secrets path.
type ApprovalRedactor interface {
	MaskApprovalInput(protocol.ToolInput) protocol.ToolInput
	MaskText(string) string
}
type ReviewVerdict string

const (
	ReviewAbstain ReviewVerdict = "abstain"
	ReviewAllow   ReviewVerdict = "allow"
	ReviewDeny    ReviewVerdict = "deny"
)

type ApprovalPreviewer interface {
	ApprovalPreview(protocol.ToolInput) (string, error)
}

type ApprovalReviewer interface {
	ReviewApproval(context.Context, ApprovalRequest) (ReviewVerdict, error)
}
type ApprovalEventKind string

const (
	ApprovalRequiredEvent      ApprovalEventKind = "approval_required"
	ApprovalTimeoutEvent       ApprovalEventKind = "approval_timeout"
	ApprovalGrantUsedEvent     ApprovalEventKind = "approval_grant_used"
	ApprovalGrantRecordedEvent ApprovalEventKind = "approval_grant_recorded"
	ApprovalGrantRefusedEvent  ApprovalEventKind = "approval_grant_refused"
	ApprovalAutoReviewedEvent  ApprovalEventKind = "approval_auto_reviewed"
)

type ApprovalEvent struct {
	kind     ApprovalEventKind
	snapshot ApprovalSnapshot
	id       ApprovalID
	tool     protocol.ToolName
	rule     string
	waited   float64
	grant    GrantCandidate
	verdict  ReviewVerdict
}

func (event ApprovalEvent) Kind() ApprovalEventKind { return event.kind }
func (event ApprovalEvent) Required() (ApprovalSnapshot, bool) {
	return event.snapshot.Clone(), event.kind == ApprovalRequiredEvent
}
func (event ApprovalEvent) Timeout() (ApprovalID, protocol.ToolName, float64, bool) {
	return event.id, event.tool, event.waited, event.kind == ApprovalTimeoutEvent
}
func (event ApprovalEvent) Grant() (protocol.ToolName, string, GrantCandidate, string, bool) {
	reason := ""
	if event.kind == ApprovalGrantRefusedEvent {
		reason = grantRefusalReason
	}
	return event.tool, event.rule, event.grant.clone(), reason, event.kind == ApprovalGrantUsedEvent || event.kind == ApprovalGrantRecordedEvent || event.kind == ApprovalGrantRefusedEvent
}
func (event ApprovalEvent) AutoReviewed() (protocol.ToolName, string, ReviewVerdict, bool) {
	return event.tool, event.rule, event.verdict, event.kind == ApprovalAutoReviewedEvent
}
func (event ApprovalEvent) clone() ApprovalEvent {
	event.snapshot = event.snapshot.Clone()
	event.grant = event.grant.clone()
	return event
}

type ApprovalEventSink interface {
	EmitApproval(context.Context, ApprovalEvent) error
}

type ApprovalBrokerConfig struct {
	Timeout  time.Duration
	Store    ApprovalStore
	Redactor ApprovalRedactor
	Reviewer ApprovalReviewer
}
type ApprovalResolution struct {
	SessionID SessionID
	Allowed   bool
	Answer    *string
	Remember  bool
}
type grantOutcome string

const grantRecorded grantOutcome = "recorded"
const grantRefused grantOutcome = "refused_banned"

type pendingApproval struct {
	snapshot     ApprovalSnapshot
	done         chan struct{}
	settled      bool
	allowed      bool
	answer       *string
	grantOutcome grantOutcome
	redactor     ApprovalRedactor
}

// ApprovalBroker is process-local. Store faults are reported, but never change
// an in-memory human decision. Remembered grants are deliberately not persisted.
type ApprovalBroker struct {
	mu       sync.Mutex
	timeout  time.Duration
	store    ApprovalStore
	redactor ApprovalRedactor
	reviewer ApprovalReviewer
	pending  map[ApprovalID]*pendingApproval
	order    []ApprovalID
	grants   map[SessionID][]GrantCandidate
	problems []string
}

func NewApprovalBroker(config ApprovalBrokerConfig) (*ApprovalBroker, error) {
	if config.Timeout < 0 {
		return nil, errors.New("approval timeout cannot be negative")
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultApprovalTimeout
	}
	return &ApprovalBroker{timeout: config.Timeout, store: config.Store, redactor: config.Redactor, reviewer: config.Reviewer, pending: make(map[ApprovalID]*pendingApproval), grants: make(map[SessionID][]GrantCandidate)}, nil
}

// ApprovalSurface binds an existing session to its broker and event emitter.
// A child/foreign session cannot borrow that surface; fresh children have no
// broker-bound session state in the Python runtime either.
type ApprovalSurface struct {
	broker   *ApprovalBroker
	binding  ToolAuthority
	sink     ApprovalEventSink
	redactor ApprovalRedactor
}

func (broker *ApprovalBroker) ForSession(binding ToolAuthority, sink ApprovalEventSink) (*ApprovalSurface, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	binding.RunContext = binding.RunContext.clone()
	return &ApprovalSurface{broker: broker, binding: binding, sink: sink, redactor: broker.redactor}, nil
}
func (surface *ApprovalSurface) matches(authority ToolAuthority) bool {
	return authority.SessionID == surface.binding.SessionID && authority.OwnerID == surface.binding.OwnerID && authority.Workspace == surface.binding.Workspace
}
func (surface *ApprovalSurface) emit(ctx context.Context, event ApprovalEvent) error {
	if surface.sink == nil {
		return nil
	}
	return surface.sink.EmitApproval(ctx, event.clone())
}
func truncateApproval(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	seen := 0
	for offset := range value {
		if seen == limit {
			return value[:offset]
		}
		seen++
	}
	return value
}
func newApproval(snapshot ApprovalSnapshot) (*pendingApproval, error) {
	id, err := newMessageID()
	if err != nil {
		return nil, err
	}
	snapshot.ApprovalID = ApprovalID("apr_" + strings.TrimPrefix(string(id), "msg_")[:12])
	snapshot.CreatedAt = actionNow()
	return &pendingApproval{snapshot: snapshot, done: make(chan struct{})}, nil
}
func (broker *ApprovalBroker) problemLocked(value string) {
	if slices.Contains(broker.problems, value) {
		return
	}
	if len(broker.problems) == maxGateProblems {
		copy(broker.problems, broker.problems[1:])
		broker.problems = broker.problems[:len(broker.problems)-1]
	}
	broker.problems = append(broker.problems, value)
}
func (broker *ApprovalBroker) Problems() []string {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return append([]string(nil), broker.problems...)
}
func writeApproval(ctx context.Context, store ApprovalStore, record ApprovalRecord) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("approval store panic")
		}
	}()
	return store.WriteApproval(ctx, record.Clone())
}

// Called while the broker lock serializes publication and settlement. This
// preserves source event-loop ordering even when List/Resolve run concurrently.
func (broker *ApprovalBroker) persistLocked(pending *pendingApproval, status ApprovalStatus, answer *string) {
	if broker.store == nil {
		return
	}
	snapshot := pending.snapshot
	record := ApprovalRecord{ApprovalID: snapshot.ApprovalID, SessionID: snapshot.SessionID, ToolUseID: snapshot.ToolUseID, ToolName: snapshot.Tool, Rule: snapshot.Rule, Message: snapshot.Message, InputPreview: snapshot.InputPreview, Status: status, CreatedAt: snapshot.CreatedAt, Kind: snapshot.Kind}
	if status != ApprovalPending {
		now := actionNow()
		record.ResolvedAt = &now
	}
	if answer != nil {
		value := *answer
		if pending.redactor != nil {
			value = pending.redactor.MaskText(value)
		}
		record.Answer = &value
	}
	if err := writeApproval(context.Background(), broker.store, record); err != nil {
		broker.problemLocked(fmt.Sprintf("approval persistence failed (%T); a parked approval lost here restores as UNKNOWN, not NOT_RUN", err))
	}
}
func (broker *ApprovalBroker) grantedLocked(session SessionID, input protocol.ToolInput) (GrantCandidate, bool) {
	for _, grant := range broker.grants[session] {
		if grant.tool != input.Name() {
			continue
		}
		if value, ok := input.Bash(); ok {
			tokens := shellTokens(value.Command)
			if len(grant.prefix) > 0 && len(tokens) >= len(grant.prefix) && slices.Equal(tokens[:len(grant.prefix)], grant.prefix) {
				return grant.clone(), true
			}
		} else if len(grant.prefix) == 0 {
			return grant.clone(), true
		}
	}
	return GrantCandidate{}, false
}
func (broker *ApprovalBroker) Granted(session SessionID, input protocol.ToolInput) (GrantCandidate, bool) {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return broker.grantedLocked(session, input)
}
func reviewApproval(ctx context.Context, reviewer ApprovalReviewer, request ApprovalRequest) (verdict ReviewVerdict, err error) {
	defer func() {
		if recover() != nil {
			verdict, err = ReviewAbstain, errors.New("approval reviewer panic")
		}
	}()
	return reviewer.ReviewApproval(ctx, request)
}
func (surface *ApprovalSurface) newApproval(snapshot ApprovalSnapshot) (*pendingApproval, error) {
	pending, err := newApproval(snapshot)
	if err == nil {
		pending.redactor = surface.redactor
	}
	return pending, err
}

func (surface *ApprovalSurface) Approve(ctx context.Context, request ApprovalRequest) (bool, error) {
	if !surface.matches(request.Authority) {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := request.Call.Validate(); err != nil {
		return false, err
	}
	broker := surface.broker
	var preview string
	var err error
	if previewer, ok := surface.redactor.(ApprovalPreviewer); ok {
		preview, err = previewer.ApprovalPreview(request.Call.Input)
	} else {
		shown := request.Call.Input
		if surface.redactor != nil {
			shown = surface.redactor.MaskApprovalInput(shown)
		}
		preview, err = protocol.PythonJSON(shown, true, false)
	}
	if err != nil {
		return false, err
	}
	preview = truncateApproval(preview, ApprovalPreviewCap)
	snapshot := ApprovalSnapshot{SessionID: request.Authority.SessionID, Tool: request.Call.Name(), ToolUseID: request.Call.ID, Rule: request.Rule, Message: request.Message, InputPreview: preview, Kind: ApprovalPermission}
	broker.mu.Lock()
	hit, granted := broker.grantedLocked(snapshot.SessionID, request.Call.Input)
	if granted {
		snapshot.GrantCandidate = hit
		pending, err := surface.newApproval(snapshot)
		if err == nil {
			broker.persistLocked(pending, ApprovalGrantAllowed, nil)
		}
		broker.mu.Unlock()
		if err != nil {
			return false, err
		}
		return true, surface.emit(ctx, ApprovalEvent{kind: ApprovalGrantUsedEvent, tool: snapshot.Tool, rule: snapshot.Rule, grant: hit})
	}
	broker.mu.Unlock()
	proposal := ProposedGrantCandidate(request.Call.Input)
	snapshot.GrantCandidate = proposal
	snapshot.GrantProposed = proposal.tool != ""
	if !snapshot.GrantProposed {
		snapshot.GrantCandidate = DefaultGrantCandidate(request.Call.Input)
	}
	if broker.reviewer != nil {
		verdict, err := reviewApproval(ctx, broker.reviewer, request)
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err != nil || (verdict != ReviewAllow && verdict != ReviewDeny && verdict != ReviewAbstain && verdict != "") {
			if err == nil {
				err = errors.New("invalid reviewer verdict")
			}
			broker.mu.Lock()
			broker.problemLocked(fmt.Sprintf("auto-reviewer raised on %s: %T", snapshot.Tool, err))
			broker.mu.Unlock()
			verdict = ReviewAbstain
		}
		if verdict == ReviewAllow || verdict == ReviewDeny {
			// Source auto-review rows carry no proposed/remembered grant.
			auto := snapshot
			auto.GrantCandidate, auto.GrantProposed = GrantCandidate{}, false
			pending, err := surface.newApproval(auto)
			if err != nil {
				return false, err
			}
			status := ApprovalAutoDenied
			if verdict == ReviewAllow {
				status = ApprovalAutoAllowed
			}
			broker.mu.Lock()
			broker.persistLocked(pending, status, nil)
			broker.mu.Unlock()
			return verdict == ReviewAllow, surface.emit(ctx, ApprovalEvent{kind: ApprovalAutoReviewedEvent, tool: snapshot.Tool, rule: snapshot.Rule, verdict: verdict})
		}
	}
	pending, err := surface.newApproval(snapshot)
	if err != nil {
		return false, err
	}
	answer, err := surface.wait(ctx, pending)
	if err != nil {
		return false, err
	}
	if answer.allowed && answer.grantOutcome != "" {
		kind := ApprovalGrantRecordedEvent
		if answer.grantOutcome == grantRefused {
			kind = ApprovalGrantRefusedEvent
		}
		if err := surface.emit(ctx, ApprovalEvent{kind: kind, tool: snapshot.Tool, grant: snapshot.GrantCandidate}); err != nil {
			return false, err
		}
	}
	return answer.allowed, nil
}
func (surface *ApprovalSurface) AskQuestion(ctx context.Context, request QuestionRequest) (QuestionAnswer, error) {
	if !surface.matches(request.Authority) {
		return NoQuestionAnswer(), nil
	}
	if err := ctx.Err(); err != nil {
		return NoQuestionAnswer(), err
	}
	text := request.Question
	if surface.redactor != nil {
		text = surface.redactor.MaskText(text)
	}
	pending, err := surface.newApproval(ApprovalSnapshot{SessionID: request.Authority.SessionID, Tool: protocol.ToolAskUser, ToolUseID: request.Authority.ToolUseID, Rule: "ask-user", Message: truncateApproval(text, 2000), Kind: ApprovalQuestion})
	if err != nil {
		return NoQuestionAnswer(), err
	}
	result, err := surface.wait(ctx, pending)
	if err != nil {
		return NoQuestionAnswer(), err
	}
	if result.answer == nil {
		return NoQuestionAnswer(), nil
	}
	return AnswerQuestion(*result.answer), nil
}

type approvalAnswer struct {
	allowed      bool
	answer       *string
	grantOutcome grantOutcome
}

func (surface *ApprovalSurface) wait(ctx context.Context, pending *pendingApproval) (approvalAnswer, error) {
	broker := surface.broker
	id := pending.snapshot.ApprovalID
	broker.mu.Lock()
	broker.pending[id] = pending
	broker.order = append(broker.order, id)
	broker.persistLocked(pending, ApprovalPending, nil)
	broker.mu.Unlock()
	defer func() {
		broker.mu.Lock()
		delete(broker.pending, id)
		for i, key := range broker.order {
			if key == id {
				copy(broker.order[i:], broker.order[i+1:])
				broker.order[len(broker.order)-1] = ""
				broker.order = broker.order[:len(broker.order)-1]
				break
			}
		}
		broker.mu.Unlock()
	}()
	if err := surface.emit(ctx, ApprovalEvent{kind: ApprovalRequiredEvent, snapshot: pending.snapshot}); err != nil {
		return approvalAnswer{}, err
	}
	timer := time.NewTimer(broker.timeout)
	defer timer.Stop()
	select {
	case <-pending.done:
	case <-ctx.Done():
		broker.mu.Lock()
		if !pending.settled {
			pending.settled = true
			close(pending.done)
		}
		broker.mu.Unlock()
		return approvalAnswer{}, ctx.Err()
	case <-timer.C:
		broker.mu.Lock()
		if !pending.settled {
			pending.settled = true
			broker.persistLocked(pending, ApprovalTimeout, nil)
			close(pending.done)
			broker.mu.Unlock()
			return approvalAnswer{}, surface.emit(ctx, ApprovalEvent{kind: ApprovalTimeoutEvent, id: id, tool: pending.snapshot.Tool, waited: broker.timeout.Seconds()})
		}
		broker.mu.Unlock()
	}
	broker.mu.Lock()
	defer broker.mu.Unlock()
	result := approvalAnswer{allowed: pending.allowed, grantOutcome: pending.grantOutcome}
	if pending.answer != nil {
		value := *pending.answer
		result.answer = &value
	}
	return result, nil
}
func (broker *ApprovalBroker) List(session SessionID) []ApprovalSnapshot {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	result := []ApprovalSnapshot{}
	for _, id := range broker.order {
		pending := broker.pending[id]
		if pending.snapshot.SessionID == session {
			result = append(result, pending.snapshot.Clone())
		}
	}
	return result
}
func (broker *ApprovalBroker) Resolve(id ApprovalID, resolution ApprovalResolution) bool {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	pending, exists := broker.pending[id]
	if !exists || pending.snapshot.SessionID != resolution.SessionID || pending.settled {
		return false
	}
	pending.settled = true
	if pending.snapshot.Kind == ApprovalQuestion {
		status := ApprovalDeclined
		if resolution.Allowed && resolution.Answer != nil {
			value := *resolution.Answer
			pending.answer = &value
			status = ApprovalAnswered
		}
		broker.persistLocked(pending, status, pending.answer)
	} else {
		pending.allowed = resolution.Allowed
		candidate := pending.snapshot.GrantCandidate
		if pending.allowed && resolution.Remember && candidate.tool != "" {
			if candidate.Banned() {
				pending.grantOutcome = grantRefused
			} else {
				grants := broker.grants[resolution.SessionID]
				found := false
				for _, grant := range grants {
					if slices.Equal(grant.Tokens(), candidate.Tokens()) {
						found = true
						break
					}
				}
				if !found {
					broker.grants[resolution.SessionID] = append(grants, candidate.clone())
				}
				pending.grantOutcome = grantRecorded
			}
		}
		status := ApprovalDenied
		if pending.allowed {
			status = ApprovalAllowed
		}
		broker.persistLocked(pending, status, nil)
	}
	close(pending.done)
	return true
}
func (broker *ApprovalBroker) cancelSessionLocked(session SessionID) int {
	delete(broker.grants, session)
	count := 0
	for _, pending := range broker.pending {
		if pending.snapshot.SessionID == session && !pending.settled {
			pending.settled = true
			broker.persistLocked(pending, ApprovalCancelled, nil)
			close(pending.done)
			count++
		}
	}
	return count
}
func (broker *ApprovalBroker) CancelSession(session SessionID) int {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	return broker.cancelSessionLocked(session)
}
func (broker *ApprovalBroker) CancelAll() int {
	broker.mu.Lock()
	defer broker.mu.Unlock()
	count := 0
	for _, pending := range broker.pending {
		count += broker.cancelSessionLocked(pending.snapshot.SessionID)
	}
	return count
}
