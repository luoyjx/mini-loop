package sqlitestate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type sessionFixture struct {
	Input          agent.SessionRecord   `json:"input"`
	RawTodos       string                `json:"raw_todos"`
	RawSteering    string                `json:"raw_steering"`
	Initial        []agent.SessionRecord `json:"initial"`
	Updated        []agent.SessionRecord `json:"updated"`
	Reopened       []agent.SessionRecord `json:"reopened"`
	EmptyMetadata  []agent.SessionRecord `json:"empty_metadata"`
	AbsentOptional []agent.SessionRecord `json:"absent_optional"`
	BeforeDelete   map[string]int        `json:"before_delete"`
	FailedDelete   map[string]int        `json:"failed_delete"`
	Deleted        map[string]int        `json:"deleted"`
	DeleteError    string                `json:"delete_error"`
	LargeMetadata  struct {
		Count        int               `json:"count"`
		Todos        int               `json:"todos"`
		Steering     int               `json:"steering"`
		LastTodo     protocol.TodoItem `json:"last_todo"`
		LastSteering string            `json:"last_steering"`
	} `json:"large_metadata"`
}

func TestLargeTypedMetadataMatchesSource(t *testing.T) {
	fixture := sourceSessionFixture(t)
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	record := fixture.Input.Clone()
	record.Todos = make([]protocol.TodoItem, fixture.LargeMetadata.Count)
	record.PendingSteering = make([]string, fixture.LargeMetadata.Count)
	for index := range record.Todos {
		record.Todos[index] = protocol.TodoItem{Content: fmt.Sprintf("todo-%d", index), Status: "pending", ActiveForm: "working"}
		record.PendingSteering[index] = fmt.Sprintf("queued-%d", index)
	}
	ctx := context.Background()
	if err := store.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	rows, err := store.LoadSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	got := rows[0]
	if len(got.Todos) != fixture.LargeMetadata.Todos || len(got.PendingSteering) != fixture.LargeMetadata.Steering {
		t.Fatal("metadata list was truncated")
	}
	if got.Todos[len(got.Todos)-1] != fixture.LargeMetadata.LastTodo || got.PendingSteering[len(got.PendingSteering)-1] != fixture.LargeMetadata.LastSteering {
		t.Fatal("metadata tail differed from Source")
	}
}

func sourceSessionFixture(t *testing.T) sessionFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-sqlite-sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sessionFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func assertSessions(t *testing.T, store *Store, expected []agent.SessionRecord) {
	t.Helper()
	got, err := store.LoadSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("sessions:\n got %#v\nwant %#v", got, expected)
	}
}

func TestSessionMetadataBytesAndFallbacksMatchSource(t *testing.T) {
	fixture := sourceSessionFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	if err := left.UpsertSession(ctx, fixture.Input); err != nil {
		t.Fatal(err)
	}
	assertSessions(t, right, fixture.Initial)
	var todos, steering string
	if err := right.conn.QueryRowContext(ctx, "SELECT todos,pending_steering FROM sessions").Scan(&todos, &steering); err != nil {
		t.Fatal(err)
	}
	if todos != fixture.RawTodos || steering != fixture.RawSteering {
		t.Fatalf("metadata bytes differ from Source:\n%q\n%q", todos, steering)
	}
	updated := fixture.Updated[0].Clone()
	updated.CreatedAt = 999
	if err := left.UpsertSession(ctx, updated); err != nil {
		t.Fatal(err)
	}
	assertSessions(t, right, fixture.Updated)
	if err := left.Close(); err != nil {
		t.Fatal(err)
	}
	if err := right.Close(); err != nil {
		t.Fatal(err)
	}
	left = openStore(t, path)
	assertSessions(t, left, fixture.Reopened)
	if _, err := left.conn.ExecContext(ctx, "UPDATE sessions SET todos='', pending_steering='', workspace_bound='false'"); err != nil {
		t.Fatal(err)
	}
	assertSessions(t, left, fixture.EmptyMetadata)
	for _, column := range []string{"owner", "pending_steering", "workspace_bound"} {
		if _, err := left.conn.ExecContext(ctx, "ALTER TABLE sessions DROP COLUMN "+column); err != nil {
			t.Fatal(err)
		}
	}
	assertSessions(t, left, fixture.AbsentOptional)
}

