package agent

import (
	"context"

	"github.com/luoyjx/mini-loop/go/teams"
)

// TeamView is a detached, non-consuming view of the session-bound inbox. A nil
// identity is distinct from an established team with an empty inbox.
type TeamView struct {
	Session  SessionID
	Identity *teams.Identity
	Inbox    []teams.Message
}

// PeekTeam resolves ownership before filesystem access; caller data cannot pick
// another team/root/member. Managed sessions start as their own one-member team.
func (manager *SessionManager) PeekTeam(ctx context.Context, owner OwnerID, id SessionID) (TeamView, error) {
	session, err := manager.Get(owner, id)
	if err != nil {
		return TeamView{}, err
	}
	view := TeamView{Session: id, Inbox: []teams.Message{}}
	if session.core.team == nil {
		return view, nil
	}
	identity := *session.core.team
	view.Identity = &identity
	view.Inbox, err = manager.teams.Peek(ctx, identity.Key())
	if err != nil {
		return TeamView{}, err
	}
	if len(view.Inbox) > 50 {
		view.Inbox = view.Inbox[len(view.Inbox)-50:]
	}
	return view, nil
}
