package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

type SpanID string
type ActivityID string
type TextPhase string
type ModelStatus string

const (
	PhaseCommentary     TextPhase        = "commentary"
	PhaseFinalAnswer    TextPhase        = "final_answer"
	ModelCompleted      ModelStatus      = "completed"
	ModelCancelled      ModelStatus      = "cancelled"
	ModelError          ModelStatus      = "error"
	EventModelStart     SessionEventKind = "model_start"
	EventModelEnd       SessionEventKind = "model_end"
	EventAssistantText  SessionEventKind = "assistant_text"
	EventToolUse        SessionEventKind = "tool_use"
	EventToolResult     SessionEventKind = "tool_result"
	EventToolCatalog    SessionEventKind = "tool_catalog"
	EventSystemPrompt   SessionEventKind = "system_prompt"
	EventCapabilityPlan SessionEventKind = "capability_plan"
	EventActivityUpdate SessionEventKind = "activity_update"
	EventTurnQueued     SessionEventKind = "turn_queued"
	EventAssistantDelta SessionEventKind = "assistant_delta"
	EventReconcile      SessionEventKind = "reconcile"
	EventRecovery       SessionEventKind = "recovery"
)

const DisplayCap = 2000
const MaxLoggedFingerprints = 512

type ModelStartEvent struct {
	SpanID                                                    SpanID
	Purpose                                                   protocol.RequestPurpose
	Model                                                     string
	MessageCount, InputTokensEstimate, ToolCount, MaxTokens   int
	ToolCatalogFingerprint, SystemHash, CapabilityFingerprint *string
}
type ModelEndEvent struct {
	SpanID                 SpanID
	Purpose                protocol.RequestPurpose
	Status                 ModelStatus
	DurationMS             float64
	Error                  *string
	StopReason             *protocol.StopReason
	Usage                  *protocol.TokenUsage
	ServedModel            *string
	PromptTokens           *int
	ToolCatalogFingerprint *string
	TokenMeter             *TokenMeterSnapshot
}
type AssistantDeltaEvent struct{ Text string }

func (event SessionEvent) AssistantDelta() (AssistantDeltaEvent, bool) {
	return event.delta, event.kind == EventAssistantDelta
}

type AssistantTextEvent struct {
	Text  string
	Phase TextPhase
}
type ToolUseEvent struct {
	Name                 protocol.ToolName
	Input                protocol.ToolInput
	ID                   string
	SpanID, ParentSpanID SpanID
	ActionID             ActionID
	ActivityID           ActivityID
	Display              ToolDisplay
}
type ToolResultEvent struct {
	Name                     protocol.ToolName
	Output, ID               string
	SpanID, ParentSpanID     SpanID
	ActionID                 ActionID
	Failed, Denied, Replayed bool
	DurationMS               float64
	CommandResult            *shell.Metadata
}
type ToolCatalogEvent struct {
	Fingerprint string
	Schemas     []protocol.ToolSchema
}
type SystemPromptEvent struct {
	Hash   string
	System string
	Cache  *protocol.CacheControl
}
type CapabilityPlanEvent struct {
	Fingerprint, CatalogFingerprint string
	PermissionMode                  PermissionMode
	Sandbox                         string
	SandboxConfined                 bool
}
type ActivityUpdateEvent struct {
	ID    ActivityID
	Title string
}
type ReconcileEvent struct {
	Name       protocol.ToolName
	ActionID   ActionID
	Verdict    EffectVerdict
	Verifiable bool
}
type RecoveryAction string

const RecoveryFailed RecoveryAction = "failed"

type RecoveryEvent struct {
	Action RecoveryAction
	Error  string
}

func (event SessionEvent) Recovery() (RecoveryEvent, bool) {
	return event.recovery, event.kind == EventRecovery
}

type sessionModelProvider struct{ session *Session }

func (provider sessionModelProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	return provider.session.completeModel(ctx, request, nil)
}