func TestSessionOrderCursorAndDetachedReadsMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-state-store.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Sessions []struct {
			Name    string
			Records []agent.SessionRecord
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Sessions) != 2 {
		t.Fatalf("incomplete Source session capture: %d", len(fixture.Sessions))
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	// Insert in reverse chronology to exercise the SQL ordering, not insertion order.
	for i := len(fixture.Sessions[0].Records) - 1; i >= 0; i-- {
		record := fixture.Sessions[0].Records[i].Clone()
		record.EventCursor = 900
		if err := left.UpsertSession(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	assertSessions(t, right, fixture.Sessions[0].Records)
	updated := fixture.Sessions[1].Records[0].Clone()
	updated.CreatedAt = 999
	updated.EventCursor = 999
	if err := left.UpsertSession(ctx, updated); err != nil {
		t.Fatal(err)
	}
	if _, err := left.conn.ExecContext(ctx, "INSERT INTO events VALUES ('s',1,'{}'), ('s',2,'{}')"); err != nil {
		t.Fatal(err)
	}
	assertSessions(t, right, fixture.Sessions[1].Records)
	loaded, err := right.LoadSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	loaded[0].Todos[0].Content = "mutated"
	loaded[0].PendingSteering[0] = "mutated"
	*loaded[1].System = "mutated"
	assertSessions(t, right, fixture.Sessions[1].Records)
}

func databaseCounts(t *testing.T, store *Store) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{"sessions", "messages", "events", "actions", "approvals"} {
		var count int
		if err := store.conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func TestDeleteRollbackAndAuditRetentionMatchSource(t *testing.T) {
	fixture := sourceSessionFixture(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	if err := left.UpsertSession(ctx, fixture.Input); err != nil {
		t.Fatal(err)
	}
	// Seed the other row types directly until their typed SQL APIs are ported.
	for _, statement := range []string{
		"INSERT INTO messages VALUES ('saved', 1, 1, '{}')",
		"INSERT INTO events VALUES ('saved', 1, '{}')",
		"INSERT INTO actions (action_id,session_id,message_id,tool_use_id,tool_name,input_hash,status,created_at) VALUES ('action','saved','m','u','bash','hash','started',1)",
		"INSERT INTO approvals (approval_id,session_id,tool_name,rule,message,input_preview,status,created_at) VALUES ('approval','saved','bash','exec','allow?','echo safe','pending',1)",
		"CREATE TRIGGER reject_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'reject deletion'); END",
	} {
		if _, err := left.conn.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if got := databaseCounts(t, right); !reflect.DeepEqual(got, fixture.BeforeDelete) {
		t.Fatal(got)
	}
	if err := left.DeleteSession(ctx, "saved"); err == nil || err.Error() != fixture.DeleteError {
		t.Fatalf("delete error: %v", err)
	}
	if got := databaseCounts(t, right); !reflect.DeepEqual(got, fixture.FailedDelete) {
		t.Fatalf("partial deletion survived rollback: %v", got)
	}
	if _, err := left.conn.ExecContext(ctx, "DROP TRIGGER reject_delete"); err != nil {
		t.Fatal(err)
	}
	if err := left.DeleteSession(ctx, "saved"); err != nil {
		t.Fatal(err)
	}
	if got := databaseCounts(t, right); !reflect.DeepEqual(got, fixture.Deleted) {
		t.Fatalf("deleted audit rows: %v", got)
	}
	if err := left.DeleteSession(ctx, "saved"); err != nil {
		t.Fatal(err)
	}
	if err := left.Close(); err != nil {
		t.Fatal(err)
	}
	if err := right.Close(); err != nil {
		t.Fatal(err)
	}
	if got := databaseCounts(t, openStore(t, path)); !reflect.DeepEqual(got, fixture.Deleted) {
		t.Fatal(got)
	}
}

func TestCancelledCommitRollsBackAndReleasesWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	left.mu.Lock()
	err := left.withImmediate(ctx, func() error {
		if _, err := left.conn.ExecContext(ctx, "INSERT INTO schema_version VALUES (123)"); err != nil {
			return err
		}
		cancel()
		return nil
	})
	left.mu.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	var count int
	if err := right.conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM schema_version").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("cancelled transaction persisted: %d rows", count)
	}
	if err := right.UpsertSession(context.Background(), sourceSessionFixture(t).Input); err != nil {
		t.Fatalf("writer reservation leaked: %v", err)
	}
}

func TestSessionClosedCancelledAndUnsupportedMetadata(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	ctx := context.Background()
	record := sourceSessionFixture(t).Input
	if err := store.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.Todos[0].Content = string([]byte{0xed, 0xa0, 0x80})
	if err := store.UpsertSession(ctx, record); err == nil {
		t.Fatal("surrogate metadata silently replaced")
	}
	if _, err := store.conn.ExecContext(ctx, `UPDATE sessions SET todos='[{"content":"keep","unknown":"cannot silently drop"}]'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadSessions(ctx); !errors.Is(err, ErrSessionMetadata) {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.DeleteSession(cancelled, "saved"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(ctx, record); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := store.LoadSessions(ctx); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := store.DeleteSession(ctx, "saved"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestInvalidTextCannotMutateRowsOrClaims(t *testing.T) {
	fixture := sourceSessionFixture(t)
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	ctx := context.Background()
	if err := store.UpsertSession(ctx, fixture.Input); err != nil {
		t.Fatal(err)
	}
	invalid := string([]byte{0xed, 0xa0, 0x80})
	for _, record := range []agent.SessionRecord{
		{SessionID: "saved", Workspace: invalid},
		{SessionID: "saved", Workspace: "/replacement", System: &invalid},
	} {
		if err := store.UpsertSession(ctx, record); !errors.Is(err, jsonvalue.ErrSurrogate) {
			t.Fatal(err)
		}
	}
	assertSessions(t, store, fixture.Initial)
	if _, err := store.AcquireLease(ctx, "saved", agent.LeaseOwner(invalid), 0); !errors.Is(err, jsonvalue.ErrSurrogate) {
		t.Fatal(err)
	}
	if _, err := store.RenewLease(ctx, "saved", agent.LeaseOwner(invalid), 0); !errors.Is(err, jsonvalue.ErrSurrogate) {
		t.Fatal(err)
	}
	if err := store.ReleaseLease(ctx, "saved", agent.LeaseOwner(invalid)); !errors.Is(err, jsonvalue.ErrSurrogate) {
		t.Fatal(err)
	}
	if _, _, err := store.LeaseHolder(ctx, agent.SessionID(invalid)); !errors.Is(err, jsonvalue.ErrSurrogate) {
		t.Fatal(err)
	}
	if err := store.DeleteSession(ctx, agent.SessionID(invalid)); !errors.Is(err, jsonvalue.ErrSurrogate) {
		t.Fatal(err)
	}
	holder, held, err := store.LeaseHolder(ctx, "saved")
	if err != nil || held {
		t.Fatalf("invalid claim changed lease: %q/%v/%v", holder, held, err)
	}
}
