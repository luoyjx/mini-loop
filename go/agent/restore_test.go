package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type restoreRecipe struct {
	Name           string              `json:"name"`
	Initial        []protocol.Message  `json:"initial"`
	Restored       []protocol.Message  `json:"restored"`
	Epoch          TranscriptEpoch     `json:"epoch"`
	PhysicalCursor EventSequence       `json:"physical_cursor"`
	SourceSequence EventSequence       `json:"seq_after_restore"`
	Status         SessionStatus       `json:"status"`
	Busy           bool                `json:"busy"`
	Mode           PermissionMode      `json:"mode"`
	Owner          OwnerID             `json:"owner"`
	CreatedAt      float64             `json:"created_at"`
	RunCount       int                 `json:"run_count"`
	Bound          bool                `json:"bound"`
	Todos          []protocol.TodoItem `json:"live_todos"`
	Steering       []string            `json:"live_steering"`
	SavedTodos     []protocol.TodoItem `json:"saved_todos"`
	SavedSteering  []string            `json:"saved_steering"`
	Repaired       []string            `json:"repaired"`
	Confirmed      bool                `json:"confirmed"`
	SecondMessages int                 `json:"second_messages"`
}

func restoreRecipes(t *testing.T) []restoreRecipe {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-state-restore.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases   []restoreRecipe `json:"cases"`
		Unknown string          `json:"unknown_result"`
		NotRun  string          `json:"not_run_result"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 7 || fixture.Unknown != unknownToolResult || fixture.NotRun != NotRunActionResult {
		t.Fatal("source fixture changed")
	}
	return fixture.Cases
}
func seedRestore(t *testing.T, store *runtimeStateStore, initial []protocol.Message) SessionRecord {
	t.Helper()
	system := "recorded system"
	row := SessionRecord{SessionID: "saved", Workspace: filepath.Join(t.TempDir(), "missing"), System: &system, CreatedAt: 10, RunCount: 4, Status: StatusRunning, Owner: "tenant", WorkspaceBound: true, Todos: []protocol.TodoItem{{Content: "remember", Status: "pending", ActiveForm: "remembering"}}, PendingSteering: []string{"queued request"}}
	if err := store.UpsertSession(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessages(context.Background(), row.SessionID, initial, 2); err != nil {
		t.Fatal(err)
	}
	event, err := DecodeStoredEvent([]byte(`{"type":"status","status":"running","seq":9,"session":"saved","transcript_epoch":2,"ts":10}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AppendEvent(context.Background(), row.SessionID, event); err != nil {
		t.Fatal(err)
	}
	return row
}
func restoreManager(t *testing.T, store StateStore, provider Provider) *SessionManager {
	t.Helper()
	config := managerTestConfig(t.TempDir(), provider)
	config.Services.StateStore = store
	config.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) {
		t.Error("restore invoked new workspace factory")
		return "", errors.New("unexpected factory")
	})
	return makeManager(t, config)
}
func restoreOne(t *testing.T, manager *SessionManager) *ManagedSession {
	t.Helper()
	rows, err := manager.RestoreSessions(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	return rows[0]
}
func equalRestoreMessages(t *testing.T, got, want []protocol.Message) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("messages\ngot %s\nwant %s", a, b)
	}
}
func TestRestoreActualPythonRecipeAndFirstRequest(t *testing.T) {
	for _, recipe := range restoreRecipes(t) {
		t.Run(recipe.Name, func(t *testing.T) {
			store := newRuntimeStateStore()
			row := seedRestore(t, store, recipe.Initial)
			if recipe.Name == "tools" {
				_ = store.WriteApproval(context.Background(), ApprovalRecord{ApprovalID: "pending", SessionID: "saved", ToolUseID: "parked", ToolName: protocol.ToolBash, Status: ApprovalPending, Kind: ApprovalPermission})
			}
			if recipe.Name == "foreign" {
				store.holders[row.SessionID] = "foreign"
			}
			calls := 0
			provider := stateProviderFunc(func(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
				calls++
				if err := req.Validate(); err != nil {
					t.Fatal("restored first request invalid", err)
				}
				stored, _ := store.LoadMessages(context.Background(), row.SessionID, nil)
				if len(stored) < len(recipe.Restored)+1 {
					t.Fatal("first request preceded transcript flush", len(stored))
				}
				if req.System == nil || !strings.Contains(*req.System, *row.System) {
					t.Fatal("lost recorded system")
				}
				return stateFinal(), nil
			})
			manager := restoreManager(t, store, provider)
			session := restoreOne(t, manager)
			info := session.Info()
			status := session.PersistenceStatus()
			if info.ID != row.SessionID || session.Owner() != recipe.Owner || info.Status != recipe.Status || info.Busy != recipe.Busy || info.PermissionMode != recipe.Mode || info.CreatedAt != recipe.CreatedAt || info.RunCount != recipe.RunCount || info.WorkspaceBound != recipe.Bound {
				t.Fatal(info)
			}
			if !reflect.DeepEqual(info.Todos, recipe.Todos) || info.PendingSteering != len(recipe.Steering) {
				t.Fatal("lost live metadata", info)
			}
			if !status.Restored || status.LeaseConfirmed != recipe.Confirmed || status.RestorePending == recipe.Confirmed {
				t.Fatal(status)
			}
			if _, err := os.Stat(row.Workspace); err != nil {
				t.Fatal("workspace not recreated", err)
			}
			if _, err := manager.Get("foreign", row.SessionID); !errors.Is(err, ErrSessionNotFound) {
				t.Fatal("owner lost", err)
			}
			if again, err := manager.RestoreSessions(context.Background()); err != nil || len(again) != 0 {
				t.Fatal("duplicate restore", again, err)
			}
			if session.core.events.next != 9 || recipe.SourceSequence != recipe.PhysicalCursor || recipe.PhysicalCursor != 1 {
				t.Fatal("event sequence regression")
			}
			if recipe.Name == "foreign" {
				// Python repairs before its failed claim; Go exposes facts without writes.
				equalRestoreMessages(t, session.Messages(), recipe.Initial)
				if _, err := session.Steer("cannot acknowledge"); !errors.Is(err, ErrSessionLeaseLost) {
					t.Fatal(err)
				}
				if _, err := session.Run(context.Background(), "continue"); !errors.Is(err, ErrSessionLeaseLost) || calls != 0 {
					t.Fatal(err, calls)
				}
				store.mu.Lock()
				delete(store.holders, row.SessionID)
				store.mu.Unlock()
			} else {
				equalRestoreMessages(t, session.Messages(), recipe.Restored)
				if !reflect.DeepEqual(status.RepairedToolUses, recipe.Repaired) {
					t.Fatal(status, recipe.Repaired)
				}
				saved, _ := store.LoadSessions(context.Background())
				if !reflect.DeepEqual(saved[0].Todos, row.Todos) || !reflect.DeepEqual(saved[0].PendingSteering, row.PendingSteering) {
					t.Fatal("repair erased metadata", saved)
				}
				if recipe.Name == "bare-user" || recipe.Name == "bare-blocks" || recipe.Name == "tools" {
					if len(recipe.SavedTodos) != 0 || len(recipe.SavedSteering) != 0 {
						t.Fatal("source overwrite no longer reproduced")
					}
				}
				if recipe.Name == "tools" {
					approvals, _ := store.ReadApprovals(context.Background(), row.SessionID, nil)
					if len(approvals) != 1 || approvals[0].Status != ApprovalExpired || approvals[0].ResolvedAt == nil {
						t.Fatal(approvals)
					}
				}
				epoch, _ := store.TranscriptEpoch(context.Background(), row.SessionID)
				if epoch != max(1, recipe.Epoch) {
					t.Fatal("repair opened epoch", epoch)
				}
				if err := manager.Stop(context.Background()); err != nil {
					t.Fatal(err)
				}
				second := restoreManager(t, store, provider)
				session = restoreOne(t, second)
				if len(session.Messages()) != recipe.SecondMessages || !reflect.DeepEqual(session.Info().Todos, row.Todos) || session.Info().PendingSteering != 1 {
					t.Fatal("second restore lost durable state", session.Info())
				}
			}
			if _, err := session.Run(context.Background(), "continue"); err != nil || calls != 1 {
				t.Fatal("first resumed request", err, calls)
			}
			store.mu.Lock()
			for _, e := range store.events[row.SessionID][1:] {
				if e.Sequence <= 9 {
					t.Error("reused event sequence", e.Sequence)
				}
			}
			store.mu.Unlock()
		})
	}
}

