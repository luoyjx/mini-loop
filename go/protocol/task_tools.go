package protocol

// Persistent task graph inputs are separate from TaskInput, which delegates a
// prompt to a child agent. Optional graph tools do not change the ten defaults.
type CreateTaskInput struct {
	Subject     string    `json:"subject"`
	Description *string   `json:"description,omitempty"`
	BlockedBy   *[]string `json:"blockedBy,omitempty"`
	Worktree    *string   `json:"worktree,omitempty"`
}
type TaskReferenceInput struct {
	TaskID string `json:"task_id"`
}

func TaskBoardSchemas() []ToolSchema {
	scalar := InputSchema{Type: SchemaString}
	create := SchemaProperties{"subject": scalar, "description": scalar, "blockedBy": {Type: SchemaArray, Items: &scalar}, "worktree": scalar}
	byID := SchemaProperties{"task_id": scalar}
	empty := SchemaProperties{}
	return []ToolSchema{
		{Name: ToolCreateTask, Description: "Create a persistent task (optionally blockedBy other task ids).", InputSchema: InputSchema{Type: SchemaObject, Properties: &create, Required: []string{"subject"}}},
		{Name: ToolListTasks, Description: "List all persistent tasks and their status.", InputSchema: InputSchema{Type: SchemaObject, Properties: &empty}},
		{Name: ToolGetTask, Description: "Get one task's full JSON by id.", InputSchema: InputSchema{Type: SchemaObject, Properties: &byID, Required: []string{"task_id"}}},
		{Name: ToolClaimTask, Description: "Claim a pending, unblocked task (sets you as owner).", InputSchema: InputSchema{Type: SchemaObject, Properties: &byID, Required: []string{"task_id"}}},
		{Name: ToolCompleteTask, Description: "Mark a task completed; reports newly-unblocked tasks.", InputSchema: InputSchema{Type: SchemaObject, Properties: &byID, Required: []string{"task_id"}}},
	}
}
