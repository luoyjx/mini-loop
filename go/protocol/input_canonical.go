package protocol

import "fmt"

// CanonicalJSON binds a validated input to Python's sorted, compact UTF-8 JSON.
// Each variant supplies concrete fields in key order; no dynamic JSON is stored.
func (input ToolInput) CanonicalJSON() (string, error) { return input.sortedJSON(true) }

// SortedPythonJSON is the spaced UTF-8 projection used by Python's step_hash.
func (input ToolInput) SortedPythonJSON() (string, error) { return input.sortedJSON(false) }
func (input ToolInput) sortedJSON(compact bool) (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	switch input.Name() {
	case ToolScheduleCron:
		v := input.scheduleCron
		return PythonJSON(struct {
			Cron      string `json:"cron"`
			Durable   *bool  `json:"durable,omitempty"`
			Prompt    string `json:"prompt"`
			Recurring *bool  `json:"recurring,omitempty"`
		}{v.Cron, v.Durable, v.Prompt, v.Recurring}, false, compact)
	case ToolEnterPlanMode, ToolExitPlanMode, ToolListCrons, ToolCancelCron:
		return PythonJSON(input, false, compact)
	case ToolBackgroundRun:
		v := input.backgroundRun
		return PythonJSON(struct {
			Prefix  *wireOptional[[]string] `json:"approval_prefix,omitempty"`
			Command string                  `json:"command"`
			Timeout *wireOptional[int]      `json:"timeout,omitempty"`
		}{optionalWire(v.ApprovalPrefix, input.nulls.ApprovalPrefix), v.Command, optionalWire(v.Timeout, input.nulls.BackgroundTimeout)}, false, compact)
	case ToolCheckBackground:
		return PythonJSON(input, false, compact)

	case ToolCreateWorktree:
		v := input.createWorktree
		return PythonJSON(struct {
			Name   string                `json:"name"`
			TaskID *wireOptional[string] `json:"task_id,omitempty"`
		}{v.Name, optionalWire(v.TaskID, input.nulls.WorktreeTaskID)}, false, compact)
	case ToolRemoveWorktree:
		v := input.removeWorktree
		return PythonJSON(struct {
			Discard *wireOptional[bool] `json:"discard_changes,omitempty"`
			Name    string              `json:"name"`
		}{optionalWire(v.DiscardChanges, input.nulls.WorktreeDiscard), v.Name}, false, compact)
	case ToolKeepWorktree, ToolEnterWorktree, ToolListWorktrees:
		return PythonJSON(input, false, compact)
	case ToolBash:
		v, _ := input.Bash()
		return PythonJSON(struct {
			ApprovalPrefix  *wireOptional[[]string] `json:"approval_prefix,omitempty"`
			Command         string                  `json:"command"`
			RunInBackground *wireOptional[bool]     `json:"run_in_background,omitempty"`
		}{optionalWire(v.ApprovalPrefix, input.nulls.ApprovalPrefix), v.Command, optionalWire(v.RunInBackground, input.nulls.Background)}, false, compact)
	case ToolReadFile:
		v, _ := input.ReadFile()
		return PythonJSON(struct {
			Limit  *wireOptional[int] `json:"limit,omitempty"`
			Offset *wireOptional[int] `json:"offset,omitempty"`
			Path   string             `json:"path"`
		}{optionalWire(v.Limit, input.nulls.Limit), optionalWire(v.Offset, input.nulls.Offset), v.Path}, false, compact)
	case ToolWriteFile:
		v, _ := input.WriteFile()
		return PythonJSON(struct {
			Content string `json:"content"`
			Path    string `json:"path"`
		}{v.Content, v.Path}, false, compact)
	case ToolEditFile:
		v, _ := input.EditFile()
		return PythonJSON(struct {
			NewText string `json:"new_text"`
			OldText string `json:"old_text"`
			Path    string `json:"path"`
		}{v.NewText, v.OldText, v.Path}, false, compact)
	case ToolTodoWrite:
		v, _ := input.TodoWrite()
		type item struct {
			ActiveForm string     `json:"activeForm"`
			Content    string     `json:"content"`
			Status     TodoStatus `json:"status"`
		}
		items := make([]item, len(v.Items))
		for i, v := range v.Items {
			items[i] = item{v.ActiveForm, v.Content, v.Status}
		}
		return PythonJSON(struct {
			Items []item `json:"items"`
		}{items}, false, compact)
	case ToolTask:
		v, _ := input.Task()
		return PythonJSON(struct {
			AgentType *wireOptional[AgentType] `json:"agent_type,omitempty"`
			Prompt    string                   `json:"prompt"`
		}{optionalWire(v.AgentType, input.nulls.AgentType), v.Prompt}, false, compact)
	case ToolLoadSkill:
		v, _ := input.LoadSkill()
		return PythonJSON(struct {
			Name  string                    `json:"name"`
			Scope *wireOptional[SkillScope] `json:"scope,omitempty"`
		}{v.Name, optionalWire(v.Scope, input.nulls.Scope)}, false, compact)
	case ToolCreateTask:
		v := input.createTask
		return PythonJSON(struct {
			BlockedBy   *wireOptional[[]string] `json:"blockedBy,omitempty"`
			Description *wireOptional[string]   `json:"description,omitempty"`
			Subject     string                  `json:"subject"`
			Worktree    *wireOptional[string]   `json:"worktree,omitempty"`
		}{optionalWire(v.BlockedBy, input.nulls.TaskDependencies), optionalWire(v.Description, input.nulls.TaskDescription), v.Subject, optionalWire(v.Worktree, input.nulls.TaskWorktree)}, false, compact)
	case ToolGlob, ToolCompress, ToolAskUser, ToolListTasks, ToolGetTask, ToolClaimTask, ToolCompleteTask:
		// These payloads already have sorted field order.
		return PythonJSON(input, false, compact)
	default:
		return "", fmt.Errorf("unsupported canonical input %q", input.Name())
	}
}
