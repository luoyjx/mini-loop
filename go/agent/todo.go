package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const MaxTodoField = 2_000

// TodoManager replaces the whole board only after every item has passed
// validation. Snapshots detach the slice so failed calls and callers cannot
// mutate the previously accepted board.
type TodoManager struct {
	mu    sync.Mutex
	items []protocol.TodoItem
}

func (manager *TodoManager) Update(items []protocol.TodoItem) (string, error) {
	validated := make([]protocol.TodoItem, 0, min(len(items), 20))
	inProgress := 0
	for i, item := range items {
		item.Content = pytext.Strip(item.Content)
		item.ActiveForm = pytext.Strip(item.ActiveForm)
		item.Status = protocol.TodoStatus(strings.ToLower(string(item.Status)))
		if item.Content == "" {
			return "", fmt.Errorf("Item %d: content required", i)
		}
		if item.Status != protocol.TodoPending && item.Status != protocol.TodoInProgress && item.Status != protocol.TodoCompleted {
			return "", fmt.Errorf("Item %d: invalid status '%s'", i, item.Status)
		}
		if item.ActiveForm == "" {
			return "", fmt.Errorf("Item %d: activeForm required", i)
		}
		item.Content = capTodoField(item.Content)
		item.ActiveForm = capTodoField(item.ActiveForm)
		if item.Status == protocol.TodoInProgress {
			inProgress++
		}
		if len(validated) < 20 {
			validated = append(validated, item)
		}
	}
	if len(items) > 20 {
		return "", fmt.Errorf("Max 20 todos")
	}
	if inProgress > 1 {
		return "", fmt.Errorf("Only one in_progress allowed")
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.items = validated
	return renderTodos(validated), nil
}

func (manager *TodoManager) Snapshot() []protocol.TodoItem {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return append([]protocol.TodoItem{}, manager.items...)
}

func (manager *TodoManager) Render() string { return renderTodos(manager.Snapshot()) }

func (manager *TodoManager) HasOpenItems() bool {
	for _, item := range manager.Snapshot() {
		if item.Status != protocol.TodoCompleted {
			return true
		}
	}
	return false
}

func capTodoField(value string) string {
	count := 0
	for index := range value {
		if count == MaxTodoField {
			return value[:index] + " [truncated]"
		}
		count++
	}
	return value
}

func renderTodos(items []protocol.TodoItem) string {
	if len(items) == 0 {
		return "No todos."
	}
	lines := make([]string, 0, len(items)+1)
	done := 0
	for _, item := range items {
		glyph, suffix := "[ ]", ""
		switch item.Status {
		case protocol.TodoCompleted:
			glyph = "[x]"
			done++
		case protocol.TodoInProgress:
			glyph = "[>]"
			suffix = " <- " + item.ActiveForm
		}
		lines = append(lines, glyph+" "+item.Content+suffix)
	}
	lines = append(lines, fmt.Sprintf("\n(%d/%d completed)", done, len(items)))
	return strings.Join(lines, "\n")
}
