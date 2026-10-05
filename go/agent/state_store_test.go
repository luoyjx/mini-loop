package agent

import (
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestSessionRecordSnapshotCannotAlterOwnerOrBoundWorkspace(t *testing.T) {
	system := "fixed system"
	record := SessionRecord{SessionID: "session", Owner: "tenant", Workspace: "/checkout",
		System: &system, WorkspaceBound: true, Status: StatusRunning, RunCount: 3,
		Todos: []protocol.TodoItem{{Content: "original"}}, PendingSteering: []string{"queued"}}
	snapshot := record.Clone()
	*snapshot.System = "rewritten"
	snapshot.Todos[0].Content = "rewritten"
	snapshot.PendingSteering[0] = "rewritten"
	snapshot.Owner = "foreign"
	snapshot.WorkspaceBound = false
	if *record.System != "fixed system" || record.Todos[0].Content != "original" ||
		record.PendingSteering[0] != "queued" || record.Owner != "tenant" || !record.WorkspaceBound {
		t.Fatal("a detached persistence snapshot changed live authority or state")
	}
}
