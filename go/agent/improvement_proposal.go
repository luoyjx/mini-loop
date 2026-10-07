package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/selfimprove"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/verifiedloop"
)

const EventImprovementProposed SessionEventKind = "improvement_proposed"

func (event SessionEvent) ImprovementProposed() (selfimprove.ProposedEvent, bool) {
	return cloneProposed(event.improvementProposed), event.kind == EventImprovementProposed
}
func cloneProposed(event selfimprove.ProposedEvent) selfimprove.ProposedEvent {
	event.TouchesVerifiers = append([]string{}, event.TouchesVerifiers...)
	event.ProposalID, event.ParentID = clonePointer(event.ProposalID), clonePointer(event.ParentID)
	return event
}

// ProposalRunOptions belongs to a trusted operator; no model tool installs it.
// The workspace must be isolated: source Git composition stages every changed path.
type ProposalRunOptions struct {
	AcceptanceCommand string
	MaxRounds         *int64
	Archive           *improvement.Archive
	ParentID          *improvement.ProposalID
}

// ProposeImprovementWithContext holds admission across verification, Git and the
// review index. The underlying session supplies the owner, worker and executor.
func (session *ManagedSession) ProposeImprovementWithContext(ctx context.Context, objective string, options ProposalRunOptions, run RunContext) (selfimprove.Proposal, error) {
	if session == nil || session.core == nil {
		return selfimprove.Proposal{}, errors.New("proposal session is not initialized")
	}
	options.MaxRounds, options.ParentID = clonePointer(options.MaxRounds), clonePointer(options.ParentID)
	var proposal selfimprove.Proposal
	_, err := session.runOperation(ctx, objective, run, false, nil, func(ctx context.Context, objective string, run RunContext) (string, error) {
		err := session.core.withVerifiedEffects(ctx, run, func(effects sessionVerifiedEffects) error {
			config := selfimprove.Config{Workspace: session.core.executionRoot(), RunID: verifiedloop.RunID(session.ID()), Worker: effects, Commands: effects, VerifiedEvents: effects, Events: effects}
			if options.Archive != nil {
				config.Archive = proposalArchive{ctx, effects, options.Archive}
			}
			service, err := selfimprove.NewService(config)
			if err != nil {
				return err
			}
			owner := improvement.ArchiveOwnerID(session.Owner())
			proposal, err = service.Propose(ctx, objective, selfimprove.Options{AcceptanceCommand: options.AcceptanceCommand, MaxRounds: options.MaxRounds, Owner: &owner, ParentID: options.ParentID})
			return err
		})
		return proposal.Summary, err
	})
	if err != nil {
		return selfimprove.Proposal{}, err
	}
	return proposal, nil
}

func (effects sessionVerifiedEffects) RunCommand(ctx context.Context, command string) (shell.Result, error) {
	return effects.RunAcceptance(ctx, command)
}
func (effects sessionVerifiedEffects) EmitProposal(ctx context.Context, event selfimprove.ProposedEvent) error {
	if err := effects.guard(ctx); err != nil {
		return err
	}
	effects.session.events.append(SessionEvent{kind: EventImprovementProposed, improvementProposed: cloneProposed(event)})
	return ctx.Err()
}

type proposalArchive struct {
	ctx     context.Context
	effects sessionVerifiedEffects
	archive *improvement.Archive
}

func (archive proposalArchive) Record(fields improvement.ProposalFields, options improvement.ArchiveRecordOptions) (improvement.ProposalID, error) {
	if err := archive.effects.guard(archive.ctx); err != nil {
		return "", err
	}
	return archive.archive.Record(fields, options)
}
