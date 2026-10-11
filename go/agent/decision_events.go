package agent

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const (
	EventDecisionCompleted SessionEventKind = "decision_completed"
	EventDecisionFailed    SessionEventKind = "decision_failed"
)

// DecisionUsage retains whole unavailable usage as {}, without fabricating zero
// token counts. Present usage contains both nonnegative signed-64 counts.
type DecisionUsage struct{ counts *decisions.TokenUsage }

func decisionUsage(v *decisions.TokenUsage) DecisionUsage {
	return DecisionUsage{counts: clonePointer(v)}
}
func (v DecisionUsage) Counts() (*decisions.TokenUsage, bool) {
	return clonePointer(v.counts), v.counts != nil
}
func (v DecisionUsage) clone() DecisionUsage { return decisionUsage(v.counts) }
func (v DecisionUsage) MarshalJSON() ([]byte, error) {
	if v.counts == nil {
		return []byte("{}"), nil
	}
	if v.counts.InputTokens < 0 || v.counts.OutputTokens < 0 {
		return nil, errors.New("invalid decision usage")
	}
	return json.Marshal(v.counts)
}
func (v *DecisionUsage) UnmarshalJSON(data []byte) error {
	var wire struct {
		Input  *int64 `json:"input_tokens"`
		Output *int64 `json:"output_tokens"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("invalid decision usage")
	}
	if wire.Input == nil && wire.Output == nil {
		// Null members are not whole unavailable usage.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		if len(fields) != 0 {
			return errors.New("invalid decision usage")
		}
		*v = DecisionUsage{}
		return nil
	}
	if wire.Input == nil || wire.Output == nil || *wire.Input < 0 || *wire.Output < 0 {
		return errors.New("invalid decision usage")
	}
	*v = decisionUsage(&decisions.TokenUsage{InputTokens: *wire.Input, OutputTokens: *wire.Output})
	return nil
}

type DecisionCompletedEvent struct {
	Provider          string                      `json:"provider"`
	Model             string                      `json:"model"`
	ProbabilitySource decisions.ProbabilitySource `json:"probability_source"`
	QuestionCount     int                         `json:"question_count"`
	Usage             DecisionUsage               `json:"usage"`
}
type DecisionFailedEvent struct {
	ErrorType decisions.ErrorKind `json:"error_type"`
}

// External decision calls use the source's smaller model telemetry variant.
// LLM calls retain the ordinary model variant and their independent child scope.
type DecisionModelStartEvent struct {
	SpanID       SpanID                  `json:"span_id"`
	Purpose      protocol.RequestPurpose `json:"purpose"`
	Model        string                  `json:"model"`
	ToolCount    int                     `json:"tool_count"`
	MessageCount int                     `json:"message_count"`
}
type DecisionModelEndEvent struct {
	SpanID       SpanID                  `json:"span_id"`
	Purpose      protocol.RequestPurpose `json:"purpose"`
	Status       ModelStatus             `json:"status"`
	DurationMS   float64                 `json:"duration_ms"`
	ServedModel  *string                 `json:"served_model,omitempty"`
	Usage        *DecisionUsage          `json:"usage,omitempty"`
	PromptTokens *int64                  `json:"prompt_tokens,omitempty"`
}

func (v DecisionModelEndEvent) clone() DecisionModelEndEvent {
	v.ServedModel, v.PromptTokens = clonePointer(v.ServedModel), clonePointer(v.PromptTokens)
	if v.Usage != nil {
		u := v.Usage.clone()
		v.Usage = &u
	}
	return v
}
func (e SessionEvent) DecisionCompleted() (DecisionCompletedEvent, bool) {
	v := e.decisionCompleted
	v.Usage = v.Usage.clone()
	return v, e.kind == EventDecisionCompleted
}
func (e SessionEvent) DecisionFailed() (DecisionFailedEvent, bool) {
	return e.decisionFailed, e.kind == EventDecisionFailed
}
func (e SessionEvent) DecisionModelStart() (DecisionModelStartEvent, bool) {
	if e.kind != EventModelStart || e.decisionModelStart == nil {
		return DecisionModelStartEvent{}, false
	}
	return *e.decisionModelStart, true
}
func (e SessionEvent) DecisionModelEnd() (DecisionModelEndEvent, bool) {
	if e.kind != EventModelEnd || e.decisionModelEnd == nil {
		return DecisionModelEndEvent{}, false
	}
	return e.decisionModelEnd.clone(), true
}
