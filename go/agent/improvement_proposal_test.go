package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/improvement"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

func proposalGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(string(output), err)
	}
	return strings.TrimSpace(string(output))
}
func initProposalGit(t *testing.T, root string) {
	t.Helper()
	proposalGit(t, root, "init", "-b", "proposal")
	proposalGit(t, root, "config", "user.email", "test@example.invalid")
	proposalGit(t, root, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("baseline"), 0600); err != nil {
		t.Fatal(err)
	}
	proposalGit(t, root, "add", "base.txt")
	proposalGit(t, root, "commit", "-m", "baseline")
}

func TestManagedProposalCommitOwnerLineageAndEvents(t *testing.T) {
	session := verifiedManaged(t, &childContractProvider{role: RoleWorker}, nil)
	root := session.core.workspace
	initProposalGit(t, root)
	archive := improvement.NewArchive(t.TempDir(), runtimeSecrets())
	parent := improvement.ProposalID("imp_" + runtimeCanary)
	run, _ := DefaultRunContext()
	proposal, err := session.ProposeImprovementWithContext(context.Background(), "improve "+runtimeCanary, ProposalRunOptions{AcceptanceCommand: "test -f made.txt", Archive: archive, ParentID: &parent}, run)
	if err != nil || !proposal.Verified || proposal.Lineage == nil || !strings.Contains(proposal.DiffStat, "made.txt") {
		t.Fatal(proposal, err)
	}
	if proposalGit(t, root, "status", "--porcelain") != "" || proposalGit(t, root, "log", "-1", "--pretty=%s") != "self-improvement proposal" {
		t.Fatal("missing artifact")
	}
	owner := improvement.ArchiveOwnerID(session.Owner())
	rows, err := archive.List(context.Background(), improvement.ArchiveQuery{Owner: &owner})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if session.Info().RunCount != 1 || session.Info().Busy {
		t.Fatal(session.Info())
	}
	events := eventRecordsOfKind(session.Events(), EventImprovementProposed)
	if len(events) != 1 {
		t.Fatal(events)
	}
	data, err := json.Marshal(events[0])
	if err != nil || strings.Contains(string(data), runtimeCanary) {
		t.Fatal(string(data), err)
	}
	decoded, err := DecodeStoredEvent(data)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := decoded.Event.ImprovementProposed()
	if !ok || !v.Verified || v.ProposalID == nil || v.ParentID == nil {
		t.Fatal(v)
	}
	*v.ParentID = "mutated"
	again, _ := decoded.Event.ImprovementProposed()
	if *again.ParentID == "mutated" {
		t.Fatal("aliased proposed event")
	}
	if len(eventRecordsOfKind(session.Events(), EventDone)) != 1 {
		t.Fatal("managed completion missing")
	}
}

type parkedProposalBash struct {
	*shell.Executor
	started chan struct{}
}

func (b parkedProposalBash) ExecuteBashResult(ctx context.Context, input protocol.BashInput) (shell.Result, error) {
	if input.Command == "git status --porcelain -uall" {
		close(b.started)
		<-ctx.Done()
		return shell.Result{}, ctx.Err()
	}
	return b.Executor.ExecuteBashResult(ctx, input)
}
func TestManagedProposalKeepsAdmissionThroughGitAndCancelsBeforeStage(t *testing.T) {
	session := verifiedManaged(t, &childContractProvider{role: RoleWorker}, nil)
	root := session.core.workspace
	initProposalGit(t, root)
	baseline := proposalGit(t, root, "rev-parse", "HEAD")
	executor := session.core.bash.(*shell.Executor)
	parked := parkedProposalBash{executor, make(chan struct{})}
	session.core.bash = parked
	run, _ := DefaultRunContext()
	done := make(chan error, 1)
	go func() {
		proposal, err := session.ProposeImprovementWithContext(context.Background(), "write", ProposalRunOptions{AcceptanceCommand: "test -f made.txt"}, run)
		if proposal.Objective != "" {
			err = errors.New("cancel returned proposal")
		}
		done <- err
	}()
	<-parked.started
	if _, err := session.TryRunWithContext(context.Background(), "competing", run); !errors.Is(err, ErrSessionBusy) {
		t.Fatal(err)
	}
	if ok, err := session.Cancel(context.Background(), "operator"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if proposalGit(t, root, "rev-parse", "HEAD") != baseline || proposalGit(t, root, "diff", "--cached", "--name-only") != "" {
		t.Fatal("cancel staged or committed")
	}
	if len(eventRecordsOfKind(session.Events(), EventDone)) != 0 || len(eventRecordsOfKind(session.Events(), EventImprovementProposed)) != 0 {
		t.Fatal("cancel published proposal success")
	}
}
