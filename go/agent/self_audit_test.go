package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

type auditRecordingStore struct {
	TrajectoryStore
	list  func(TrajectoryQuery) ([]TrajectorySummary, error)
	visit func(context.Context, TrajectoryID, TrajectoryEventQuery, func([]byte) error) error
}

func (s *auditRecordingStore) Count(SessionID) (int, error)                        { return 0, nil }
func (s *auditRecordingStore) List(q TrajectoryQuery) ([]TrajectorySummary, error) { return s.list(q) }
func (s *auditRecordingStore) VisitRecords(ctx context.Context, id TrajectoryID, q TrajectoryEventQuery, v func([]byte) error) error {
	return s.visit(ctx, id, q, v)
}

func TestSelfAuditOwnerAdmissionPrecedesEveryRecordingBody(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	store := &auditRecordingStore{}
	config.Services.Trajectories = store
	manager := makeManager(t, config)
	alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	bob := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
	owner := "alice"
	wrongOwner := OwnerID("bob")
	duration := 1250.0
	lists, visits := 0, 0
	store.list = func(q TrajectoryQuery) ([]TrajectorySummary, error) {
		manager.Summary() // proves the manager mutex is released before callbacks.
		lists++
		if q.Session == nil || *q.Session != alice.ID() || q.Limit != 10 {
			t.Fatalf("unowned query: %+v", q)
		}
		return []TrajectorySummary{
			{ID: "other-session", Session: bob.ID(), Owner: &wrongOwner},
			{ID: "forged-owner", Session: alice.ID(), Owner: &wrongOwner},
			{ID: "owned", Session: alice.ID(), Status: TrajectoryError, DurationMS: &duration},
		}, nil
	}
	store.visit = func(ctx context.Context, id TrajectoryID, q TrajectoryEventQuery, v func([]byte) error) error {
		visits++
		if id != "owned" || q.Limit != 200 || len(q.Types) != 1 || q.Types[0] != EventToolUse {
			t.Fatalf("unowned visit: %s %+v", id, q)
		}
		return v([]byte(`{"type":"tool_use","name":"load_skill","input":{"name":"safe"}}`))
	}
	result := manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{Owner: &owner})
	report := selfaudit.BuildReport(result, selfaudit.Scope{Owner: &owner})
	if lists != 1 || visits != 1 || len(result.Sessions) != 1 || *result.TotalSessions != 1 {
		t.Fatalf("read bounds: %d/%d %+v", lists, visits, result)
	}
	if !strings.Contains(report, "safe: 1 load(s), 1 in turns") || strings.Contains(report, "cron") || strings.Contains(report, string(bob.ID())) {
		t.Fatal(report)
	}
	// Cancellation is observed before storage IO.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := manager.ObserveSelfAudit(ctx, selfaudit.Scope{Owner: &owner})
	if lists != 1 || visits != 1 || cancelled.Trajectories.TrendsFailure.Class != "CancelledError" {
		t.Fatal("cancelled observation read storage")
	}
}

func TestSelfAuditRecordingAndEventScanCaps(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	store := &auditRecordingStore{}
	config.Services.Trajectories = store
	manager := makeManager(t, config)
	createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	visits := 0
	store.list = func(q TrajectoryQuery) ([]TrajectorySummary, error) {
		if q.Session != nil || q.Limit != 50 {
			t.Fatalf("fleet query %+v", q)
		}
		rows := []TrajectorySummary{}
		for i := 0; i < 60; i++ {
			rows = append(rows, TrajectorySummary{ID: TrajectoryID(fmt.Sprint(i)), Status: TrajectoryCompleted})
		}
		return rows, nil
	}
	store.visit = func(_ context.Context, _ TrajectoryID, q TrajectoryEventQuery, v func([]byte) error) error {
		visits++
		for i := 0; i < q.Limit; i++ {
			if err := v([]byte(`{"name":"read_file"}`)); err != nil {
				return err
			}
		}
		return v([]byte(`{"name":"load_skill","input":{"name":"over-budget"}}`))
	}
	result := manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{IncludeGlobal: true})
	report := selfaudit.BuildReport(result, selfaudit.Scope{IncludeGlobal: true})
	if visits != 50 || len(result.Trajectories.Global) != 50 || strings.Contains(report, "over-budget") {
		t.Fatal("record/event cap not respected")
	}
}

func TestSelfAuditRecordingFaultsRemainPrivateAndSeparate(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	store := &auditRecordingStore{}
	config.Services.Trajectories = store
	manager := makeManager(t, config)
	store.list = func(TrajectoryQuery) ([]TrajectorySummary, error) {
		return []TrajectorySummary{{ID: "ok", Status: TrajectoryCompleted}}, nil
	}
	store.visit = func(context.Context, TrajectoryID, TrajectoryEventQuery, func([]byte) error) error {
		return errors.New("private credential")
	}
	report := selfaudit.BuildReport(manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{}), selfaudit.Scope{})
	if !strings.Contains(report, "1 completed") || !strings.Contains(report, "unreadable: OSError") || strings.Contains(report, "private credential") {
		t.Fatal(report)
	}
	store.list = func(TrajectoryQuery) ([]TrajectorySummary, error) { panic("private panic") }
	report = selfaudit.BuildReport(manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{}), selfaudit.Scope{})
	if strings.Contains(report, "private panic") || strings.Count(report, "unreadable: RuntimeError") != 2 {
		t.Fatal(report)
	}
}

