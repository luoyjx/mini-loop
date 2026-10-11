package agent

import (
	"fmt"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/tasks"
)

func (handler *runtimeHandler) executeTaskBoard(input protocol.ToolInput) (string, error) {
	store := handler.taskStore
	switch input.Name() {
	case protocol.ToolCreateTask:
		value, _ := input.CreateTask()
		description := ""
		if value.Description != nil {
			description = *value.Description
		}
		var deps []tasks.ID
		if value.BlockedBy != nil {
			for _, dep := range *value.BlockedBy {
				deps = append(deps, tasks.ID(dep))
			}
		}
		task, err := store.Create(value.Subject, description, deps, value.Worktree)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Created %s: %s", task.ID, task.Subject), nil
	case protocol.ToolListTasks:
		return store.Render()
	case protocol.ToolGetTask:
		value, _ := input.TaskReference()
		task, err := store.Load(tasks.ID(value.TaskID))
		if err != nil {
			return "", err
		}
		if task == nil {
			return "Error: task " + value.TaskID + " not found", nil
		}
		return tasks.JSON(*task)
	case protocol.ToolClaimTask:
		value, _ := input.TaskReference()
		return store.Claim(tasks.ID(value.TaskID), tasks.Owner(handler.session.label))
	case protocol.ToolCompleteTask:
		value, _ := input.TaskReference()
		owner := tasks.Owner(handler.session.label)
		return store.Complete(tasks.ID(value.TaskID), &owner)
	default:
		return "", fmt.Errorf("unsupported task board input %s", input.Name())
	}
}