func TestRestorePendingReloadsLatestUnderClaim(t *testing.T) {
	store := newRuntimeStateStore()
	row := seedRestore(t, store, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("stale")}})
	store.holders[row.SessionID] = "foreign"
	_ = store.WriteApproval(context.Background(), ApprovalRecord{ApprovalID: "pending", SessionID: row.SessionID, ToolUseID: "parked", Status: ApprovalPending})
	calls := 0
	manager := restoreManager(t, store, stateProviderFunc(func(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		if err := req.Validate(); err != nil {
			t.Fatal(err)
		}
		wire, _ := json.Marshal(req.Messages)
		if strings.Contains(string(wire), "stale") || !strings.Contains(string(wire), "fresh") {
			t.Fatal(string(wire))
		}
		return stateFinal(), nil
	}))
	session := restoreOne(t, manager)
	approvals, _ := store.ReadApprovals(context.Background(), row.SessionID, nil)
	if approvals[0].Status != ApprovalPending {
		t.Fatal("expired foreign writer approval")
	}
	store.mu.Lock()
	store.messages[row.SessionID][2] = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("fresh")}}
	latest := store.rows[row.SessionID]
	latest.RunCount = 8
	latest.PendingSteering = []string{"fresh queue"}
	store.rows[row.SessionID] = latest
	delete(store.holders, row.SessionID)
	store.faults["messages"] = errors.New("read failed")
	store.mu.Unlock()
	if _, err := session.Run(context.Background(), "continue"); err == nil || calls != 0 {
		t.Fatal("reload failure admitted", err, calls)
	}
	if _, held, _ := store.LeaseHolder(context.Background(), row.SessionID); held {
		t.Fatal("failed reload retained lease")
	}
	store.mu.Lock()
	delete(store.faults, "messages")
	store.mu.Unlock()
	if _, err := session.Run(context.Background(), "continue"); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	if session.Info().RunCount != 9 || session.PersistenceStatus().RestorePending {
		t.Fatal(session.Info(), session.PersistenceStatus())
	}
}

