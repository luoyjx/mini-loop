package protocol

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Team inputs contain member names and protocol IDs, never an owner, session,
// team identity or mailbox root. Those are supplied by the runtime binding.
type SpawnTeammateInput struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Prompt string `json:"prompt"`
}
type SendMessageInput struct {
	To       string        `json:"to"`
	Content  string        `json:"content"`
	Type     *string       `json:"type,omitempty"`
	Metadata *TeamMetadata `json:"metadata,omitempty"`
}
type BroadcastInput struct {
	Content string `json:"content"`
}
type RequestShutdownInput struct {
	Target string  `json:"target"`
	Reason *string `json:"reason,omitempty"`
}
type RequestPlanInput struct {
	Teammate string `json:"teammate"`
	Task     string `json:"task"`
}
type SubmitPlanInput struct {
	Plan string `json:"plan"`
}
type ReviewPlanInput struct {
	RequestID string  `json:"request_id"`
	Approve   bool    `json:"approve"`
	Feedback  *string `json:"feedback,omitempty"`
}

// TeamMetadata is an immutable object of closed JSON variants. The zero value
// is an empty object; null/absent is represented by the enclosing optional field.
// Historical nonfinite and surrogate values do not enter model input state.
type TeamMetadata struct{ value jsonvalue.Value }

func (v TeamMetadata) data() jsonvalue.Value {
	if v.value.Kind() == jsonvalue.Null {
		return jsonvalue.ObjectValue(nil)
	}
	return v.value
}
func (v TeamMetadata) Value() jsonvalue.Value       { return v.data() }
func (v TeamMetadata) MarshalJSON() ([]byte, error) { return v.data().MarshalJSON() }
func (v *TeamMetadata) UnmarshalJSON(data []byte) error {
	var value jsonvalue.Value
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value.Kind() != jsonvalue.Object {
		return errors.New("team metadata must be an object")
	}
	if _, err := value.MarshalJSON(); err != nil {
		return err
	}
	v.value = value
	return nil
}

func cloneString(v *string) *string {
	if v == nil {
		return nil
	}
	s := *v
	return &s
}
func cloneSendMessage(v SendMessageInput) SendMessageInput {
	v.Type = cloneString(v.Type)
	if v.Metadata != nil {
		m := *v.Metadata
		v.Metadata = &m
	}
	return v
}
func cloneRequestShutdown(v RequestShutdownInput) RequestShutdownInput {
	v.Reason = cloneString(v.Reason)
	return v
}
func cloneReviewPlan(v ReviewPlanInput) ReviewPlanInput {
	v.Feedback = cloneString(v.Feedback)
	return v
}
func SpawnTeammateToolInput(v SpawnTeammateInput) ToolInput {
	return ToolInput{name: ToolSpawnTeammate, spawnTeammate: v}
}
func SendMessageToolInput(v SendMessageInput) ToolInput {
	return ToolInput{name: ToolSendMessage, sendMessage: cloneSendMessage(v)}
}
func ReadInboxToolInput() ToolInput { return ToolInput{name: ToolReadInbox} }
func BroadcastToolInput(v BroadcastInput) ToolInput {
	return ToolInput{name: ToolBroadcast, broadcast: v}
}
func ListTeammatesToolInput() ToolInput { return ToolInput{name: ToolListTeammates} }
func RequestShutdownToolInput(v RequestShutdownInput) ToolInput {
	return ToolInput{name: ToolRequestShutdown, requestShutdown: cloneRequestShutdown(v)}
}
func RequestPlanToolInput(v RequestPlanInput) ToolInput {
	return ToolInput{name: ToolRequestPlan, requestPlan: v}
}
func SubmitPlanToolInput(v SubmitPlanInput) ToolInput {
	return ToolInput{name: ToolSubmitPlan, submitPlan: v}
}
func ReviewPlanToolInput(v ReviewPlanInput) ToolInput {
	return ToolInput{name: ToolReviewPlan, reviewPlan: cloneReviewPlan(v)}
}
func ListProtocolsToolInput() ToolInput { return ToolInput{name: ToolListProtocols} }
func (v ToolInput) SpawnTeammate() (SpawnTeammateInput, bool) {
	return v.spawnTeammate, v.name == ToolSpawnTeammate
}
func (v ToolInput) SendMessage() (SendMessageInput, bool) {
	return cloneSendMessage(v.sendMessage), v.name == ToolSendMessage
}
func (v ToolInput) Broadcast() (BroadcastInput, bool) { return v.broadcast, v.name == ToolBroadcast }
func (v ToolInput) RequestShutdown() (RequestShutdownInput, bool) {
	return cloneRequestShutdown(v.requestShutdown), v.name == ToolRequestShutdown
}
func (v ToolInput) RequestPlan() (RequestPlanInput, bool) {
	return v.requestPlan, v.name == ToolRequestPlan
}
func (v ToolInput) SubmitPlan() (SubmitPlanInput, bool) {
	return v.submitPlan, v.name == ToolSubmitPlan
}
func (v ToolInput) ReviewPlan() (ReviewPlanInput, bool) {
	return cloneReviewPlan(v.reviewPlan), v.name == ToolReviewPlan
}

