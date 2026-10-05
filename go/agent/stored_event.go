package agent

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

// MaxStoredEventBytes bounds one archival JSON row, independently of a provider
// frame. A catalogue/system/transcript event can exceed the provider frame cap.
const MaxStoredEventBytes = 16 * 1024 * 1024

// UnmarshalJSON shares the archival boundary. Without this method ordinary JSON
// decoding would silently leave SessionEvent's private discriminator empty.
// A failed row never replaces an already decoded record.
func (record *SessionEventRecord) UnmarshalJSON(data []byte) error {
	next, err := DecodeStoredEvent(data)
	if err != nil {
		return err
	}
	*record = next
	return nil
}

// DecodeStoredEvent decodes the flat event projection into a known variant.
// It is an archival adapter, not a RunContext decoder. Historical provenance
// stays informational: no actor, capabilities, approval grant or live authority
// can be acquired by reading a row. Unsupported event types fail explicitly.
// Managed catch-up uses this adapter; decoding alone enables no state backend.
func DecodeStoredEvent(data []byte) (SessionEventRecord, error) {
	if len(data) > MaxStoredEventBytes {
		return SessionEventRecord{}, fmt.Errorf("stored event exceeds %d bytes", MaxStoredEventBytes)
	}
	var header eventHeader
	if err := json.Unmarshal(data, &header); err != nil {
		return SessionEventRecord{}, err
	}
	// duration_ms also belongs to model/tool payloads. It alone must not create
	// a trajectory terminal stamp through the embedded pointer in eventHeader.
	var terminalMarker struct {
		Status *TrajectoryStatus `json:"trajectory_status"`
	}
	if err := json.Unmarshal(data, &terminalMarker); err != nil {
		return SessionEventRecord{}, err
	}
	if terminalMarker.Status == nil {
		header.TrajectoryTerminal = nil
	}
	if header.Seq == 0 || header.Session == "" || header.Epoch < 1 || (header.Depth != nil && *header.Depth < 0) {
		return SessionEventRecord{}, fmt.Errorf("stored event requires positive sequence/epoch, session and nonnegative depth")
	}
	event, err := decodeStoredEventPayload(header.Type, data)
	if err != nil {
		return SessionEventRecord{}, err
	}
	scope := EventScope{Label: header.Agent, RunContext: RunContext{
		messageID: header.MessageID, parentMessageID: clonePointer(header.ParentMessageID),
		authority: AuthorityUntrusted, origin: "stored_event", channel: "storage", stampedBy: "state_store",
	}}
	if header.Depth != nil {
		scope.Depth = *header.Depth
	}
	return SessionEventRecord{Sequence: header.Seq, Timestamp: header.TS, SessionID: header.Session,
		TranscriptEpoch: header.Epoch, Scope: scope, Event: event,
		Trajectory: header.TrajectoryStamp, Terminal: header.TrajectoryTerminal}, nil
}

func storedPayload[T any](data []byte) (T, error) {
	var payload T
	err := json.Unmarshal(data, &payload)
	return payload, err
}

func storedGrant(tokens []string) GrantCandidate {
	if len(tokens) == 0 {
		return GrantCandidate{}
	}
	return GrantCandidate{tool: protocol.ToolName(tokens[0]), prefix: append([]string(nil), tokens[1:]...)}
}

