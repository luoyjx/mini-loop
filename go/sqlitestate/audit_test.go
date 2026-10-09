package sqlitestate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
)

func TestAuditRowsRecoveryAndImmutableIdentityMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-state-store.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Actions struct {
			AfterOpen       agent.ActionRecord   `json:"after_open"`
			ScopedUnknown   []agent.ActionID     `json:"scoped_unknown"`
			AfterScoped     []agent.ActionRecord `json:"after_scoped"`
			GlobalUnknown   []agent.ActionID     `json:"global_unknown"`
			RepeatedUnknown []agent.ActionID     `json:"repeated_unknown"`
			Updated         agent.ActionRecord   `json:"updated"`
		}
		Approvals []agent.ApprovalRecord
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	initial := fixture.Actions.AfterOpen.Clone()
	if err := left.WriteAction(ctx, initial); err != nil {
		t.Fatal(err)
	}
	other := initial.Clone()
	other.ActionID = "other-action"
	other.SessionID = "other"
	if err := left.WriteAction(ctx, other); err != nil {
		t.Fatal(err)
	}
	approval := fixture.Approvals[0].Clone()
	approval.Status = "pending"
	approval.ResolvedAt = nil
	approval.Answer = nil
	if err := left.WriteApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	updatedApproval := fixture.Approvals[0].Clone()
	updatedApproval.Message = "identity must stay"
	updatedApproval.Kind = "question"
	if err := left.WriteApproval(ctx, updatedApproval); err != nil {
		t.Fatal(err)
	}
	gotApprovals, err := right.ReadApprovals(ctx, "s", nil)
	if err != nil || !reflect.DeepEqual(gotApprovals, fixture.Approvals) {
		t.Fatalf("approvals: %#v/%v", gotApprovals, err)
	}
	if err := left.Close(); err != nil {
		t.Fatal(err)
	}
	left = openStore(t, path)
	got, found, err := left.ReadAction(ctx, initial.ActionID)
	if err != nil || !found || !reflect.DeepEqual(got, fixture.Actions.AfterOpen) {
		t.Fatalf("open marked actions: %#v/%v", got, err)
	}
	session := agent.SessionID("s")
	ids, err := left.MarkInflightUnknown(ctx, &session)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if err != nil || !reflect.DeepEqual(ids, fixture.Actions.ScopedUnknown) {
		t.Fatalf("scoped unknown: %v/%v", ids, err)
	}
	for _, expected := range fixture.Actions.AfterScoped {
		got, found, err := right.ReadAction(ctx, expected.ActionID)
		if err != nil || !found || !reflect.DeepEqual(got, expected) {
			t.Fatalf("scoped row: %#v/%v", got, err)
		}
	}
	ids, err = right.MarkInflightUnknown(ctx, nil)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if err != nil || !reflect.DeepEqual(ids, fixture.Actions.GlobalUnknown) {
		t.Fatalf("global unknown: %v/%v", ids, err)
	}
	ids, err = left.MarkInflightUnknown(ctx, nil)
	if err != nil || !reflect.DeepEqual(ids, fixture.Actions.RepeatedUnknown) {
		t.Fatalf("repeat: %v/%v", ids, err)
	}
	update := fixture.Actions.Updated.Clone()
	update.SessionID = "cannot-transfer"
	update.InputHash = "cannot-change"
	update.CreatedAt = 999
	if err := left.WriteAction(ctx, update); err != nil {
		t.Fatal(err)
	}
	got, found, err = right.ReadAction(ctx, update.ActionID)
	if err != nil || !found || !reflect.DeepEqual(got, fixture.Actions.Updated) {
		t.Fatalf("immutable action: %#v/%v", got, err)
	}
	*got.Result = "mutated"
	got, _, err = left.ReadAction(ctx, update.ActionID)
	if err != nil || *got.Result != *fixture.Actions.Updated.Result {
		t.Fatal("loaded result aliases storage")
	}
	if err := left.DeleteSession(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := right.ReadAction(ctx, initial.ActionID); err != nil || !found {
		t.Fatal("deletion removed audit")
	}
	gotApprovals, err = right.ReadApprovals(ctx, "s", nil)
	if err != nil || !reflect.DeepEqual(gotApprovals, fixture.Approvals) {
		t.Fatal("deletion removed approval audit")
	}
}

func TestNullableApprovalIdentityAndStatusOrdering(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	for _, record := range []agent.ApprovalRecord{
		{ApprovalID: "null", SessionID: "s", ToolUseIDNull: true, ToolName: "bash", Status: "pending", Kind: "approval", CreatedAt: 2},
		{ApprovalID: "empty", SessionID: "s", ToolUseID: "", ToolName: "bash", Status: "cancelled", Kind: "approval", CreatedAt: 1},
	} {
		if err := store.WriteApproval(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.ReadApprovals(ctx, "s", nil)
	if err != nil || len(rows) != 2 || rows[0].ApprovalID != "empty" || rows[0].ToolUseIDNull || !rows[1].ToolUseIDNull {
		t.Fatalf("nullable ordering: %#v/%v", rows, err)
	}
	for _, row := range rows {
		encoded, err := row.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var decoded agent.ApprovalRecord
		if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, row) {
			t.Fatalf("nullable JSON: %s/%v", encoded, err)
		}
	}
	status := agent.ApprovalStatus("pending")
	filtered, err := store.ReadApprovals(ctx, "s", &status)
	if err != nil || len(filtered) != 1 || filtered[0].ApprovalID != "null" {
		t.Fatalf("status filter: %#v/%v", filtered, err)
	}
}

func TestRecoveryRollbackMissingAndClosedRows(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	for _, id := range []agent.ActionID{"first", "reject"} {
		if err := store.WriteAction(ctx, agent.ActionRecord{ActionID: id, SessionID: "s", ToolName: "bash", Status: agent.ActionStarted}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.conn.ExecContext(ctx, "CREATE TRIGGER reject_recovery BEFORE UPDATE ON actions WHEN OLD.action_id='reject' BEGIN SELECT RAISE(ABORT,'reject recovery'); END"); err != nil {
		t.Fatal(err)
	}
	if ids, err := store.MarkInflightUnknown(ctx, nil); err == nil || ids != nil {
		t.Fatalf("recovery failure: %v/%v", ids, err)
	}
	for _, id := range []agent.ActionID{"first", "reject"} {
		row, found, err := store.ReadAction(ctx, id)
		if err != nil || !found || row.Status != agent.ActionStarted {
			t.Fatalf("partial recovery: %#v/%v", row, err)
		}
	}
	if _, found, err := store.ReadAction(ctx, "missing"); err != nil || found {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteAction(ctx, agent.ActionRecord{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, _, err := store.ReadAction(ctx, "first"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := store.MarkInflightUnknown(ctx, nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := store.WriteApproval(ctx, agent.ApprovalRecord{}); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := store.ReadApprovals(ctx, "s", nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
