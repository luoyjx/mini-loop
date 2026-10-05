package agent

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/cron"
	"github.com/luoyjx/mini-loop/go/protocol"
	"strings"
)

// CronControl binds model effects to a known session and established owner.
// Arm is deliberately absent: restored activation requires an operator edge.
type CronControl interface {
	ScheduleCron(OwnerID, SessionID, ScheduleCronRequest) (cron.Scheduled, error)
	ListCrons(OwnerID, SessionID) (string, error)
	CancelCron(OwnerID, SessionID, cron.ID) (string, error)
}

func (manager *SessionManager) ListCrons(owner OwnerID, id SessionID) (string, error) {
	manager.cronMu.Lock()
	defer manager.cronMu.Unlock()
	if err := manager.cronSession(owner, id, false); err != nil {
		return "", err
	}
	return manager.cron.ListFor(cron.SessionID(id)), nil
}

func (h *runtimeHandler) executeCron(input protocol.ToolInput) (string, error) {
	if h.cron == nil {
		if input.Name() == protocol.ToolScheduleCron {
			return "Error: cron scheduler not available", nil
		}
		return "Error: cron not available", nil
	}
	switch input.Name() {
	case protocol.ToolScheduleCron:
		v, _ := input.ScheduleCron()
		job, err := h.cron.ScheduleCron(h.binding.OwnerID, h.binding.SessionID, ScheduleCronRequest{v.Cron, v.Prompt, v.Recurring, v.Durable})
		if err != nil {
			if strings.HasPrefix(err.Error(), "Error:") {
				return err.Error(), nil
			}
			return "", err
		}
		return job.Render(), nil
	case protocol.ToolListCrons:
		return h.cron.ListCrons(h.binding.OwnerID, h.binding.SessionID)
	case protocol.ToolCancelCron:
		v, _ := input.CancelCron()
		return h.cron.CancelCron(h.binding.OwnerID, h.binding.SessionID, cron.ID(v.JobID))
	}
	return "", errors.New("unsupported cron input")
}
