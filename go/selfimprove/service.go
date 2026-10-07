// Package selfimprove composes verified execution with reviewable Git proposals.
// It never merges. Callers must supply an isolated, operator-owned workspace.
package selfimprove

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/verifiedloop"
)

const nextReview = "review the diff on the branch; merge only after the paired benchmark and your own read agree it is an improvement"
const uncommitted = "(uncommitted: git add/commit failed in the worktree; the proposal is the working-tree change below)\n"

type CommandRunner interface {
	RunCommand(context.Context, string) (shell.Result, error)
}
type RepositoryChecker interface {
	IsRepository(context.Context, string) (bool, error)
}
type ProposalRecorder interface {
	Record(improvement.ProposalFields, improvement.ArchiveRecordOptions) (improvement.ProposalID, error)
}
type EventSink interface {
	EmitProposal(context.Context, ProposedEvent) error
}

type Config struct {
	Workspace      string
	RunID          verifiedloop.RunID
	Worker         verifiedloop.Worker
	Commands       CommandRunner
	VerifiedEvents verifiedloop.VerifiedEventSink
	Events         EventSink
	Archive        ProposalRecorder
	Repository     RepositoryChecker
}
type Service struct{ config Config }

// AdmissionError contains the two source operator-correctable refusal messages.
type AdmissionError struct{ Detail string }

func (e *AdmissionError) Error() string { return e.Detail }

func NewService(config Config) (*Service, error) {
	if config.Workspace == "" || config.Worker == nil || config.Commands == nil {
		return nil, errors.New("proposal requires workspace, worker and commands")
	}
	if config.Repository == nil {
		config.Repository = GitRepository{}
	}
	return &Service{config}, nil
}

type Options struct {
	AcceptanceCommand string
	MaxRounds         *int64
	Owner             *improvement.ArchiveOwnerID
	ParentID          *improvement.ProposalID
}
type Lineage struct {
	ProposalID improvement.ProposalID  `json:"proposal_id"`
	ParentID   *improvement.ProposalID `json:"parent_id"`
}
type Proposal struct {
	Objective        string                 `json:"objective"`
	Verified         bool                   `json:"verified"`
	Rounds           int64                  `json:"rounds"`
	Summary          string                 `json:"summary"`
	Workspace        string                 `json:"workspace"`
	Branch           string                 `json:"branch"`
	DiffStat         string                 `json:"diff_stat"`
	TouchesVerifiers []string               `json:"touches_verifiers"`
	Integrity        verifiedloop.Integrity `json:"integrity"`
	Next             string                 `json:"next"`
	Lineage          *Lineage               `json:"-"`
}

func (p Proposal) MarshalJSON() ([]byte, error) {
	type fields Proposal
	if p.Lineage == nil {
		return json.Marshal(fields(p))
	}
	return json.Marshal(struct {
		fields
		Lineage
	}{fields(p), *p.Lineage})
}

type ProposedEvent struct {
	Objective        string                  `json:"objective"`
	Verified         bool                    `json:"verified"`
	Branch           string                  `json:"branch"`
	DiffStat         string                  `json:"diff_stat"`
	TouchesVerifiers []string                `json:"touches_verifiers"`
	ProposalID       *improvement.ProposalID `json:"proposal_id"`
	ParentID         *improvement.ProposalID `json:"parent_id"`
}

