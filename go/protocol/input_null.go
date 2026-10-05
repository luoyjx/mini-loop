package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// These closed flags distinguish absent optional fields from explicit JSON null
// for replay and step identity. Execution accessors still expose the same nil
// default value; no untyped JSON survives the decoder.
type nullField bool

func (field *nullField) UnmarshalJSON(data []byte) error {
	*field = nullField(bytes.Equal(bytes.TrimSpace(data), []byte("null")))
	return nil
}

type inputNullFields struct {
	BackgroundTimeout nullField `json:"timeout"`
	BackgroundID      nullField `json:"bg_id"`
	WorktreeTaskID    nullField `json:"task_id"`
	WorktreeDiscard   nullField `json:"discard_changes"`
	TaskDescription   nullField `json:"description"`
	TaskDependencies  nullField `json:"blockedBy"`
	TaskWorktree      nullField `json:"worktree"`
	Background        nullField `json:"run_in_background"`
	ApprovalPrefix    nullField `json:"approval_prefix"`
	Limit             nullField `json:"limit"`
	Offset            nullField `json:"offset"`
	AgentType         nullField `json:"agent_type"`
	Scope             nullField `json:"scope"`
}

// wireOptional is used only in concretely instantiated encoding boundary structs.
type wireOptional[T any] struct{ value *T }

func (field wireOptional[T]) MarshalJSON() ([]byte, error) { return json.Marshal(field.value) }
func optionalWire[T any](value *T, explicitNull nullField) *wireOptional[T] {
	if value == nil && !bool(explicitNull) {
		return nil
	}
	return &wireOptional[T]{value}
}

func (input ToolInput) marshalOptionalJSON() ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	switch input.name {
	case ToolBackgroundRun:
		v := input.backgroundRun
		return json.Marshal(struct {
			Command string                  `json:"command"`
			Timeout *wireOptional[int]      `json:"timeout,omitempty"`
			Prefix  *wireOptional[[]string] `json:"approval_prefix,omitempty"`
		}{v.Command, optionalWire(v.Timeout, input.nulls.BackgroundTimeout), optionalWire(v.ApprovalPrefix, input.nulls.ApprovalPrefix)})
	case ToolCheckBackground:
		return json.Marshal(struct {
			ID *wireOptional[string] `json:"bg_id,omitempty"`
		}{optionalWire(input.checkBackground.ID, input.nulls.BackgroundID)})

	case ToolCreateWorktree:
		v := input.createWorktree
		return json.Marshal(struct {
			Name   string                `json:"name"`
			TaskID *wireOptional[string] `json:"task_id,omitempty"`
		}{v.Name, optionalWire(v.TaskID, input.nulls.WorktreeTaskID)})
	case ToolRemoveWorktree:
		v := input.removeWorktree
		return json.Marshal(struct {
			Name    string              `json:"name"`
			Discard *wireOptional[bool] `json:"discard_changes,omitempty"`
		}{v.Name, optionalWire(v.DiscardChanges, input.nulls.WorktreeDiscard)})
	case ToolBash:
		v := input.bash
		return json.Marshal(struct {
			Command    string                  `json:"command"`
			Background *wireOptional[bool]     `json:"run_in_background,omitempty"`
			Prefix     *wireOptional[[]string] `json:"approval_prefix,omitempty"`
		}{v.Command, optionalWire(v.RunInBackground, input.nulls.Background), optionalWire(v.ApprovalPrefix, input.nulls.ApprovalPrefix)})
	case ToolReadFile:
		v := input.readFile
		return json.Marshal(struct {
			Path   string             `json:"path"`
			Limit  *wireOptional[int] `json:"limit,omitempty"`
			Offset *wireOptional[int] `json:"offset,omitempty"`
		}{v.Path, optionalWire(v.Limit, input.nulls.Limit), optionalWire(v.Offset, input.nulls.Offset)})
	case ToolTask:
		v := input.task
		return json.Marshal(struct {
			Prompt    string                   `json:"prompt"`
			AgentType *wireOptional[AgentType] `json:"agent_type,omitempty"`
		}{v.Prompt, optionalWire(v.AgentType, input.nulls.AgentType)})
	case ToolCreateTask:
		v := input.createTask
		return json.Marshal(struct {
			Subject     string                  `json:"subject"`
			Description *wireOptional[string]   `json:"description,omitempty"`
			BlockedBy   *wireOptional[[]string] `json:"blockedBy,omitempty"`
			Worktree    *wireOptional[string]   `json:"worktree,omitempty"`
		}{v.Subject, optionalWire(v.Description, input.nulls.TaskDescription), optionalWire(v.BlockedBy, input.nulls.TaskDependencies), optionalWire(v.Worktree, input.nulls.TaskWorktree)})
	case ToolLoadSkill:
		v := input.loadSkill
		return json.Marshal(struct {
			Name  string                    `json:"name"`
			Scope *wireOptional[SkillScope] `json:"scope,omitempty"`
		}{v.Name, optionalWire(v.Scope, input.nulls.Scope)})
	}
	return nil, fmt.Errorf("tool %s has no optional fields", input.name)
}