func decodeStoredEventPayload(kind SessionEventKind, data []byte) (SessionEvent, error) {
	event := SessionEvent{kind: kind}
	switch kind {
	case EventGoalChange:
		v, err := storedPayload[GoalChangeEvent](data)
		if err != nil {
			return event, err
		}
		if err = v.Validate(); err != nil {
			return event, err
		}
		event.goalChange = v.Clone()
		return event, nil
	case EventPlanMode:
		v, err := storedPayload[struct {
			Active *bool `json:"active"`
		}](data)
		if err != nil {
			return event, err
		}
		if v.Active == nil {
			return event, errors.New("stored plan_mode requires active boolean")
		}
		event.planMode = PlanModeEvent{Active: *v.Active}
		return event, nil

	case EventTurnQueued:
		return event, nil
	case EventBackgroundResult:
		v, err := storedPayload[BackgroundResultEvent](data)
		event.backgroundResult = v
		return event, err
	case EventSessionForked:
		v, err := storedPayload[SessionForkedEvent](data)
		event.sessionForked = v
		return event, err
	case EventTrajectoryStart, EventTrajectoryEnd:
		v, err := storedPayload[struct {
			RunIndex int              `json:"run_index"`
			Status   TrajectoryStatus `json:"status"`
			Duration float64          `json:"duration_ms"`
		}](data)
		event.trajectory = TrajectoryLifecycle{RunIndex: v.RunIndex, Status: v.Status, DurationMS: v.Duration}
		return event, err
	case EventSteeringDelivered, EventPostureUpdate:
		v, err := storedPayload[struct {
			Count int    `json:"count"`
			Text  string `json:"text"`
		}](data)
		if kind == EventSteeringDelivered {
			event.steeringDelivered = SteeringDeliveredEvent{Count: v.Count, Text: v.Text}
		} else {
			event.postureUpdate = PostureUpdateEvent{Count: v.Count, Text: v.Text}
		}
		return event, err
	case EventStatus:
		v, err := storedPayload[struct {
			Status    SessionStatus `json:"status"`
			Cancelled bool          `json:"cancelled"`
		}](data)
		if err == nil && v.Status != StatusIdle && v.Status != StatusRunning && v.Status != StatusError {
			err = fmt.Errorf("unsupported stored session status %q", v.Status)
		}
		event.status = StatusEvent{Status: v.Status, Cancelled: v.Cancelled}
		return event, err
	case EventDone:
		v, err := storedPayload[struct {
			Text  string    `json:"text"`
			Phase TextPhase `json:"phase"`
		}](data)
		event.done = DoneEvent{Text: v.Text, Phase: v.Phase}
		return event, err
	case EventCancelled:
		v, err := storedPayload[struct {
			Reason   string   `json:"reason"`
			Repaired []string `json:"repaired_tool_uses"`
		}](data)
		event.cancelled = CancelledEvent{Reason: v.Reason, RepairedToolUses: v.Repaired}
		return event, err
	case EventError:
		v, err := storedPayload[struct {
			Error string `json:"error"`
		}](data)
		// The flat Python/Go projection stores the rendered error, not its cause.
		event.runError = RunErrorEvent{kind: ErrorRuntime, detail: v.Error}
		return event, err
	case EventAssistantText, EventAssistantDelta, EventStreamStart:
		v, err := storedPayload[struct {
			Text        string    `json:"text"`
			Phase       TextPhase `json:"phase"`
			StreamID    StreamID  `json:"stream_id"`
			Provisional bool      `json:"provisional"`
		}](data)
		switch kind {
		case EventAssistantText:
			event.assistantText = AssistantTextEvent{Text: v.Text, Phase: v.Phase, StreamID: v.StreamID}
		case EventAssistantDelta:
			event.delta = AssistantDeltaEvent{Text: v.Text, Phase: v.Phase, StreamID: v.StreamID, Provisional: v.Provisional}
		case EventStreamStart:
			event.streamStart = StreamStartEvent{Phase: v.Phase, StreamID: v.StreamID, Provisional: v.Provisional}
		}
		return event, err
	case EventActivityUpdate:
		v, err := storedPayload[struct {
			ID    ActivityID `json:"activity_id"`
			Title string     `json:"title"`
		}](data)
		event.activity = ActivityUpdateEvent{ID: v.ID, Title: v.Title}
		return event, err
	case EventToolCatalog:
		v, err := storedPayload[struct {
			Fingerprint string                `json:"fingerprint"`
			Schemas     []protocol.ToolSchema `json:"schemas"`
		}](data)
		event.toolCatalog = ToolCatalogEvent{Fingerprint: v.Fingerprint, Schemas: v.Schemas}
		return event, err
	case EventSystemPrompt:
		// text is a source string-or-block union, decoded only at this boundary.
		v, err := storedPayload[struct {
			Hash string          `json:"hash"`
			Text json.RawMessage `json:"text"`
		}](data)
		if err != nil {
			return SessionEvent{}, err
		}
		event.systemPrompt.Hash = v.Hash
		if len(v.Text) == 0 || string(v.Text) == "null" {
			return SessionEvent{}, fmt.Errorf("stored system prompt requires text")
		}
		if v.Text[0] == '"' {
			err = json.Unmarshal(v.Text, &event.systemPrompt.System)
		} else {
			var blocks []eventSystemBlock
			err = json.Unmarshal(v.Text, &blocks)
			if err == nil && (len(blocks) != 1 || blocks[0].Type != protocol.BlockText || blocks[0].Cache == nil) {
				err = fmt.Errorf("unsupported stored system prompt block shape")
			}
			if err == nil {
				event.systemPrompt.System, event.systemPrompt.Cache = blocks[0].Text, blocks[0].Cache
			}
		}
		return event, err
	case EventCapabilityPlan:
		v, err := storedPayload[struct {
			Fingerprint string         `json:"fingerprint"`
			Catalog     string         `json:"catalog_fingerprint"`
			Mode        PermissionMode `json:"permission_mode"`
			Sandbox     string         `json:"sandbox"`
			Confined    bool           `json:"sandbox_confined"`
		}](data)
		event.capabilityPlan = CapabilityPlanEvent{Fingerprint: v.Fingerprint, CatalogFingerprint: v.Catalog,
			PermissionMode: v.Mode, Sandbox: v.Sandbox, SandboxConfined: v.Confined}
		return event, err
	case EventModelStart:
		v, err := storedPayload[struct {
			Span       SpanID                  `json:"span_id"`
			Purpose    protocol.RequestPurpose `json:"purpose"`
			Model      string                  `json:"model"`
			Messages   int                     `json:"message_count"`
			Estimate   int                     `json:"input_tokens_estimate"`
			Tools      int                     `json:"tool_count"`
			Max        int                     `json:"max_tokens"`
			Catalog    *string                 `json:"tool_catalog_fingerprint"`
			System     *string                 `json:"system_hash"`
			Capability *string                 `json:"capability_fingerprint"`
		}](data)
		event.modelStart = ModelStartEvent{SpanID: v.Span, Purpose: v.Purpose, Model: v.Model,
			MessageCount: v.Messages, InputTokensEstimate: v.Estimate, ToolCount: v.Tools, MaxTokens: v.Max,
			ToolCatalogFingerprint: v.Catalog, SystemHash: v.System, CapabilityFingerprint: v.Capability}
		return event, err
	case EventModelEnd:
		v, err := storedPayload[struct {
			Span     SpanID                  `json:"span_id"`
			Purpose  protocol.RequestPurpose `json:"purpose"`
			Status   ModelStatus             `json:"status"`
			Duration float64                 `json:"duration_ms"`
			Error    *string                 `json:"error"`
			Stop     *protocol.StopReason    `json:"stop_reason"`
			Usage    *protocol.TokenUsage    `json:"usage"`
			Served   *string                 `json:"served_model"`
			Prompt   *int                    `json:"prompt_tokens"`
			Catalog  *string                 `json:"tool_catalog_fingerprint"`
			Meter    *TokenMeterSnapshot     `json:"token_meter"`
		}](data)
		event.modelEnd = ModelEndEvent{SpanID: v.Span, Purpose: v.Purpose, Status: v.Status, DurationMS: v.Duration,
			Error: v.Error, StopReason: v.Stop, Usage: v.Usage, ServedModel: v.Served, PromptTokens: v.Prompt,
			ToolCatalogFingerprint: v.Catalog, TokenMeter: v.Meter}
		return event, err
	case EventToolUse:
		v, err := storedPayload[struct {
			Name     protocol.ToolName `json:"name"`
			ID       string            `json:"id"`
			Input    json.RawMessage   `json:"input"`
			Span     SpanID            `json:"span_id"`
			Parent   SpanID            `json:"parent_span_id"`
			Action   ActionID          `json:"action_id"`
			Activity ActivityID        `json:"activity_id"`
			Display  struct {
				Verb   string `json:"verb"`
				Object string `json:"object"`
			} `json:"display"`
		}](data)
		if err != nil {
			return SessionEvent{}, err
		}
		input, err := protocol.DecodeToolInput(v.Name, v.Input)
		event.toolUse = ToolUseEvent{Name: v.Name, ID: v.ID, Input: input, SpanID: v.Span, ParentSpanID: v.Parent,
			ActionID: v.Action, ActivityID: v.Activity, Display: ToolDisplay{Verb: v.Display.Verb, Object: v.Display.Object}}
		return event, err
	case EventToolResult:
		v, err := storedPayload[struct {
			Name     protocol.ToolName `json:"name"`
			ID       string            `json:"id"`
			Output   string            `json:"output"`
			Span     SpanID            `json:"span_id"`
			Parent   SpanID            `json:"parent_span_id"`
			Action   ActionID          `json:"action_id"`
			Error    bool              `json:"error"`
			Denied   bool              `json:"denied"`
			Replayed bool              `json:"replayed"`
			Duration float64           `json:"duration_ms"`
			Command  *struct {
				Exit     *int  `json:"exit_code"`
				Timeout  bool  `json:"timed_out"`
				Overflow bool  `json:"overflowed"`
				Duration int64 `json:"duration_ms"`
			} `json:"command_result"`
		}](data)
		event.toolResult = ToolResultEvent{Name: v.Name, ID: v.ID, Output: v.Output, SpanID: v.Span, ParentSpanID: v.Parent,
			ActionID: v.Action, Failed: v.Error, Denied: v.Denied, Replayed: v.Replayed, DurationMS: v.Duration}
		if v.Command != nil {
			event.toolResult.CommandResult = &shell.Metadata{ExitCode: v.Command.Exit, TimedOut: v.Command.Timeout,
				Overflowed: v.Command.Overflow, DurationMS: v.Command.Duration}
		}
		return event, err
	case EventTodo:
		v, err := storedPayload[struct {
			Items []protocol.TodoItem `json:"items"`
		}](data)
		event.todos = v.Items
		return event, err
	case EventStuck:
		v, err := storedPayload[struct {
			Pattern StuckPattern       `json:"pattern"`
			Detail  string             `json:"detail"`
			Tool    *protocol.ToolName `json:"tool"`
			Halted  bool               `json:"halted"`
			Nudges  int                `json:"nudges_used"`
		}](data)
		event.stuck = StuckEvent{signal: StuckSignal{Pattern: v.Pattern, Detail: v.Detail, Tool: v.Tool}, halted: v.Halted, nudgesUsed: v.Nudges}
		return event, err
	case EventReconcile:
		v, err := storedPayload[struct {
			Name       protocol.ToolName `json:"name"`
			Action     ActionID          `json:"action_id"`
			Verdict    EffectVerdict     `json:"verdict"`
			Verifiable bool              `json:"verifiable"`
		}](data)
		event.reconcile = ReconcileEvent{Name: v.Name, ActionID: v.Action, Verdict: v.Verdict, Verifiable: v.Verifiable}
		return event, err
	case EventRecovery:
		v, err := storedPayload[RecoveryEvent](data)
		event.recovery = v
		return event, err
	case EventSubagentStart, EventSubagentEnd, EventSubagentRefused:
		v, err := storedPayload[struct {
			Role    AgentRole `json:"role"`
			Prompt  string    `json:"prompt"`
			Summary string    `json:"summary"`
			Depth   int       `json:"child_depth"`
			Limit   int       `json:"limit"`
		}](data)
		event.subagent = SubagentEvent{kind: kind, role: v.Role, prompt: v.Prompt, summary: v.Summary, childDepth: v.Depth, limit: v.Limit}
		return event, err
	case EventCompact:
		v, err := storedPayload[struct {
			Kind       CompactionKind `json:"kind"`
			Persisted  int            `json:"persisted"`
			Removed    int            `json:"removed"`
			Cleared    int            `json:"cleared"`
			Error      string         `json:"error"`
			Transcript string         `json:"transcript"`
			Messages   int            `json:"replaced_messages"`
			Tokens     int            `json:"replaced_tokens_estimate"`
			Input      int            `json:"summary_input_tokens"`
			Output     int            `json:"summary_output_tokens"`
			Model      string         `json:"summary_model"`
		}](data)
		if err != nil {
			return SessionEvent{}, err
		}
		event.compact.kind = v.Kind
		switch v.Kind {
		case CompactBudget:
			event.compact.count = v.Persisted
		case CompactSnip:
			event.compact.count = v.Removed
		case CompactMicro:
			event.compact.count = v.Cleared
		case CompactFailed:
			event.compact.failure = v.Error
		case CompactAuto:
			event.compact.summary = SummaryReceipt{Transcript: v.Transcript, ReplacedMessages: v.Messages, ReplacedTokensEstimate: v.Tokens, InputTokens: v.Input, OutputTokens: v.Output, Model: v.Model}
		default:
			return SessionEvent{}, fmt.Errorf("unsupported stored compaction kind %q", v.Kind)
		}
		return event, nil
	case SessionEventKind(ApprovalRequiredEvent):
		v, err := storedPayload[struct {
			ApprovalID   ApprovalID        `json:"approval_id"`
			SessionID    SessionID         `json:"session_id"`
			Tool         protocol.ToolName `json:"tool"`
			ToolUseID    string            `json:"tool_use_id"`
			Rule         string            `json:"rule"`
			Message      string            `json:"message"`
			InputPreview string            `json:"input_preview"`
			CreatedAt    float64           `json:"created_at"`
			Kind         ApprovalKind      `json:"kind"`
			Grant        []string          `json:"grant_candidate"`
			Proposed     bool              `json:"grant_proposed"`
		}](data)
		event.approval = ApprovalEvent{kind: ApprovalRequiredEvent, snapshot: ApprovalSnapshot{
			ApprovalID: v.ApprovalID, SessionID: v.SessionID, Tool: v.Tool, ToolUseID: v.ToolUseID, Rule: v.Rule,
			Message: v.Message, InputPreview: v.InputPreview, CreatedAt: v.CreatedAt, Kind: v.Kind,
			GrantCandidate: storedGrant(v.Grant), GrantProposed: v.Proposed}}
		return event, err
	case SessionEventKind(ApprovalTimeoutEvent):
		v, err := storedPayload[struct {
			ID     ApprovalID        `json:"approval_id"`
			Tool   protocol.ToolName `json:"tool"`
			Waited float64           `json:"waited"`
		}](data)
		event.approval = ApprovalEvent{kind: ApprovalTimeoutEvent, id: v.ID, tool: v.Tool, waited: v.Waited}
		return event, err
	case SessionEventKind(ApprovalGrantUsedEvent), SessionEventKind(ApprovalGrantRecordedEvent), SessionEventKind(ApprovalGrantRefusedEvent), SessionEventKind(ApprovalAutoReviewedEvent):
		v, err := storedPayload[struct {
			Tool    protocol.ToolName `json:"tool"`
			Rule    string            `json:"rule"`
			Grant   []string          `json:"grant"`
			Verdict ReviewVerdict     `json:"verdict"`
		}](data)
		event.approval = ApprovalEvent{kind: ApprovalEventKind(kind), tool: v.Tool, rule: v.Rule, grant: storedGrant(v.Grant), verdict: v.Verdict}
		return event, err
	case SessionEventKind(EventTurnPaused), SessionEventKind(EventProviderRefusal), SessionEventKind(EventProviderStopUnhandled):
		v, err := storedPayload[struct {
			Reason     protocol.StopReason `json:"stop_reason"`
			Resumption int                 `json:"resumption"`
			Detail     string              `json:"detail"`
		}](data)
		event.stop = ProviderStopEvent{kind: StopEventKind(kind), reason: v.Reason, resumption: v.Resumption, detail: v.Detail}
		return event, err
	default:
		return SessionEvent{}, fmt.Errorf("unsupported stored event kind %q", kind)
	}
}