func TestRestorePendingRejectsChangedIdentity(t *testing.T) {
	for _, field := range []string{"owner", "workspace", "system", "bound", "missing"} {
		t.Run(field, func(t *testing.T) {
			store := newRuntimeStateStore()
			row := seedRestore(t, store, nil)
			store.holders[row.SessionID] = "foreign"
			manager := restoreManager(t, store, stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
				t.Fatal("changed identity reached provider")
				return stateFinal(), nil
			}))
			session := restoreOne(t, manager)
			store.mu.Lock()
			latest := store.rows[row.SessionID]
			switch field {
			case "owner":
				latest.Owner = "other"
			case "workspace":
				latest.Workspace = t.TempDir()
			case "system":
				x := "changed"
				latest.System = &x
			case "bound":
				latest.WorkspaceBound = false
			}
			store.rows[row.SessionID] = latest
			delete(store.holders, row.SessionID)
			if field == "missing" {
				delete(store.rows, row.SessionID)
			}
			store.mu.Unlock()
			_, err := session.Run(context.Background(), "continue")
			if err == nil {
				t.Fatal("changed identity admitted")
			}
			if _, held, _ := store.LeaseHolder(context.Background(), row.SessionID); held {
				t.Fatal("failed identity retained lease")
			}
		})
	}
}

