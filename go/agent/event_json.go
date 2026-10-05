package agent

import (
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type eventSystemBlock struct {
	Type  protocol.BlockKind     `json:"type"`
	Text  string                 `json:"text"`
	Cache *protocol.CacheControl `json:"cache_control"`
}

type eventHeader struct {
	*TrajectoryStamp
	*TrajectoryTerminal
	Type            SessionEventKind `json:"type"`
	Seq             EventSequence    `json:"seq"`
	TS              float64          `json:"ts"`
	Session         SessionID        `json:"session"`
	Epoch           int              `json:"transcript_epoch"`
	Agent           string           `json:"agent,omitempty"`
	Depth           *int             `json:"depth,omitempty"`
	MessageID       MessageID        `json:"message_id,omitempty"`
	ParentMessageID *MessageID       `json:"parent_message_id,omitempty"`
	Ephemeral       bool             `json:"ephemeral,omitempty"`
}

// Both objects come from concrete typed encoders. Joining their member bytes
// preserves the flat Python event shape without a generic payload in the domain.
func marshalEvent[T any](header eventHeader, payload T) ([]byte, error) {
	h, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if len(p) < 2 || p[0] != '{' || p[len(p)-1] != '}' {
		return nil, fmt.Errorf("event payload must encode an object")
	}
	if len(p) == 2 {
		return h, nil
	}
	h[len(h)-1] = ','
	return append(h, p[1:]...), nil
}

func (record SessionEventRecord) MarshalJSON() ([]byte, error) {
	e := record.Event
	h := eventHeader{TrajectoryStamp: record.Trajectory, TrajectoryTerminal: record.Terminal, Type: e.kind, Seq: record.Sequence, TS: record.Timestamp, Session: record.SessionID, Epoch: record.TranscriptEpoch, Ephemeral: e.Ephemeral()}
	if record.Scope.Label != "" {
		depth := record.Scope.Depth
		h.Agent = record.Scope.Label
		h.Depth = &depth
		h.MessageID = record.Scope.RunContext.MessageID()
		h.ParentMessageID = record.Scope.RunContext.Snapshot().ParentMessageID
	}
	switch e.kind {
	case EventBackgroundResult:
		return marshalEvent(h, e.backgroundResult)
	case EventTrajectoryStart:
		return marshalEvent(h, struct {
			RunIndex int `json:"run_index"`
		}{e.trajectory.RunIndex})
	case EventTrajectoryEnd:
		return marshalEvent(h, struct {
			Status     TrajectoryStatus `json:"status"`
			DurationMS float64          `json:"duration_ms"`
		}{e.trajectory.Status, e.trajectory.DurationMS})
	case EventSessionForked:
		return marshalEvent(h, e.sessionForked)
	case EventSteeringDelivered:
		return marshalEvent(h, struct {
			Count int    `json:"count"`
			Text  string `json:"text"`
		}{e.steeringDelivered.Count, e.steeringDelivered.Text})
	case EventPostureUpdate:
		return marshalEvent(h, struct {
			Count int    `json:"count"`
			Text  string `json:"text"`
		}{e.postureUpdate.Count, e.postureUpdate.Text})
	case EventStatus:
		return marshalEvent(h, struct {
			Status    SessionStatus `json:"status"`
			Cancelled bool          `json:"cancelled,omitempty"`
		}{e.status.Status, e.status.Cancelled})
	case EventDone:
		return marshalEvent(h, struct {
			Text  string    `json:"text"`
			Phase TextPhase `json:"phase"`
		}{e.done.Text, e.done.Phase})
	case EventCancelled:
		v, _ := e.Cancelled()
		return marshalEvent(h, struct {
			Reason   string   `json:"reason"`
			Repaired []string `json:"repaired_tool_uses"`
		}{v.Reason, v.RepairedToolUses})
	case EventError:
		return marshalEvent(h, struct {
			Error string `json:"error"`
		}{e.runError.Error()})
	case EventAssistantText:
		return marshalEvent(h, struct {
			Text     string    `json:"text"`
			Phase    TextPhase `json:"phase"`
			StreamID StreamID  `json:"stream_id,omitempty"`
		}{e.assistantText.Text, e.assistantText.Phase, e.assistantText.StreamID})
	case EventAssistantDelta:
		return marshalEvent(h, struct {
			Text        string    `json:"text"`
			StreamID    StreamID  `json:"stream_id,omitempty"`
			Phase       TextPhase `json:"phase,omitempty"`
			Provisional bool      `json:"provisional,omitempty"`
		}{e.delta.Text, e.delta.StreamID, e.delta.Phase, e.delta.Provisional})
	case EventStreamStart:
		return marshalEvent(h, struct {
			StreamID    StreamID  `json:"stream_id"`
			Phase       TextPhase `json:"phase"`
			Provisional bool      `json:"provisional"`
		}{e.streamStart.StreamID, e.streamStart.Phase, e.streamStart.Provisional})
	case EventTurnQueued:
		return marshalEvent(h, struct{}{})
	case EventActivityUpdate:
		return marshalEvent(h, struct {
			ID          ActivityID `json:"activity_id"`
			Title       string     `json:"title"`
			Source      string     `json:"source"`
			Provisional bool       `json:"provisional"`
		}{e.activity.ID, e.activity.Title, "commentary", false})
	case EventToolCatalog:
		return marshalEvent(h, struct {
			Fingerprint string                `json:"fingerprint"`
			Schemas     []protocol.ToolSchema `json:"schemas"`
		}{e.toolCatalog.Fingerprint, e.toolCatalog.Schemas})
	case EventCapabilityPlan:
		v := e.capabilityPlan
		return marshalEvent(h, struct {
			Fingerprint string         `json:"fingerprint"`
			Catalog     string         `json:"catalog_fingerprint"`
			Mode        PermissionMode `json:"permission_mode"`
			Sandbox     string         `json:"sandbox"`
			Confined    bool           `json:"sandbox_confined"`
		}{v.Fingerprint, v.CatalogFingerprint, v.PermissionMode, v.Sandbox, v.SandboxConfined})
	case EventSystemPrompt:
		if e.systemPrompt.Cache == nil {
			return marshalEvent(h, struct {
				Hash string `json:"hash"`
				Text string `json:"text"`
			}{e.systemPrompt.Hash, e.systemPrompt.System})
		}
		return marshalEvent(h, struct {
			Hash string             `json:"hash"`
			Text []eventSystemBlock `json:"text"`
		}{e.systemPrompt.Hash, []eventSystemBlock{{Type: protocol.BlockText, Text: e.systemPrompt.System, Cache: e.systemPrompt.Cache}}})
	case EventModelStart:
		v := e.modelStart
		return marshalEvent(h, struct {
			Span       SpanID                  `json:"span_id"`
			Purpose    protocol.RequestPurpose `json:"purpose"`
			Model      string                  `json:"model"`
			Messages   int                     `json:"message_count"`
			Estimate   int                     `json:"input_tokens_estimate"`
			Tools      int                     `json:"tool_count"`
			Max        int                     `json:"max_tokens"`
			Catalog    *string                 `json:"tool_catalog_fingerprint,omitempty"`
			System     *string                 `json:"system_hash,omitempty"`
			Capability *string                 `json:"capability_fingerprint,omitempty"`
		}{v.SpanID, v.Purpose, v.Model, v.MessageCount, v.InputTokensEstimate, v.ToolCount, v.MaxTokens, v.ToolCatalogFingerprint, v.SystemHash, v.CapabilityFingerprint})
	case EventModelEnd:
		v := e.modelEnd
		return marshalEvent(h, struct {
			Span     SpanID                  `json:"span_id"`
			Purpose  protocol.RequestPurpose `json:"purpose"`
			Status   ModelStatus             `json:"status"`
			Duration float64                 `json:"duration_ms"`
			Error    *string                 `json:"error,omitempty"`
			Stop     *protocol.StopReason    `json:"stop_reason,omitempty"`
			Usage    *protocol.TokenUsage    `json:"usage,omitempty"`
			Served   *string                 `json:"served_model,omitempty"`
			Prompt   *int                    `json:"prompt_tokens,omitempty"`
			Catalog  *string                 `json:"tool_catalog_fingerprint,omitempty"`
			Meter    *TokenMeterSnapshot     `json:"token_meter,omitempty"`
		}{v.SpanID, v.Purpose, v.Status, v.DurationMS, v.Error, v.StopReason, v.Usage, v.ServedModel, v.PromptTokens, v.ToolCatalogFingerprint, v.TokenMeter})
	case EventToolUse:
		v := e.toolUse
		return marshalEvent(h, struct {
			Name     protocol.ToolName  `json:"name"`
			ID       string             `json:"id"`
			Input    protocol.ToolInput `json:"input"`
			Span     SpanID             `json:"span_id"`
			Parent   SpanID             `json:"parent_span_id"`
			Action   ActionID           `json:"action_id"`
			Activity ActivityID         `json:"activity_id,omitempty"`
			Display  struct {
				Verb   string `json:"verb"`
				Object string `json:"object"`
			} `json:"display"`
		}{v.Name, v.ID, v.Input, v.SpanID, v.ParentSpanID, v.ActionID, v.ActivityID, struct {
			Verb   string `json:"verb"`
			Object string `json:"object"`
		}{v.Display.Verb, v.Display.Object}})
	case EventToolResult:
		v := e.toolResult
		type commandMetadata struct {
			Exit     *int  `json:"exit_code"`
			Timeout  bool  `json:"timed_out"`
			Overflow bool  `json:"overflowed"`
			Duration int64 `json:"duration_ms"`
		}
		var cmd *commandMetadata
		if v.CommandResult != nil {
			c := v.CommandResult
			cmd = &commandMetadata{c.ExitCode, c.TimedOut, c.Overflowed, c.DurationMS}
		}
		return marshalEvent(h, struct {
			Name     protocol.ToolName `json:"name"`
			ID       string            `json:"id"`
			Output   string            `json:"output"`
			Span     SpanID            `json:"span_id"`
			Parent   SpanID            `json:"parent_span_id"`
			Action   ActionID          `json:"action_id"`
			Error    bool              `json:"error"`
			Denied   bool              `json:"denied,omitempty"`
			Replay   bool              `json:"replayed,omitempty"`
			Duration float64           `json:"duration_ms"`
			Command  *commandMetadata  `json:"command_result,omitempty"`
		}{v.Name, v.ID, v.Output, v.SpanID, v.ParentSpanID, v.ActionID, v.Failed, v.Denied, v.Replayed, v.DurationMS, cmd})
	case EventTodo:
		return marshalEvent(h, struct {
			Items []protocol.TodoItem `json:"items"`
		}{append([]protocol.TodoItem{}, e.todos...)})
	case EventStuck:
		v := e.stuck
		return marshalEvent(h, struct {
			Pattern StuckPattern       `json:"pattern"`
			Detail  string             `json:"detail"`
			Tool    *protocol.ToolName `json:"tool"`
			Halted  bool               `json:"halted"`
			Nudges  int                `json:"nudges_used"`
		}{v.signal.Pattern, v.signal.Detail, v.signal.Tool, v.halted, v.nudgesUsed})
	case EventReconcile:
		v := e.reconcile
		return marshalEvent(h, struct {
			Name       protocol.ToolName `json:"name"`
			Action     ActionID          `json:"action_id"`
			Verdict    EffectVerdict     `json:"verdict"`
			Verifiable bool              `json:"verifiable"`
		}{v.Name, v.ActionID, v.Verdict, v.Verifiable})
	case EventRecovery:
		return marshalEvent(h, e.recovery)
	case EventSubagentStart:
		v := e.subagent
		return marshalEvent(h, struct {
			Role   AgentRole `json:"role"`
			Prompt string    `json:"prompt"`
		}{v.role, v.prompt})
	case EventSubagentEnd:
		v := e.subagent
		return marshalEvent(h, struct {
			Role    AgentRole `json:"role"`
			Summary string    `json:"summary"`
		}{v.role, v.summary})
	case EventSubagentRefused:
		v := e.subagent
		return marshalEvent(h, struct {
			Role  AgentRole `json:"role"`
			Depth int       `json:"child_depth"`
			Limit int       `json:"limit"`
		}{v.role, v.childDepth, v.limit})
	case EventCompact:
		v := e.compact
		switch v.kind {
		case CompactBudget:
			return marshalEvent(h, struct {
				Kind  CompactionKind `json:"kind"`
				Count int            `json:"persisted"`
			}{v.kind, v.count})
		case CompactSnip:
			return marshalEvent(h, struct {
				Kind  CompactionKind `json:"kind"`
				Count int            `json:"removed"`
			}{v.kind, v.count})
		case CompactMicro:
			return marshalEvent(h, struct {
				Kind  CompactionKind `json:"kind"`
				Count int            `json:"cleared"`
			}{v.kind, v.count})
		case CompactFailed:
			return marshalEvent(h, struct {
				Kind  CompactionKind `json:"kind"`
				Error string         `json:"error"`
			}{v.kind, v.failure})
		case CompactAuto:
			s := v.summary
			return marshalEvent(h, struct {
				Kind       CompactionKind `json:"kind"`
				Transcript string         `json:"transcript"`
				Messages   int            `json:"replaced_messages"`
				Tokens     int            `json:"replaced_tokens_estimate"`
				Input      int            `json:"summary_input_tokens"`
				Output     int            `json:"summary_output_tokens"`
				Model      string         `json:"summary_model"`
			}{v.kind, s.Transcript, s.ReplacedMessages, s.ReplacedTokensEstimate, s.InputTokens, s.OutputTokens, s.Model})
		}
	case SessionEventKind(ApprovalRequiredEvent):
		return marshalEvent(h, e.approval.snapshot)
	case SessionEventKind(ApprovalTimeoutEvent):
		v := e.approval
		return marshalEvent(h, struct {
			ID     ApprovalID        `json:"approval_id"`
			Tool   protocol.ToolName `json:"tool"`
			Waited float64           `json:"waited"`
		}{v.id, v.tool, v.waited})
	case SessionEventKind(ApprovalGrantUsedEvent), SessionEventKind(ApprovalGrantRecordedEvent), SessionEventKind(ApprovalGrantRefusedEvent):
		v := e.approval
		if e.kind == SessionEventKind(ApprovalGrantUsedEvent) {
			return marshalEvent(h, struct {
				Tool  protocol.ToolName `json:"tool"`
				Rule  string            `json:"rule"`
				Grant GrantCandidate    `json:"grant"`
			}{v.tool, v.rule, v.grant})
		}
		reason := ""
		if e.kind == SessionEventKind(ApprovalGrantRefusedEvent) {
			reason = grantRefusalReason
		}
		return marshalEvent(h, struct {
			Tool   protocol.ToolName `json:"tool"`
			Grant  GrantCandidate    `json:"grant"`
			Reason string            `json:"reason,omitempty"`
		}{v.tool, v.grant, reason})
	case SessionEventKind(ApprovalAutoReviewedEvent):
		v := e.approval
		return marshalEvent(h, struct {
			Tool    protocol.ToolName `json:"tool"`
			Rule    string            `json:"rule"`
			Verdict ReviewVerdict     `json:"verdict"`
		}{v.tool, v.rule, v.verdict})
	default:
		if v, ok := e.Stop(); ok {
			return marshalEvent(h, struct {
				Reason     protocol.StopReason `json:"stop_reason"`
				Resumption int                 `json:"resumption,omitempty"`
				Detail     string              `json:"detail,omitempty"`
			}{v.Reason(), v.Resumption(), v.Detail()})
		}
	}
	return nil, fmt.Errorf("unsupported event kind %q", e.kind)
}