func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func (event ModelStartEvent) clone() ModelStartEvent {
	event.ToolCatalogFingerprint = clonePointer(event.ToolCatalogFingerprint)
	event.SystemHash = clonePointer(event.SystemHash)
	event.CapabilityFingerprint = clonePointer(event.CapabilityFingerprint)
	return event
}
func (event ModelEndEvent) clone() ModelEndEvent {
	event.Error, event.ServedModel = clonePointer(event.Error), clonePointer(event.ServedModel)
	event.StopReason, event.PromptTokens = clonePointer(event.StopReason), clonePointer(event.PromptTokens)
	event.ToolCatalogFingerprint = clonePointer(event.ToolCatalogFingerprint)
	if event.Usage != nil {
		v := (protocol.ModelReply{Usage: *event.Usage}).Clone().Usage
		event.Usage = &v
	}
	if event.TokenMeter != nil {
		v := *event.TokenMeter
		v.AnchorTokens, v.AnchorEnvelope = clonePointer(v.AnchorTokens), clonePointer(v.AnchorEnvelope)
		event.TokenMeter = &v
	}
	return event
}
func (event ToolCatalogEvent) clone() ToolCatalogEvent {
	schemas := make([]protocol.ToolSchema, len(event.Schemas))
	for i, schema := range event.Schemas {
		schemas[i] = schema.Clone()
	}
	event.Schemas = schemas
	return event
}
func (event SessionEvent) ModelStart() (ModelStartEvent, bool) {
	return event.modelStart.clone(), event.kind == EventModelStart
}
func (event SessionEvent) ModelEnd() (ModelEndEvent, bool) {
	return event.modelEnd.clone(), event.kind == EventModelEnd
}
func (event SessionEvent) AssistantText() (AssistantTextEvent, bool) {
	return event.assistantText, event.kind == EventAssistantText
}
func (event SessionEvent) ToolUse() (ToolUseEvent, bool) {
	return event.toolUse, event.kind == EventToolUse
}
func (event SessionEvent) ToolResult() (ToolResultEvent, bool) {
	v := event.toolResult
	v.CommandResult = clonePointer(v.CommandResult)
	if v.CommandResult != nil {
		v.CommandResult.ExitCode = clonePointer(v.CommandResult.ExitCode)
	}
	return v, event.kind == EventToolResult
}
func (event SessionEvent) ToolCatalog() (ToolCatalogEvent, bool) {
	return event.toolCatalog.clone(), event.kind == EventToolCatalog
}
func (event SessionEvent) SystemPrompt() (SystemPromptEvent, bool) {
	v := event.systemPrompt
	v.Cache = clonePointer(v.Cache)
	return v, event.kind == EventSystemPrompt
}
func (event SessionEvent) CapabilityPlan() (CapabilityPlanEvent, bool) {
	return event.capabilityPlan, event.kind == EventCapabilityPlan
}
func (event SessionEvent) ActivityUpdate() (ActivityUpdateEvent, bool) {
	return event.activity, event.kind == EventActivityUpdate
}
func (event SessionEvent) Reconcile() (ReconcileEvent, bool) {
	return event.reconcile, event.kind == EventReconcile
}
func (event SessionEvent) Ephemeral() bool { return event.kind == EventAssistantDelta }