func TestRestoreFaultsNeverPublishOrDeleteHistory(t *testing.T) {
	for _, fault := range []string{"sessions", "messages", "epoch", "events", "cursor", "acquire", "approvals", "approval-write", "append", "upsert", "renew", "renew-reject", "invalid-history", "invalid-event", "foreign-event", "invalid-record"} {
		t.Run(fault, func(t *testing.T) {
			store := newRuntimeStateStore()
			row := seedRestore(t, store, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("crash")}})
			_ = store.WriteApproval(context.Background(), ApprovalRecord{ApprovalID: "pending", SessionID: row.SessionID, Status: ApprovalPending})
			switch fault {
			case "approval-write":
				store.approvalStoreSpy.broken = true
			case "renew-reject":
				store.renewReject = true
			case "invalid-history":
				store.messages[row.SessionID][2] = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("missing", "bad", false))}}
			case "invalid-event":
				store.events[row.SessionID][0].Sequence = 0
			case "foreign-event":
				store.events[row.SessionID][0].SessionID = "foreign"
			case "invalid-record":
				r := store.rows[row.SessionID]
				r.RunCount = -1
				store.rows[row.SessionID] = r
			default:
				store.faults[fault] = errors.New("backend fault")
			}
			manager := restoreManager(t, store, &FakeProvider{})
			sessions, err := manager.RestoreSessions(context.Background())
			if err == nil || len(sessions) != 0 || len(manager.List("tenant")) != 0 {
				t.Fatal("failed restore published", err, sessions)
			}
			store.mu.Lock()
			_, exists := store.rows[row.SessionID]
			events := len(store.events[row.SessionID])
			holder := store.holders[row.SessionID]
			store.mu.Unlock()
			if !exists || events != 1 || holder != "" {
				t.Fatal("failed restore destroyed history or retained lease", exists, events, holder)
			}
		})
	}
}

type blockingRestoreStore struct {
	*runtimeStateStore
	entered, proceed chan struct{}
}

func (s *blockingRestoreStore) LoadSessions(ctx context.Context) ([]SessionRecord, error) {
	close(s.entered)
	<-s.proceed
	return s.runtimeStateStore.LoadSessions(ctx)
}
func TestRestoreStopWaitsForInventoryAndPreventsLatePublication(t *testing.T) {
	backing := newRuntimeStateStore()
	row := seedRestore(t, backing, nil)
	store := &blockingRestoreStore{backing, make(chan struct{}), make(chan struct{})}
	manager := restoreManager(t, store, &FakeProvider{})
	result := make(chan error, 1)
	go func() { _, err := manager.RestoreSessions(context.Background()); result <- err }()
	<-store.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := manager.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stop ignored restore", err)
	}
	close(store.proceed)
	if err := <-result; !errors.Is(err, ErrManagerStopped) {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(manager.List("tenant")) != 0 {
		t.Fatal("late restore published")
	}
	backing.mu.Lock()
	_, exists := backing.rows[row.SessionID]
	backing.mu.Unlock()
	if !exists {
		t.Fatal("stop deleted historical state")
	}
}

type claimAdvancesRestoreStore struct {
	*runtimeStateStore
	advance func()
}

