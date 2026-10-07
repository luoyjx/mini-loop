package selfimprove

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/verifiedloop"
)

type commandFunc func(context.Context, string) (shell.Result, error)

func (f commandFunc) RunCommand(ctx context.Context, s string) (shell.Result, error) {
	return f(ctx, s)
}

type workerFunc func(context.Context, string) (string, error)

func (f workerFunc) RunWorker(ctx context.Context, s string) (string, error) { return f(ctx, s) }

type repositoryFunc func(context.Context, string) (bool, error)

func (f repositoryFunc) IsRepository(ctx context.Context, s string) (bool, error) { return f(ctx, s) }

type archiveFunc func(improvement.ProposalFields, improvement.ArchiveRecordOptions) (improvement.ProposalID, error)

func (f archiveFunc) Record(p improvement.ProposalFields, o improvement.ArchiveRecordOptions) (improvement.ProposalID, error) {
	return f(p, o)
}

type eventFunc func(context.Context, ProposedEvent) error

func (f eventFunc) EmitProposal(ctx context.Context, e ProposedEvent) error { return f(ctx, e) }

type verifiedEventFunc func(context.Context, verifiedloop.VerifiedEvent) error

func (f verifiedEventFunc) EmitVerifiedEvent(ctx context.Context, e verifiedloop.VerifiedEvent) error {
	return f(ctx, e)
}

type proposalWire struct {
	Objective, Summary, Workspace, Branch string
	Verified                              bool
	Rounds                                int64
	DiffStat                              string   `json:"diff_stat"`
	Touches                               []string `json:"touches_verifiers"`
	Integrity                             verifiedloop.Integrity
	Next                                  string
	ProposalID                            *improvement.ProposalID `json:"proposal_id"`
	ParentID                              *improvement.ProposalID `json:"parent_id"`
}
type archiveWire struct {
	Fields   improvement.ProposalFields `json:"fields"`
	Owner    improvement.ArchiveOwnerID `json:"owner"`
	ParentID *improvement.ProposalID    `json:"parent_id"`
}

func TestActualPythonProposalComposition(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name           string
			Status         string
			Objective      *string
			Command        *string
			Maximum        *int64
			AcceptanceExit *int            `json:"acceptance_exit"`
			AddExit        *int            `json:"add_exit"`
			CommitExit     json.RawMessage `json:"commit_exit"`
			Archive        bool
			Repository     *bool
			Owner          *improvement.ArchiveOwnerID
			Parent         *improvement.ProposalID
			Failure        string
			Calls          []string
			Events         []ProposedEvent
			ArchiveRows    []archiveWire `json:"archive_rows"`
			Proposal       *proposalWire
			Error          *string
		}
	}
	data, err := os.ReadFile("../testdata/python-improvement-proposal.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 17 {
		t.Fatal(len(fixture.Cases))
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			calls := []string{}
			events := []ProposedEvent{}
			rows := []archiveWire{}
			zero := 0
			commands := commandFunc(func(_ context.Context, text string) (shell.Result, error) {
				calls = append(calls, text)
				if recipe.Failure == text {
					return shell.Result{}, errors.New(text + " failed")
				}
				result := shell.Result{ExitCode: &zero}
				switch text {
				case "accept":
					result.ExitCode = recipe.AcceptanceExit
					if result.ExitCode == nil {
						result.ExitCode = &zero
					}
				case "git status --porcelain -uall":
					result.Stdout = recipe.Status
				case "git add -A":
					if recipe.AddExit != nil {
						result.ExitCode = recipe.AddExit
					}
				case "git commit -m 'self-improvement proposal' --no-verify":
					if recipe.CommitExit != nil {
						if err := json.Unmarshal(recipe.CommitExit, &result.ExitCode); err != nil {
							t.Fatal(err)
						}
					}
				case "git diff --stat HEAD~1 HEAD":
					result.Stdout = " code.py | 1 +\n"
				case "git rev-parse --abbrev-ref HEAD":
					result.Stdout = " proposal-branch\n"
				default:
					t.Fatal(text)
				}
				return result, nil
			})
			config := Config{Workspace: t.TempDir(), RunID: "proposal-run", Commands: commands,
				Worker: workerFunc(func(context.Context, string) (string, error) {
					calls = append(calls, "worker")
					return "worker-summary", nil
				}),
				Repository: repositoryFunc(func(context.Context, string) (bool, error) {
					return recipe.Repository == nil || *recipe.Repository, nil
				}),
				VerifiedEvents: verifiedEventFunc(func(_ context.Context, e verifiedloop.VerifiedEvent) error {
					calls = append(calls, string(e.Kind()))
					return nil
				}),
				Events: eventFunc(func(_ context.Context, e ProposedEvent) error {
					calls = append(calls, "improvement_proposed")
					if recipe.Failure == "event" {
						return errors.New("event failed")
					}
					events = append(events, e)
					return nil
				}),
			}
			if recipe.Archive {
				config.Archive = archiveFunc(func(fields improvement.ProposalFields, options improvement.ArchiveRecordOptions) (improvement.ProposalID, error) {
					calls = append(calls, "archive")
					if recipe.Failure == "archive" {
						return "", errors.New("archive failed")
					}
					*fields.Workspace = "<workspace>"
					owner := improvement.ArchiveOwnerID("anonymous")
					if options.Owner != nil {
						owner = *options.Owner
					}
					rows = append(rows, archiveWire{fields, owner, options.ParentID})
					return "imp_fixture", nil
				})
			}
			service, err := NewService(config)
			if err != nil {
				t.Fatal(err)
			}
			objective, command := "improve code", "accept"
			if recipe.Objective != nil {
				objective = *recipe.Objective
			}
			if recipe.Command != nil {
				command = *recipe.Command
			}
			maximum := int64(1)
			if recipe.Maximum != nil {
				maximum = *recipe.Maximum
			}
			proposal, err := service.Propose(context.Background(), objective, Options{AcceptanceCommand: command, MaxRounds: &maximum, Owner: recipe.Owner, ParentID: recipe.Parent})
			if recipe.Error != nil {
				if err == nil || err.Error() != *recipe.Error {
					t.Fatal(err, recipe.Error)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				proposal.Workspace = "<workspace>"
				encoded, err := json.Marshal(proposal)
				if err != nil {
					t.Fatal(err)
				}
				var got proposalWire
				if err = json.Unmarshal(encoded, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(&got, recipe.Proposal) {
					t.Fatalf("got %+v want %+v", got, recipe.Proposal)
				}
				if recipe.Archive && proposal.Lineage.ParentID == nil && !strings.Contains(string(encoded), `"parent_id":null`) {
					t.Fatal(string(encoded))
				}
				if !recipe.Archive && strings.Contains(string(encoded), `"parent_id"`) {
					t.Fatal(string(encoded))
				}
			}
			if !reflect.DeepEqual(calls, recipe.Calls) || !reflect.DeepEqual(events, recipe.Events) || !reflect.DeepEqual(rows, recipe.ArchiveRows) {
				t.Fatalf("calls %v want %v; events %+v/%+v; rows %+v/%+v", calls, recipe.Calls, events, recipe.Events, rows, recipe.ArchiveRows)
			}
		})
	}
}

