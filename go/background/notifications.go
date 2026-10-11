package background

import (
	"fmt"
	"strings"
)

// Batch uses the newest 50 completed results. Drain consumes the whole queue;
// omitted entries remain available through Check subject to result retention.
type Batch struct {
	Notifications []Notification
	Dropped       int
}

func (manager *Manager) DrainBatch() Batch {
	done := manager.Drain()
	dropped := max(0, len(done)-MaxNotifications)
	return Batch{done[dropped:], dropped}
}
func (batch Batch) Render() string {
	var lines []string
	for _, row := range batch.Notifications {
		lines = append(lines, fmt.Sprintf("<task_notification id=\"%s\" status=\"%s\">\n%s\n</task_notification>", row.ID, row.Status, row.Result))
	}
	text := strings.Join(lines, "\n")
	if batch.Dropped > 0 {
		text += fmt.Sprintf("\n[%d earlier background result(s) omitted from this batch; retrieve any with check_background(bg_id)]", batch.Dropped)
	}
	return text
}