func decodeTeamInput(name ToolName, data []byte) (ToolInput, error) {
	var result ToolInput
	switch name {
	case ToolSpawnTeammate:
		var wire struct {
			Name   *string `json:"name"`
			Role   *string `json:"role"`
			Prompt *string `json:"prompt"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Name == nil || wire.Role == nil || wire.Prompt == nil {
			return result, errors.New("spawn_teammate requires name, role and prompt")
		}
		result = SpawnTeammateToolInput(SpawnTeammateInput{*wire.Name, *wire.Role, *wire.Prompt})
	case ToolSendMessage:
		var wire struct {
			To       *string       `json:"to"`
			Content  *string       `json:"content"`
			Type     *string       `json:"type"`
			Metadata *TeamMetadata `json:"metadata"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.To == nil || wire.Content == nil {
			return result, errors.New("send_message requires to and content")
		}
		result = SendMessageToolInput(SendMessageInput{*wire.To, *wire.Content, wire.Type, wire.Metadata})
	case ToolReadInbox, ToolListTeammates, ToolListProtocols:
		var wire struct{}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		result = ToolInput{name: name}
	case ToolBroadcast:
		var wire struct {
			Content *string `json:"content"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Content == nil {
			return result, errors.New("broadcast requires content")
		}
		result = BroadcastToolInput(BroadcastInput{*wire.Content})
	case ToolRequestShutdown:
		var wire struct {
			Target *string `json:"target"`
			Reason *string `json:"reason"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Target == nil {
			return result, errors.New("request_shutdown requires target")
		}
		result = RequestShutdownToolInput(RequestShutdownInput{*wire.Target, wire.Reason})
	case ToolRequestPlan:
		var wire struct {
			Teammate *string `json:"teammate"`
			Task     *string `json:"task"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Teammate == nil || wire.Task == nil {
			return result, errors.New("request_plan requires teammate and task")
		}
		result = RequestPlanToolInput(RequestPlanInput{*wire.Teammate, *wire.Task})
	case ToolSubmitPlan:
		var wire struct {
			Plan *string `json:"plan"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.Plan == nil {
			return result, errors.New("submit_plan requires plan")
		}
		result = SubmitPlanToolInput(SubmitPlanInput{*wire.Plan})
	case ToolReviewPlan:
		var wire struct {
			RequestID *string `json:"request_id"`
			Approve   *bool   `json:"approve"`
			Feedback  *string `json:"feedback"`
		}
		if err := decodeToolObject(data, &wire); err != nil {
			return result, err
		}
		if wire.RequestID == nil || wire.Approve == nil {
			return result, errors.New("review_plan requires request_id and approve")
		}
		result = ReviewPlanToolInput(ReviewPlanInput{*wire.RequestID, *wire.Approve, wire.Feedback})
	default:
		return result, fmt.Errorf("unsupported team tool %q", name)
	}
	if err := json.Unmarshal(data, &result.nulls); err != nil {
		return ToolInput{}, err
	}
	if (name == ToolSendMessage && result.nulls.Type) || (name == ToolRequestShutdown && result.nulls.TeamReason) || (name == ToolReviewPlan && result.nulls.TeamFeedback) {
		return ToolInput{}, errors.New("team optional text must be a string")
	}
	if err := result.Validate(); err != nil {
		return ToolInput{}, err
	}
	return result, nil
}

// teamInputProjection encodes one concretely selected input with sorted fields.
// It exists only while rendering Python-compatible step identity.
type teamInputProjection struct {
	input  ToolInput
	sorted bool
}

func (v teamInputProjection) MarshalJSON() ([]byte, error) { return v.input.marshalTeamJSON(v.sorted) }
func (input ToolInput) marshalTeamJSON(sorted bool) ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	switch input.name {
	case ToolSpawnTeammate:
		if sorted {
			return json.Marshal(struct {
				Name   string `json:"name"`
				Prompt string `json:"prompt"`
				Role   string `json:"role"`
			}{input.spawnTeammate.Name, input.spawnTeammate.Prompt, input.spawnTeammate.Role})
		}
		return json.Marshal(input.spawnTeammate)
	case ToolReadInbox, ToolListTeammates, ToolListProtocols:
		return []byte("{}"), nil
	case ToolBroadcast:
		return json.Marshal(input.broadcast)
	case ToolRequestPlan:
		if sorted {
			return json.Marshal(struct {
				Task     string `json:"task"`
				Teammate string `json:"teammate"`
			}{input.requestPlan.Task, input.requestPlan.Teammate})
		}
		return json.Marshal(input.requestPlan)
	case ToolSubmitPlan:
		return json.Marshal(input.submitPlan)
	case ToolSendMessage:
		v := input.sendMessage
		if sorted {
			if v.Metadata != nil {
				m := TeamMetadata{value: v.Metadata.data().Sorted()}
				v.Metadata = &m
			}
			return json.Marshal(struct {
				Content  string                      `json:"content"`
				Metadata *wireOptional[TeamMetadata] `json:"metadata,omitempty"`
				To       string                      `json:"to"`
				Type     *string                     `json:"type,omitempty"`
			}{v.Content, optionalWire(v.Metadata, input.nulls.TeamMetadata), v.To, v.Type})
		}
		return json.Marshal(struct {
			To       string                      `json:"to"`
			Content  string                      `json:"content"`
			Type     *string                     `json:"type,omitempty"`
			Metadata *wireOptional[TeamMetadata] `json:"metadata,omitempty"`
		}{v.To, v.Content, v.Type, optionalWire(v.Metadata, input.nulls.TeamMetadata)})
	case ToolRequestShutdown:
		if sorted {
			return json.Marshal(struct {
				Reason *string `json:"reason,omitempty"`
				Target string  `json:"target"`
			}{input.requestShutdown.Reason, input.requestShutdown.Target})
		}
		return json.Marshal(input.requestShutdown)
	case ToolReviewPlan:
		if sorted {
			return json.Marshal(struct {
				Approve   bool    `json:"approve"`
				Feedback  *string `json:"feedback,omitempty"`
				RequestID string  `json:"request_id"`
			}{input.reviewPlan.Approve, input.reviewPlan.Feedback, input.reviewPlan.RequestID})
		}
		return json.Marshal(input.reviewPlan)
	}
	return nil, fmt.Errorf("unsupported team tool %q", input.name)
}

func TeamSchemas() []ToolSchema {
	spawn := SchemaProperties{"name": {Type: SchemaString}, "role": {Type: SchemaString}, "prompt": {Type: SchemaString}}
	send := SchemaProperties{"to": {Type: SchemaString}, "content": {Type: SchemaString}, "type": {Type: SchemaString}, "metadata": {Type: SchemaObject}}
	broadcast := SchemaProperties{"content": {Type: SchemaString}}
	shutdown := SchemaProperties{"target": {Type: SchemaString}, "reason": {Type: SchemaString}}
	request := SchemaProperties{"teammate": {Type: SchemaString}, "task": {Type: SchemaString}}
	plan := SchemaProperties{"plan": {Type: SchemaString}}
	review := SchemaProperties{"request_id": {Type: SchemaString}, "approve": {Type: SchemaBoolean}, "feedback": {Type: SchemaString}}
	empty := SchemaProperties{}
	return []ToolSchema{
		{Name: ToolSpawnTeammate, Description: "Spawn an autonomous concurrent teammate.", InputSchema: InputSchema{Type: SchemaObject, Properties: &spawn, Required: []string{"name", "role", "prompt"}}},
		{Name: ToolSendMessage, Description: "Send a typed message to a teammate.", InputSchema: InputSchema{Type: SchemaObject, Properties: &send, Required: []string{"to", "content"}}},
		{Name: ToolReadInbox, Description: "Read, route, and drain your inbox.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
		{Name: ToolBroadcast, Description: "Send a message to all teammates.", InputSchema: InputSchema{Type: SchemaObject, Properties: &broadcast, Required: []string{"content"}}},
		{Name: ToolListTeammates, Description: "List teammates in this team.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
		{Name: ToolRequestShutdown, Description: "Request a teammate shutdown with an auditable handshake.", InputSchema: InputSchema{Type: SchemaObject, Properties: &shutdown, Required: []string{"target"}}},
		{Name: ToolRequestPlan, Description: "Ask a teammate to submit a plan for a task.", InputSchema: InputSchema{Type: SchemaObject, Properties: &request, Required: []string{"teammate", "task"}}},
		{Name: ToolSubmitPlan, Description: "Submit a plan to the lead for approval.", InputSchema: InputSchema{Type: SchemaObject, Properties: &plan, Required: []string{"plan"}}},
		{Name: ToolReviewPlan, Description: "Approve or reject a submitted teammate plan.", InputSchema: InputSchema{Type: SchemaObject, Properties: &review, Required: []string{"request_id", "approve"}}},
		{Name: ToolListProtocols, Description: "List protocol request states.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
	}
}