func TestSelfAuditConcurrentSnapshotsAndGateCounts(t *testing.T) {
	manager := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	owner := "alice"
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				session.core.gate.recordProblem("observer fault")
				_ = manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{Owner: &owner})
			}
		}()
	}
	wg.Wait()
	ledger := manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{Owner: &owner}).Sessions[0].Problems.Registry
	if ledger.Total.String() != "200" || len(ledger.Entries) != 1 || len(session.core.gate.Problems()) != 100 {
		t.Fatalf("gate diagnostics %+v", ledger)
	}
}

func TestActualSourceManagerSelfAudit(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name          string
			Owner         *string
			IncludeGlobal bool `json:"include_global"`
			Sessions      []struct {
				Owner   string
				Created float64
				Status  SessionStatus
			}
			Report string
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-self-audit-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			manager := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
			for _, input := range row.Sessions {
				session := createManaged(t, manager, CreateSessionRequest{Owner: OwnerID(input.Owner)})
				session.createdAt = input.Created
				session.status = input.Status
			}
			scope := selfaudit.Scope{Owner: row.Owner, IncludeGlobal: row.IncludeGlobal}
			got := selfaudit.BuildReport(manager.ObserveSelfAudit(context.Background(), scope), scope)
			if got != row.Report {
				t.Fatalf("report\ngot %s\nwant %s", got, row.Report)
			}
		})
	}
}

func TestSelfAuditOwnerRecordingSessionBudget(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	store := &auditRecordingStore{}
	config.Services.Trajectories = store
	manager := makeManager(t, config)
	for i := 0; i < 21; i++ {
		createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	}
	owner := "alice"
	lists, visits := 0, 0
	store.list = func(q TrajectoryQuery) ([]TrajectorySummary, error) {
		lists++
		if q.Session == nil || q.Limit != 10 {
			t.Fatal(q)
		}
		return nil, nil
	}
	store.visit = func(context.Context, TrajectoryID, TrajectoryEventQuery, func([]byte) error) error {
		visits++
		return nil
	}
	manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{Owner: &owner})
	if lists != 20 || visits != 0 {
		t.Fatalf("empty owned recording scan: %d/%d", lists, visits)
	}
	lists = 0
	store.list = func(q TrajectoryQuery) ([]TrajectorySummary, error) {
		lists++
		rows := []TrajectorySummary{}
		for i := 0; i < 10; i++ {
			rows = append(rows, TrajectorySummary{ID: TrajectoryID(fmt.Sprintf("%s-%d", *q.Session, i)), Session: *q.Session, Status: TrajectoryCompleted})
		}
		return rows, nil
	}
	manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{Owner: &owner})
	if lists != 5 || visits != 50 {
		t.Fatalf("full owned recording scan: %d/%d", lists, visits)
	}
}

func TestSelfAuditEventPanicDoesNotHideTrajectoryTrends(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	store := &auditRecordingStore{}
	config.Services.Trajectories = store
	manager := makeManager(t, config)
	store.list = func(TrajectoryQuery) ([]TrajectorySummary, error) {
		return []TrajectorySummary{{ID: "ok", Status: TrajectoryCompleted}}, nil
	}
	store.visit = func(context.Context, TrajectoryID, TrajectoryEventQuery, func([]byte) error) error {
		panic("private event payload")
	}
	report := selfaudit.BuildReport(manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{}), selfaudit.Scope{})
	if !strings.Contains(report, "1 completed") || strings.Count(report, "unreadable: RuntimeError") != 1 || strings.Contains(report, "private event payload") {
		t.Fatal(report)
	}
}

func TestSelfAuditLazyTaskDiagnosticsAreObservedWithoutInitializingAnotherStore(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.TaskTools = true
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	owner := "alice"
	before := manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{Owner: &owner})
	if before.Sessions[0].Problems.Tasks != nil {
		t.Fatal("observation initialized task board")
	}
	directory := filepath.Join(session.core.workspace, ".tasks")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "task_broken.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	input, err := protocol.DecodeToolInput(protocol.ToolListTasks, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	dispatchManagerMemory(t, session, input)
	after := manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{Owner: &owner})
	if after.Sessions[0].Problems.Tasks == nil || len(after.Sessions[0].Problems.Tasks.Entries) == 0 {
		t.Fatal("live task diagnostic not observed")
	}
}

func TestSelfAuditEventObservationByteBudget(t *testing.T) {
	store := &auditRecordingStore{}
	padding := strings.Repeat("x", 1024*1024)
	data := []byte(`{"name":"read_file","padding":"` + padding + `"}`)
	store.visit = func(_ context.Context, _ TrajectoryID, _ TrajectoryEventQuery, visit func([]byte) error) error {
		return visit(data)
	}
	budget := 64 * 1024 * 1024
	var failure *selfaudit.Failure
	for i := 0; i < 64; i++ {
		_, failure = observeAuditEvents(context.Background(), store, "budget", &budget)
		if failure != nil {
			break
		}
	}
	if failure == nil || failure.Class != "ObservationLimitError" || budget < 0 {
		t.Fatalf("byte budget: %d %+v", budget, failure)
	}
}
