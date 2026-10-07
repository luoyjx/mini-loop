package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/teams"
)

const EventTeamInbox SessionEventKind = "team_inbox"

type TeamInboxEvent struct {
	Count int `json:"count"`
}

func (event SessionEvent) TeamInbox() (TeamInboxEvent, bool) {
	return event.teamInbox, event.kind == EventTeamInbox
}

// Consume preserves shutdown assignment even after a later partial protocol fault.
// A bound runtime can only consume its own registered identity, never its parent's.
func (manager *SessionManager) consumeTeamInbox(ctx context.Context, core *Session) (teams.ConsumedInbox, error) {
	if err := ctx.Err(); err != nil {
		return teams.ConsumedInbox{}, err
	}
	manager.mu.Lock()
	session := manager.sessions[core.id]
	valid := manager.state == ManagerActive && session != nil && session.core == core && core.team != nil
	manager.mu.Unlock()
	if !valid {
		return teams.ConsumedInbox{}, errors.New("team inbox binding is no longer active")
	}
	inbox, err := manager.teamProtocols.Consume(ctx, *core.team)
	if inbox.ShutdownRequested {
		core.teamShutdown.Store(true)
	}
	return inbox, err
}

// Called with the core turn lock, after background notifications and before user
// injectors. A delegated in-process child has no manager binding and does nothing.
func (core *Session) injectTeam(ctx context.Context) error {
	if core.teamManager == nil || core.team == nil {
		return nil
	}
	inbox, err := core.teamManager.consumeTeamInbox(ctx, core)
	if err != nil {
		return err
	}
	if len(inbox.Messages) == 0 {
		return nil
	}
	// Source emits the delivery count before rendering; malformed sender data may
	// fail after the inbox has drained and this event has already been recorded.
	core.events.append(SessionEvent{kind: EventTeamInbox, teamInbox: TeamInboxEvent{len(inbox.Messages)}})
	text, err := teams.RenderMessages(inbox.Messages)
	if err != nil {
		return err
	}
	core.appendMessages(protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("<team_inbox>\n" + text + "\n</team_inbox>")})
	return nil
}
