package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/tasks"
	"github.com/luoyjx/mini-loop/go/teams"
	"github.com/luoyjx/mini-loop/go/worktrees"
)

func teammateRunContext(name teams.MemberName) (RunContext, error) {
	id, err := newMessageID()
	actor, delegate := ActorID(name), "lead"
	return RunContext{messageID: id, origin: "peer_agent", actorID: &actor, channel: "agent", authority: AuthorityPeerAgent, stampedBy: "session_manager", delegatedBy: &delegate}, err
}

func (manager *SessionManager) deliverTeammateResult(ctx context.Context, session *ManagedSession, result string, task *tasks.Task) error {
	identity := session.core.team
	kind := teams.MessageType("result")
	request := teams.SendRequest{From: identity.Key(), To: teams.Key(identity.Team, teams.Lead), Content: result, Type: &kind}
	if task != nil {
		request.Metadata = teams.NewMetadata(teams.Field{Name: "task_id", Value: teams.Text(string(task.ID))})
	}
	_, err := manager.teamProtocols.Deliver(ctx, request)
	return err
}

// Source polls once even when the sleep crosses the deadline. Activity resets
// the deadline after result delivery; cancellation never emits an idle notice.
func (manager *SessionManager) teammateIdleLoop(ctx context.Context, session *ManagedSession) error {
	core := session.core
	board := core.taskDiagnostics.Load()
	deadline := time.Now().Add(manager.config.TeamIdleTimeout)
	for time.Now().Before(deadline) {
		timer := time.NewTimer(manager.config.TeamIdlePoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		inbox, err := manager.consumeTeamInbox(ctx, core)
		if err != nil {
			return err
		}
		if core.teamShutdown.Swap(false) {
			return nil
		}
		if len(inbox.Messages) > 0 {
			text, err := teams.RenderRawMessages(inbox.Messages)
			if err != nil {
				return err
			}
			run, err := teammateRunContext(core.team.Name)
			if err != nil {
				return err
			}
			result, err := session.RunWithContext(ctx, "<team_inbox>\n"+text+"\n</team_inbox>", run)
			if err != nil {
				return err
			}
			if err = manager.deliverTeammateResult(ctx, session, result, nil); err != nil {
				return err
			}
			deadline = time.Now().Add(manager.config.TeamIdleTimeout)
			continue
		}
		runnable, err := board.Runnable()
		if err != nil {
			return err
		}
		var claimed *tasks.Task
		for _, candidate := range runnable {
			if err = ctx.Err(); err != nil {
				return err
			}
			result, err := board.Claim(candidate.ID, tasks.Owner(core.team.Name))
			if err != nil {
				return err
			}
			if strings.HasPrefix(result, "Claimed") {
				claimed, err = board.Load(candidate.ID)
				if err != nil {
					return err
				}
				break
			}
		}
		if claimed == nil {
			continue
		}
		target := core.workspace
		if claimed.Worktree != nil && manager.config.Services.Worktrees != nil {
			path, err := manager.config.Services.Worktrees.PathFor(worktrees.Name(*claimed.Worktree))
			if err == nil {
				if _, err := os.Stat(path); err == nil {
					target = path
				}
			}
		}
		run, err := teammateRunContext(core.team.Name)
		if err != nil {
			return err
		}
		prompt := fmt.Sprintf("You autonomously claimed %s: %s\n%s\nComplete the work, then call complete_task for %s.", claimed.ID, claimed.Subject, claimed.Description, claimed.ID)
		result, err := session.runOperation(ctx, prompt, run, false, nil, func(ctx context.Context, prompt string, run RunContext) (string, error) {
			// Managed admission excludes other turns; core/runtime locks protect
			// the same atomic dependency rebind used by enter_worktree.
			core.mu.Lock()
			core.runtime.mu.Lock()
			err := core.runtime.enterWorkspace(ctx, target)
			core.runtime.mu.Unlock()
			core.mu.Unlock()
			if err != nil {
				return "", err
			}
			return core.RunWithContext(ctx, prompt, run)
		})
		if err != nil {
			return err
		}
		if err = manager.deliverTeammateResult(ctx, session, result, claimed); err != nil {
			return err
		}
		deadline = time.Now().Add(manager.config.TeamIdleTimeout)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	kind := teams.MessageType("idle_notification")
	_, err := manager.teamProtocols.Deliver(ctx, teams.SendRequest{From: core.team.Key(), To: teams.Key(core.team.Team, teams.Lead), Content: "Idle timeout reached; teammate shut down.", Type: &kind})
	return err
}