func newSpan(prefix string, size int) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:])[:size], nil
}
func digestText(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])[:16]
}
func rememberFingerprint(seen map[string]bool, value string) bool {
	if seen[value] {
		return false
	}
	if len(seen) >= MaxLoggedFingerprints {
		clear(seen)
	}
	seen[value] = true
	return true
}
func (s *Session) appendText(text string, phase TextPhase) {
	if text != "" {
		s.events.append(SessionEvent{kind: EventAssistantText, assistantText: AssistantTextEvent{text, phase}})
	}
}
func (s *Session) completeModel(ctx context.Context, request protocol.ModelRequest, catalog *ToolCatalogSnapshot) (protocol.ModelReply, error) {
	var fingerprint, capability *string
	if catalog != nil {
		v := catalog.Fingerprint()
		fingerprint = &v
		if rememberFingerprint(s.loggedCatalogs, v) {
			s.events.append(SessionEvent{kind: EventToolCatalog, toolCatalog: ToolCatalogEvent{v, catalog.Schemas()}})
		}
		plan := CapabilityPlanEvent{CatalogFingerprint: v, PermissionMode: s.mode, Sandbox: "NullSandbox"}
		if executor, ok := s.bash.(interface{ SandboxConfigured() bool }); ok && executor.SandboxConfigured() {
			plan.Sandbox = "GoSandbox"
		}
		encoded, err := protocol.PythonJSON(struct {
			Catalog  string         `json:"catalog_fingerprint"`
			Mode     PermissionMode `json:"permission_mode"`
			Sandbox  string         `json:"sandbox"`
			Confined bool           `json:"sandbox_confined"`
		}{plan.CatalogFingerprint, plan.PermissionMode, plan.Sandbox, plan.SandboxConfined}, true, false)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		plan.Fingerprint = digestText(encoded)
		capability = &plan.Fingerprint
		if rememberFingerprint(s.loggedCapabilities, plan.Fingerprint) {
			s.events.append(SessionEvent{kind: EventCapabilityPlan, capabilityPlan: plan})
		}
	}
	request, err := s.cachePolicy.Annotate(request)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, fmt.Errorf("model request: %w", err)
	}
	var systemHash *string
	if request.System != nil {
		system := *request.System
		canonical := system
		if request.Cache.System != nil {
			control := *request.Cache.System
			canonical, err = protocol.PythonJSON([]struct {
				Cache struct {
					TTL  protocol.CacheTTL  `json:"ttl,omitempty"`
					Type protocol.CacheType `json:"type"`
				} `json:"cache_control"`
				Text string             `json:"text"`
				Type protocol.BlockKind `json:"type"`
			}{{struct {
				TTL  protocol.CacheTTL  `json:"ttl,omitempty"`
				Type protocol.CacheType `json:"type"`
			}{control.TTL, control.Type}, system, protocol.BlockText}}, false, false)
			if err != nil {
				return protocol.ModelReply{}, err
			}
		}
		v := digestText(canonical)
		systemHash = &v
		if rememberFingerprint(s.loggedSystems, v) {
			s.events.append(SessionEvent{kind: EventSystemPrompt, systemPrompt: SystemPromptEvent{v, system, request.Cache.System}})
		}
	}
	id, err := newSpan("model_", 16)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	span := SpanID(id)
	s.lastModelSpan = span
	wire, err := request.Wire()
	if err != nil {
		return protocol.ModelReply{}, err
	}
	estimated, err := protocol.PythonJSON(wire.Messages, true, false)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	start := ModelStartEvent{span, request.Purpose, request.Model, len(request.Messages), len(estimated) / 4, len(request.Tools), request.MaxTokens, fingerprint, systemHash, capability}
	s.events.append(SessionEvent{kind: EventModelStart, modelStart: start})
	started := time.Now()
	reply, err := s.limitedComplete(ctx, request)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		err = reply.Validate()
	}
	end := ModelEndEvent{SpanID: span, Purpose: request.Purpose, DurationMS: float64(time.Since(started).Microseconds()) / 1000, ToolCatalogFingerprint: fingerprint}
	if err != nil {
		end.ToolCatalogFingerprint = nil
		end.Status = ModelError
		if ctx.Err() != nil {
			end.Status = ModelCancelled
		} else {
			detail := boundedError(err)
			end.Error = &detail
			s.events.append(SessionEvent{kind: EventRecovery, recovery: RecoveryEvent{RecoveryFailed, detail}})
		}
		s.events.append(SessionEvent{kind: EventModelEnd, modelEnd: end})
		return protocol.ModelReply{}, err
	}
	if request.Purpose == protocol.PurposeAgentTurn {
		s.meter.Observe(reply.Usage, s.messages, s.envelope)
	}
	end.Status, end.StopReason, end.Usage, end.ServedModel = ModelCompleted, &reply.StopReason, &reply.Usage, &reply.Model
	if prompt, ok := PromptTokens(reply.Usage); ok {
		end.PromptTokens = &prompt
	}
	meter := s.meter.Snapshot()
	end.TokenMeter = &meter
	s.events.append(SessionEvent{kind: EventModelEnd, modelEnd: end})
	return reply, nil
}
func boundedError(err error) string { return truncateRunes(fmt.Sprintf("%T: %v", err, err), 500) }
func truncateRunes(text string, cap int) string {
	runes := []rune(text)
	if len(runes) > cap {
		return string(runes[:cap])
	}
	return text
}
func (s *Session) dispatchTool(ctx context.Context, run RunContext, use protocol.ToolUseBlock) (ToolOutcome, error) {
	return s.dispatchToolAnnounced(ctx, run, use, nil)
}

func (s *Session) limitedComplete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	lease, err := s.modelLimiter.Acquire(ctx)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	defer lease.Release()
	return s.provider.Complete(ctx, request)
}

func (s *Session) dispatchToolAnnounced(ctx context.Context, run RunContext, use protocol.ToolUseBlock, announce func()) (ToolOutcome, error) {
	call := ToolCall{ID: use.ID, Input: use.Input}
	action, err := ToolActionID(s.id, run, call)
	if err != nil {
		return ToolOutcome{}, err
	}
	id, err := newSpan("tool_", 16)
	if err != nil {
		return ToolOutcome{}, err
	}
	span := SpanID(id)
	s.events.append(SessionEvent{kind: EventToolUse, toolUse: ToolUseEvent{use.Name, use.Input, use.ID, span, s.lastModelSpan, action, s.activityID, ToolLabel(use.Input)}})
	if announce != nil {
		announce()
	}
	started := time.Now()
	outcome, err := s.gate.dispatch(ctx, ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.workspace, Mode: s.mode, RunContext: run.clone()}, call, func(v ActionReconciliation) {
		s.events.append(SessionEvent{kind: EventReconcile, reconcile: ReconcileEvent{use.Input.Name(), v.ActionID, v.Verdict, v.Verifiable}})
	})
	if err != nil {
		return outcome, err
	}
	metadata, hasMetadata := outcome.CommandResult()
	var command *shell.Metadata
	if hasMetadata {
		command = &metadata
	}
	s.events.append(SessionEvent{kind: EventToolResult, toolResult: ToolResultEvent{use.Input.Name(), truncateRunes(outcome.Output, DisplayCap), use.ID, span, s.lastModelSpan, outcome.ActionID, outcome.Failed, outcome.Denied, outcome.Replayed, float64(time.Since(started).Microseconds()) / 1000, command}})
	return outcome, nil
}