func prefix(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}
func detached[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func call[T any](ctx context.Context, f func() (T, error)) (value T, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	defer func() {
		if recover() != nil {
			var zero T
			value = zero
			err = errors.New("proposal callback failed")
		}
	}()
	value, err = f()
	if canceled := ctx.Err(); canceled != nil {
		var zero T
		return zero, canceled
	}
	return
}

type acceptance struct{ commands CommandRunner }

func (a acceptance) RunAcceptance(ctx context.Context, command string) (shell.Result, error) {
	return a.commands.RunCommand(ctx, command)
}

func (s *Service) Propose(ctx context.Context, objective string, options Options) (Proposal, error) {
	if s == nil {
		return Proposal{}, errors.New("proposal service is not initialized")
	}
	options.MaxRounds, options.Owner, options.ParentID = detached(options.MaxRounds), detached(options.Owner), detached(options.ParentID)
	if pytext.Strip(options.AcceptanceCommand) == "" {
		return Proposal{}, &AdmissionError{"an improvement needs an acceptance command; without one the auditor has nothing to verify and 'verified' would be a vibe"}
	}
	ok, err := call(ctx, func() (bool, error) { return s.config.Repository.IsRepository(ctx, s.config.Workspace) })
	if err != nil {
		return Proposal{}, err
	}
	if !ok {
		return Proposal{}, &AdmissionError{"self-improvement runs only in a git checkout (worktree): a proposal must be diffable and revertible, or it is just a mutation"}
	}
	loop, err := verifiedloop.NewService(verifiedloop.ServiceConfig{RunID: s.config.RunID, Worker: s.config.Worker, Acceptance: acceptance{s.config.Commands}, Probe: verifiedloop.WorkspaceIntegrity{Workspace: s.config.Workspace}, Events: s.config.VerifiedEvents})
	if err != nil {
		return Proposal{}, err
	}
	outcome, err := loop.RunTask(ctx, objective, verifiedloop.TaskOptions{AcceptanceCommand: options.AcceptanceCommand, MaxRounds: options.MaxRounds})
	if err != nil {
		return Proposal{}, err
	}
	command := func(text string) (shell.Result, error) {
		return call(ctx, func() (shell.Result, error) { return s.config.Commands.RunCommand(ctx, text) })
	}
	status, err := command("git status --porcelain -uall")
	if err != nil {
		return Proposal{}, err
	}
	touched := []string{}
	diffText := "(no changes)"
	if pytext.Strip(status.Stdout) != "" {
		for _, line := range strings.FieldsFunc(status.Stdout, func(r rune) bool {
			return strings.ContainsRune("\n\r\v\f\x1c\x1d\x1e\u0085\u2028\u2029", r)
		}) {
			path := pytext.Strip(string([]rune(line)[min(3, len([]rune(line))):]))
			if _, after, ok := strings.Cut(path, " -> "); ok {
				path = after
			}
			if path != "" {
				touched = append(touched, path)
			}
		}
		staged, err := command("git add -A")
		if err != nil {
			return Proposal{}, err
		}
		committed, err := command("git commit -m 'self-improvement proposal' --no-verify")
		if err != nil {
			return Proposal{}, err
		}
		if staged.ExitCode != nil && *staged.ExitCode == 0 && committed.ExitCode != nil && *committed.ExitCode == 0 {
			diff, err := command("git diff --stat HEAD~1 HEAD")
			if err != nil {
				return Proposal{}, err
			}
			diffText = pytext.Strip(diff.Stdout)
		} else {
			diffText = uncommitted + pytext.Strip(status.Stdout)
		}
	}
	branch, err := command("git rev-parse --abbrev-ref HEAD")
	if err != nil {
		return Proposal{}, err
	}
	proposal := Proposal{Objective: objective, Verified: outcome.Status == verifiedloop.TaskComplete, Rounds: outcome.Rounds, Summary: outcome.Summary, Workspace: s.config.Workspace, Branch: pytext.Strip(branch.Stdout), DiffStat: diffText, TouchesVerifiers: improvement.VerifierTouches(touched), Integrity: outcome.Integrity, Next: nextReview}
	if len(proposal.TouchesVerifiers) > 0 {
		proposal.Next = "this proposal CHANGES THE ACCEPTANCE INSTRUMENTS (" + strings.Join(proposal.TouchesVerifiers[:min(5, len(proposal.TouchesVerifiers))], ", ") + "): verify the verifiers first, then " + proposal.Next
	}
	if s.config.Archive != nil {
		rounds := int(proposal.Rounds)
		if int64(rounds) != proposal.Rounds {
			return Proposal{}, errors.New("proposal rounds exceed archive counter profile")
		}
		integrity := improvement.Integrity(proposal.Integrity)
		touches := append([]string{}, proposal.TouchesVerifiers...)
		fields := improvement.ProposalFields{Objective: detached(&proposal.Objective), Verified: detached(&proposal.Verified), Rounds: &rounds, Branch: detached(&proposal.Branch), Workspace: detached(&proposal.Workspace), DiffStat: detached(&proposal.DiffStat), TouchesVerifiers: &touches, Integrity: &integrity}
		id, err := call(ctx, func() (improvement.ProposalID, error) {
			return s.config.Archive.Record(fields, improvement.ArchiveRecordOptions{Owner: options.Owner, ParentID: options.ParentID})
		})
		if err != nil {
			return Proposal{}, err
		}
		proposal.Lineage = &Lineage{id, detached(options.ParentID)}
	}
	if s.config.Events != nil {
		event := ProposedEvent{Objective: prefix(objective, 200), Verified: proposal.Verified, Branch: proposal.Branch, DiffStat: prefix(proposal.DiffStat, 500), TouchesVerifiers: append([]string{}, proposal.TouchesVerifiers[:min(10, len(proposal.TouchesVerifiers))]...), ParentID: detached(options.ParentID)}
		if proposal.Lineage != nil {
			event.ProposalID = detached(&proposal.Lineage.ProposalID)
		}
		_, err = call(ctx, func() (struct{}, error) { return struct{}{}, s.config.Events.EmitProposal(ctx, event) })
		if err != nil {
			return Proposal{}, err
		}
	}
	return proposal, nil
}

type ShellCommands struct{ Executor *shell.Executor }

func (s ShellCommands) RunCommand(ctx context.Context, text string) (shell.Result, error) {
	if s.Executor == nil {
		return shell.Result{}, errors.New("proposal command executor is not initialized")
	}
	return s.Executor.ExecuteBashResult(ctx, protocol.BashInput{Command: text})
}

type GitRepository struct{}

func (GitRepository) IsRepository(ctx context.Context, workspace string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, "git", "rev-parse", "--is-inside-work-tree")
	command.Dir = workspace
	err := command.Run()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return err == nil, nil
}
