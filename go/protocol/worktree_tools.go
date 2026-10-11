package protocol

type CreateWorktreeInput struct {
	Name   string  `json:"name"`
	TaskID *string `json:"task_id,omitempty"`
}
type RemoveWorktreeInput struct {
	Name           string `json:"name"`
	DiscardChanges *bool  `json:"discard_changes,omitempty"`
}
type WorktreeNameInput struct {
	Name string `json:"name"`
}

func cloneCreateWorktree(v CreateWorktreeInput) CreateWorktreeInput {
	if v.TaskID != nil {
		value := *v.TaskID
		v.TaskID = &value
	}
	return v
}
func cloneRemoveWorktree(v RemoveWorktreeInput) RemoveWorktreeInput {
	if v.DiscardChanges != nil {
		value := *v.DiscardChanges
		v.DiscardChanges = &value
	}
	return v
}
func CreateWorktreeToolInput(v CreateWorktreeInput) ToolInput {
	return ToolInput{name: ToolCreateWorktree, createWorktree: cloneCreateWorktree(v)}
}
func RemoveWorktreeToolInput(v RemoveWorktreeInput) ToolInput {
	return ToolInput{name: ToolRemoveWorktree, removeWorktree: cloneRemoveWorktree(v)}
}
func KeepWorktreeToolInput(v WorktreeNameInput) ToolInput {
	return ToolInput{name: ToolKeepWorktree, worktreeName: v}
}
func EnterWorktreeToolInput(v WorktreeNameInput) ToolInput {
	return ToolInput{name: ToolEnterWorktree, worktreeName: v}
}
func ListWorktreesToolInput() ToolInput { return ToolInput{name: ToolListWorktrees} }
func (v ToolInput) CreateWorktree() (CreateWorktreeInput, bool) {
	return cloneCreateWorktree(v.createWorktree), v.name == ToolCreateWorktree
}
func (v ToolInput) RemoveWorktree() (RemoveWorktreeInput, bool) {
	return cloneRemoveWorktree(v.removeWorktree), v.name == ToolRemoveWorktree
}
func (v ToolInput) WorktreeName() (WorktreeNameInput, bool) {
	return v.worktreeName, v.name == ToolKeepWorktree || v.name == ToolEnterWorktree
}

func WorktreeSchemas() []ToolSchema {
	scalar := InputSchema{Type: SchemaString}
	create := SchemaProperties{"name": scalar, "task_id": scalar}
	remove := SchemaProperties{"name": scalar, "discard_changes": {Type: SchemaBoolean}}
	name := SchemaProperties{"name": scalar}
	empty := SchemaProperties{}
	return []ToolSchema{
		{Name: ToolCreateWorktree, Description: "Create an isolated git worktree and optionally bind a task.", InputSchema: InputSchema{Type: SchemaObject, Properties: &create, Required: []string{"name"}}},
		{Name: ToolRemoveWorktree, Description: "Remove a clean worktree, or explicitly discard its changes.", InputSchema: InputSchema{Type: SchemaObject, Properties: &remove, Required: []string{"name"}}},
		{Name: ToolKeepWorktree, Description: "Keep a worktree and record it for review.", InputSchema: InputSchema{Type: SchemaObject, Properties: &name, Required: []string{"name"}}},
		{Name: ToolListWorktrees, Description: "List git worktrees.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
		{Name: ToolEnterWorktree, Description: "Switch this agent's file tools into a worktree.", InputSchema: InputSchema{Type: SchemaObject, Properties: &name, Required: []string{"name"}}},
	}
}