func (s *claimAdvancesRestoreStore) AcquireLease(ctx context.Context, id SessionID, owner LeaseOwner, ttl time.Duration) (bool, error) {
	acquired, err := s.runtimeStateStore.AcquireLease(ctx, id, owner, ttl)
	if acquired && s.advance != nil {
		s.advance()
		s.advance = nil
	}
	return acquired, err
}
func TestRestoreReadsAfterClaimAndRetainsOperatorControl(t *testing.T) {
	backing := newRuntimeStateStore()
	row := seedRestore(t, backing, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("stale")}})
	store := &claimAdvancesRestoreStore{runtimeStateStore: backing, advance: func() {
		backing.mu.Lock()
		defer backing.mu.Unlock()
		backing.messages[row.SessionID][2] = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("fresh at claim")}}
		r := backing.rows[row.SessionID]
		r.RunCount = 7
		backing.rows[row.SessionID] = r
	}}
	manager := restoreManager(t, store, &FakeProvider{})
	session := restoreOne(t, manager)
	data, _ := json.Marshal(session.Messages())
	if strings.Contains(string(data), "stale") || !strings.Contains(string(data), "fresh at claim") || session.Info().RunCount != 7 {
		t.Fatal(string(data), session.Info())
	}
	// The exposed snapshot detaches diagnostics from the private repair list.
	session.core.persistence.mu.Lock()
	session.core.persistence.repaired = []string{"known"}
	session.core.persistence.mu.Unlock()
	status := session.PersistenceStatus()
	status.RepairedToolUses[0] = "changed"
	if session.PersistenceStatus().RepairedToolUses[0] != "known" {
		t.Fatal("aliased persistence status")
	}
}

type approvalBarrierRestoreStore struct {
	*runtimeStateStore
	entered, proceed chan struct{}
}

func (s *approvalBarrierRestoreStore) ReadApprovals(ctx context.Context, id SessionID, status *ApprovalStatus) ([]ApprovalRecord, error) {
	close(s.entered)
	<-s.proceed
	return s.runtimeStateStore.ReadApprovals(ctx, id, status)
}
func TestRestoreStoppedAfterClaimReleasesUnpublishedLease(t *testing.T) {
	backing := newRuntimeStateStore()
	row := seedRestore(t, backing, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("crash")}})
	store := &approvalBarrierRestoreStore{backing, make(chan struct{}), make(chan struct{})}
	manager := restoreManager(t, store, &FakeProvider{})
	result := make(chan error, 1)
	go func() { _, err := manager.RestoreSessions(context.Background()); result <- err }()
	<-store.entered
	if _, held, _ := backing.LeaseHolder(context.Background(), row.SessionID); !held {
		t.Fatal("test never acquired lease")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := manager.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(store.proceed)
	if err := <-result; !errors.Is(err, ErrManagerStopped) {
		t.Fatal(err)
	}
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, held, _ := backing.LeaseHolder(context.Background(), row.SessionID); held {
		t.Fatal("unpublished lease retained")
	}
	if _, err := os.Stat(row.Workspace); err != nil {
		t.Fatal("historical workspace removed", err)
	}
	messages, _ := backing.LoadMessages(context.Background(), row.SessionID, nil)
	if len(messages) != 2 {
		t.Fatal("repair not retained", messages)
	}
}
func TestRestoreReservationsTombstonesAndCancellation(t *testing.T) {
	store := newRuntimeStateStore()
	row := seedRestore(t, store, nil)
	manager := restoreManager(t, store, &FakeProvider{})
	manager.reservations[row.SessionID] = true
	if _, err := manager.RestoreSessions(context.Background()); !errors.Is(err, ErrStateRestoreConflict) {
		t.Fatal(err)
	}
	delete(manager.reservations, row.SessionID)
	session := restoreOne(t, manager)
	store.faults["delete"] = errors.New("delete unavailable")
	if deleted, err := manager.Delete(row.Owner, row.SessionID, DeleteSessionOptions{}); !deleted || err != nil {
		t.Fatal(deleted, err)
	}
	if err := manager.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RestoreSessions(context.Background()); !errors.Is(err, ErrStateRestoreConflict) {
		t.Fatal("deleted row resurrected", err)
	}
	if session.PersistenceStatus().LeaseConfirmed {
		t.Fatal("deleted handle retained lease")
	}
	delete(store.faults, "delete")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.RestoreSessions(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Cleanup errors are expected for the deliberately broken delete above.
	if len(manager.CleanupErrors()) == 0 {
		t.Fatal("delete failure not reported")
	}
}
