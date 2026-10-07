package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerTeamBindingForkAndNoTeamProjection(t *testing.T) {
	manager := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	view, err := manager.PeekTeam(context.Background(), "alice", session.ID())
	if err != nil || view.Identity == nil || string(view.Identity.Team) != string(session.ID()) || view.Identity.Name != "lead" || len(view.Inbox) != 0 {
		t.Fatalf("default view %+v %v", view, err)
	}
	view.Identity.Name = "poison"
	view, err = manager.PeekTeam(context.Background(), "alice", session.ID())
	if err != nil || view.Identity.Name != "lead" {
		t.Fatal(view, err)
	}
	fork, err := manager.Fork(context.Background(), "alice", session.ID())
	if err != nil {
		t.Fatal(err)
	}
	child, err := manager.PeekTeam(context.Background(), "alice", fork.ID())
	if err != nil || child.Identity == nil || child.Identity.Team == view.Identity.Team {
		t.Fatal(child, err)
	}
	// A bare/custom agent may have no team. Removing this immutable binding is
	// confined to this test, before any concurrent readers are started.
	session.core.team = nil
	view, err = manager.PeekTeam(context.Background(), "alice", session.ID())
	if err != nil || view.Identity != nil || len(view.Inbox) != 0 {
		t.Fatal(view, err)
	}
}
func TestManagerTeamOwnerAdmissionPrecedesMailboxIO(t *testing.T) {
	manager := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	path := filepath.Join(manager.WorkspaceRoot(), ".teams", string(session.ID()), "inboxes", "lead.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.PeekTeam(context.Background(), "bob", session.ID()); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal(err)
	}
	if _, err := manager.PeekTeam(context.Background(), "alice", session.ID()); err == nil {
		t.Fatal("owned decoding failure hidden")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.PeekTeam(ctx, "alice", session.ID()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRestoredManagerSessionKeepsStableLeadTeamIdentity(t *testing.T) {
	store := newRuntimeStateStore()
	row := seedRestore(t, store, nil)
	manager := restoreManager(t, store, &FakeProvider{})
	session := restoreOne(t, manager)
	view, err := manager.PeekTeam(context.Background(), row.Owner, session.ID())
	if err != nil || view.Identity == nil || string(view.Identity.Team) != string(row.SessionID) || view.Identity.Name != "lead" {
		t.Fatalf("restored team %+v %v", view, err)
	}
}
