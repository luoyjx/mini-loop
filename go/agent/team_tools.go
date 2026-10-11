package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/teams"
	"github.com/luoyjx/mini-loop/go/workspace"
)

// Check the immutable binding before the journal can return a prior result.
// Handler admission alone cannot protect replayed private inbox projections.
type teamAuthorityGuard struct{ handler *runtimeHandler }

func (g teamAuthorityGuard) GuardTool(ctx context.Context, authority ToolAuthority, call ToolCall) (string, bool, error) {
	switch call.Name() {
	case protocol.ToolSpawnTeammate, protocol.ToolSendMessage, protocol.ToolReadInbox, protocol.ToolBroadcast, protocol.ToolListTeammates, protocol.ToolRequestShutdown, protocol.ToolRequestPlan, protocol.ToolSubmitPlan, protocol.ToolReviewPlan, protocol.ToolListProtocols:
	default:
		return "", false, nil
	}
	h := g.handler
	h.mu.Lock()
	defer h.mu.Unlock()
	if authority.SessionID != h.binding.SessionID || authority.OwnerID != h.binding.OwnerID {
		return "Error: team authority does not match bound session", true, nil
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil || root != h.binding.Workspace {
		return "Error: team workspace does not match bound session", true, nil
	}
	if h.teamManager != nil {
		if _, err := h.teamManager.Get(h.binding.OwnerID, h.binding.SessionID); err != nil {
			return "Error: " + err.Error(), true, nil
		}
		if h.teamManager.State() != ManagerActive {
			return "Error: " + ErrManagerStopped.Error(), true, nil
		}
	}
	return "", false, ctx.Err()
}

// managerTeamDirectory projects immutable session bindings. It does not create
// members or transfer identity; teammate publication belongs to the manager.
type managerTeamDirectory struct{ manager *SessionManager }

func (d managerTeamDirectory) Member(identity teams.Identity) teams.MemberState {
	d.manager.mu.Lock()
	defer d.manager.mu.Unlock()
	for _, id := range d.manager.order {
		s := d.manager.sessions[id]
		if s != nil && s.core.team != nil && *s.core.team == identity && identity.Name != teams.Lead {
			return teams.MemberReady
		}
	}
	return teams.MemberMissing
}

func (manager *SessionManager) teamNames(team teams.TeamID) []teams.MemberName {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	names := []teams.MemberName{}
	for _, id := range manager.order {
		s := manager.sessions[id]
		if s != nil && s.core.team != nil && s.core.team.Team == team && s.core.team.Name != teams.Lead {
			names = append(names, s.core.team.Name)
		}
	}
	return names
}

func teamTraits(name protocol.ToolName) ToolTraits {
	switch name {
	case protocol.ToolSpawnTeammate:
		return ToolTraits{Risk: RiskExec}
	case protocol.ToolReadInbox, protocol.ToolListTeammates, protocol.ToolListProtocols:
		// Source declares read_inbox read-only despite consuming and routing acks.
		return ToolTraits{Risk: RiskRead, Readonly: true}
	default:
		return ToolTraits{Risk: RiskWrite}
	}
}

func (h *runtimeHandler) executeTeam(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	manager := h.teamManager
	if manager == nil {
		if input.Name() == protocol.ToolSpawnTeammate {
			return "Error: teams not available (no manager)", nil
		}
		if input.Name() == protocol.ToolSendMessage || input.Name() == protocol.ToolReadInbox {
			return "Error: message bus not available", nil
		}
		return "Error: teams not available", nil
	}
	// Ownership and current registration precede mailbox/protocol access. The
	// model cannot select a team, member identity, owner or mailbox root.
	session, err := manager.Get(h.binding.OwnerID, h.binding.SessionID)
	if err != nil {
		return "", err
	}
	if manager.State() != ManagerActive {
		return "", ErrManagerStopped
	}
	identity := session.core.team
	if identity == nil {
		return "", errors.New("managed session has no team identity")
	}
	switch input.Name() {
	case protocol.ToolSpawnTeammate:
		v, _ := input.SpawnTeammate()
		spawn, err := manager.SpawnTeammate(ctx, h.binding.OwnerID, h.binding.SessionID, SpawnTeammateRequest{Name: teams.MemberName(v.Name), Role: v.Role, Prompt: v.Prompt, RunContext: authority.RunContext})
		var refusal *TeamSpawnRefusal
		if errors.As(err, &refusal) {
			return refusal.Error(), nil
		}
		if err != nil {
			return "", err
		}
		return spawn.Render(), nil
	case protocol.ToolSendMessage:
		v, _ := input.SendMessage()
		names := manager.teamNames(identity.Team)
		if v.To != string(teams.Lead) && !slices.Contains(names, teams.MemberName(v.To)) {
			known := append(names, teams.Lead)
			slices.Sort(known)
			roster := make([]string, len(known))
			for i, name := range known {
				roster[i] = string(name)
			}
			return fmt.Sprintf("Error: no teammate named %s in this team; message not sent. Known recipients: %s", pytext.Repr(v.To), strings.Join(roster, ", ")), nil
		}
		var kind *teams.MessageType
		if v.Type != nil {
			value := teams.MessageType(*v.Type)
			kind = &value
		}
		fields := []teams.Field{}
		if v.Metadata != nil {
			data := v.Metadata.Value()
			for _, key := range data.Keys() {
				value, _ := data.Lookup(key)
				fields = append(fields, teams.Field{Name: key, Value: value})
			}
		}
		result, err := manager.teams.Send(ctx, teams.SendRequest{From: identity.Key(), To: teams.Key(identity.Team, teams.MemberName(v.To)), Content: v.Content, Type: kind, Metadata: teams.NewMetadata(fields...)})
		return result.Text, err
	case protocol.ToolReadInbox:
		inbox, err := manager.teamProtocols.Consume(ctx, *identity)
		if inbox.ShutdownRequested {
			session.core.teamShutdown.Store(true)
		}
		if err != nil {
			return "", err
		}
		if len(inbox.Messages) == 0 {
			return "(empty inbox)", nil
		}
		return teams.RenderMessages(inbox.Messages)
	case protocol.ToolBroadcast:
		v, _ := input.Broadcast()
		sent, refused := 0, []string{}
		kind := teams.MessageType("broadcast")
		for _, name := range manager.teamNames(identity.Team) {
			if name == identity.Name {
				continue
			}
			result, err := manager.teams.Send(ctx, teams.SendRequest{From: identity.Key(), To: teams.Key(identity.Team, name), Content: v.Content, Type: &kind})
			if err != nil {
				return "", err
			}
			if result.Status == teams.Refused {
				refused = append(refused, string(name)+": "+result.Text)
			} else {
				sent++
			}
		}
		if len(refused) > 0 {
			return fmt.Sprintf("Broadcast to %d teammate(s); %d refused (%s)", sent, len(refused), strings.Join(refused[:min(3, len(refused))], "; ")), nil
		}
		return fmt.Sprintf("Broadcast to %d teammate(s)", sent), nil
	case protocol.ToolListTeammates:
		names := manager.teamNames(identity.Team)
		if len(names) == 0 {
			return "No teammates.", nil
		}
		lines := make([]string, len(names))
		for i, name := range names {
			lines[i] = "  - " + string(name)
		}
		return strings.Join(lines, "\n"), nil
	case protocol.ToolRequestShutdown:
		if identity.Name != teams.Lead {
			return "Error: only the lead can request teammate shutdown", nil
		}
		v, _ := input.RequestShutdown()
		return manager.teamProtocols.RequestShutdown(ctx, identity.Team, teams.MemberName(v.Target), optionalText(v.Reason))
	case protocol.ToolRequestPlan:
		if identity.Name != teams.Lead {
			return "Error: only the lead can request plans", nil
		}
		v, _ := input.RequestPlan()
		return manager.teamProtocols.RequestPlan(ctx, identity.Team, teams.MemberName(v.Teammate), v.Task)
	case protocol.ToolSubmitPlan:
		v, _ := input.SubmitPlan()
		return manager.teamProtocols.SubmitPlan(ctx, *identity, v.Plan)
	case protocol.ToolReviewPlan:
		if identity.Name != teams.Lead {
			return "Error: only the lead can review plans", nil
		}
		v, _ := input.ReviewPlan()
		return manager.teamProtocols.ReviewPlan(ctx, identity.Team, teams.RequestID(v.RequestID), v.Approve, optionalText(v.Feedback))
	case protocol.ToolListProtocols:
		states := manager.teamProtocols.TeamProtocols(identity.Team)
		if len(states) == 0 {
			return "No protocol requests.", nil
		}
		return teams.RenderProtocols(states)
	}
	return "", errors.New("unsupported team input")
}

func optionalText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