func TestRealGitProposalAndLineage(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(data), err)
		}
		return strings.TrimSpace(string(data))
	}
	git("init", "-b", "proposal")
	git("config", "user.email", "test@example.invalid")
	git("config", "user.name", "Test")
	os.WriteFile(filepath.Join(root, "base.txt"), []byte("base"), 0600)
	git("add", "base.txt")
	git("commit", "-m", "baseline")
	baseline := git("rev-parse", "HEAD")
	executor, err := shell.New(shell.Config{Workspace: root, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	archive := improvement.NewArchive(t.TempDir(), nil)
	worker := workerFunc(func(context.Context, string) (string, error) {
		return "done", os.WriteFile(filepath.Join(root, "made.txt"), []byte("proof"), 0600)
	})
	service, err := NewService(Config{Workspace: root, RunID: "real", Worker: worker, Commands: ShellCommands{executor}, Archive: archive})
	if err != nil {
		t.Fatal(err)
	}
	owner := improvement.ArchiveOwnerID("tenant")
	parent := improvement.ProposalID("imp_parent")
	proposal, err := service.Propose(context.Background(), "improve", Options{AcceptanceCommand: "test -f made.txt", Owner: &owner, ParentID: &parent})
	if err != nil || !proposal.Verified || proposal.Branch != "proposal" || proposal.Lineage == nil || !strings.Contains(proposal.DiffStat, "made.txt") {
		t.Fatal(proposal, err)
	}
	if git("rev-parse", "HEAD") == baseline || git("status", "--porcelain") != "" || git("log", "-1", "--pretty=%s") != "self-improvement proposal" {
		t.Fatal("proposal artifact not committed")
	}
	rows, err := archive.List(context.Background(), improvement.ArchiveQuery{Owner: &owner})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	// Acceptance refusal still produces a reviewable branch commit.
	worker = workerFunc(func(context.Context, string) (string, error) {
		return "claims success", os.WriteFile(filepath.Join(root, "failed.txt"), []byte("candidate"), 0600)
	})
	service, _ = NewService(Config{Workspace: root, RunID: "failed", Worker: worker, Commands: ShellCommands{executor}})
	maximum := int64(1)
	proposal, err = service.Propose(context.Background(), "attempt", Options{AcceptanceCommand: "false", MaxRounds: &maximum})
	if err != nil || proposal.Verified || !strings.Contains(proposal.DiffStat, "failed.txt") || git("status", "--porcelain") != "" {
		t.Fatal(proposal, err)
	}
}

func TestProposalCancellationPrivatePanicAndRepositoryRefusal(t *testing.T) {
	calls := 0
	config := Config{Workspace: t.TempDir(), Worker: workerFunc(func(context.Context, string) (string, error) { calls++; return "", nil }), Commands: commandFunc(func(context.Context, string) (shell.Result, error) { calls++; panic("credential") })}
	service, _ := NewService(config)
	if _, err := service.Propose(context.Background(), "objective", Options{AcceptanceCommand: "true"}); err == nil || calls != 0 {
		t.Fatal(err, calls)
	}
	config.Repository = repositoryFunc(func(context.Context, string) (bool, error) { return true, nil })
	service, _ = NewService(config)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Propose(ctx, "objective", Options{AcceptanceCommand: "true"}); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(err, calls)
	}
	if _, err := service.Propose(context.Background(), "objective", Options{AcceptanceCommand: "true"}); err == nil || strings.Contains(err.Error(), "credential") {
		t.Fatal(err)
	}
}
