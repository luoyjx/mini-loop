package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sessionquery"
)

func requestArchive[T any](t *testing.T, input T) sessionquery.ArchiveValue {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func reconstructStarts(t *testing.T, session *ManagedSession, store *runtimeStateStore) []sessionquery.Reconstruction {
	t.Helper()
	events, err := store.LoadEvents(context.Background(), session.ID(), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	var results []sessionquery.Reconstruction
	for _, event := range events {
		if event.Event.Kind() == EventModelStart {
			sequence, err := sessionquery.ParseRequestSequence(strconv.FormatUint(uint64(event.Sequence), 10))
			if err != nil {
				t.Fatal(err)
			}
			result, err := session.ReconstructRequest(context.Background(), sequence)
			if err != nil || result.Kind() != sessionquery.ReconstructionFound {
				t.Fatal(result, err)
			}
			results = append(results, result)
		}
	}
	return results
}

func TestReconstructRequestNativeProviderBoundaryAndOldEpoch(t *testing.T) {
	store := newRuntimeStateStore()
	var requests []protocol.ModelRequest
	var session *ManagedSession
	provider := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		requests = append(requests, request.Clone())
		// A real in-flight provider invocation must be able to inspect the
		// already-flushed request without acquiring the core's turn lock.
		if results := reconstructStarts(t, session, store); len(results) != len(requests) {
			t.Fatal("in-flight request was unavailable", len(results), len(requests))
		}
		if len(requests) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUseWithoutCaller("bash", protocol.BashToolInput(protocol.BashInput{Command: "echo historical"}))}, protocol.StopToolUse), nil
		}
		return stateFinal(), nil
	})
	session = stateManaged(t, store, provider, "")
	if _, err := session.Run(context.Background(), "original"); err != nil {
		t.Fatal(err)
	}
	before := reconstructStarts(t, session, store)
	if len(before) != 2 || len(requests) != 2 {
		t.Fatal(len(before), len(requests))
	}
	for index, result := range before {
		projection, _ := result.Request()
		request := requests[index]
		wire, err := request.Wire()
		if err != nil {
			t.Fatal(err)
		}
		if !requestArchive(t, projection.Messages).PythonEqual(requestArchive(t, request.Messages)) ||
			!projection.System.PythonEqual(requestArchive(t, wire.System)) ||
			!projection.Tools.PythonEqual(requestArchive(t, request.Tools)) ||
			!projection.Model.PythonEqual(requestArchive(t, request.Model)) ||
			!projection.MaxTokens.PythonEqual(requestArchive(t, request.MaxTokens)) {
			t.Fatalf("request %d does not match the actual provider boundary", index)
		}
		if projection.Capability == nil || !projection.Capability.PermissionMode.PythonEqual(requestArchive(t, ModeAuto)) {
			t.Fatal("missing historical capability", projection.Capability)
		}
	}
	// The sanctioned idle transcript rewrite advances the persisted epoch on the
	// next request guard. Reconstruction must continue selecting the old epoch.
	session.core.mu.Lock()
	session.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("summary")}}
	session.core.publishLive()
	session.core.mu.Unlock()
	if _, err := session.Run(context.Background(), "later"); err != nil {
		t.Fatal(err)
	}
	if epoch, err := store.TranscriptEpoch(context.Background(), session.ID()); err != nil || epoch != 2 {
		t.Fatal(epoch, err)
	}
	after := reconstructStarts(t, session, store)
	for index := range before {
		if !requestArchive(t, before[index]).PythonEqual(requestArchive(t, after[index])) {
			t.Fatal("old request changed after rewrite", index)
		}
	}
}

func TestReconstructRequestMaskedStoredProjectionAndMissingStore(t *testing.T) {
	store := newRuntimeStateStore()
	session, err := NewManagedSession(RuntimeConfig{ID: "masked", Owner: "tenant", Workspace: t.TempDir(), Provider: &FakeProvider{}, Bash: echoExecutor{}, Mode: ModeAuto, MaxRounds: 2, StateStore: store, Secrets: runtimeSecrets()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), runtimeCanary); err != nil {
		t.Fatal(err)
	}
	results := reconstructStarts(t, session, store)
	if len(results) == 0 {
		t.Fatal("missing native request")
	}
	data, err := json.Marshal(results[0])
	if err != nil || strings.Contains(string(data), "0123456789") {
		t.Fatal("reconstruction leaked live input", err)
	}
	empty := stateManaged(t, nil, &FakeProvider{}, "")
	session.core.persistence.mu.Lock()
	session.core.persistence.disabled = true
	session.core.persistence.mu.Unlock()
	for _, target := range []*ManagedSession{empty, session} {
		result, err := target.ReconstructRequest(context.Background(), sessionquery.NewRequestSequence(1))
		if err != nil || result.Kind() != sessionquery.ReconstructionMissingStart {
			t.Fatal(result, err)
		}
	}
}

type reconstructionReadStore struct {
	*transcriptReadStore
	eventRead func(context.Context, SessionID, EventOrdinal, *int) ([]SessionEventRecord, error)
}

func (store *reconstructionReadStore) LoadEvents(ctx context.Context, id SessionID, after EventOrdinal, limit *int) ([]SessionEventRecord, error) {
	if store.eventRead != nil {
		return store.eventRead(ctx, id, after, limit)
	}
	return store.runtimeStateStore.LoadEvents(ctx, id, after, limit)
}

func TestReconstructRequestReadFaultsCancellationAndConcurrentCapture(t *testing.T) {
	backing := newRuntimeStateStore()
	store := &reconstructionReadStore{transcriptReadStore: &transcriptReadStore{runtimeStateStore: backing}}
	session := stateManaged(t, store, &FakeProvider{}, "")
	if _, err := session.Run(context.Background(), "request"); err != nil {
		t.Fatal(err)
	}
	events, _ := backing.LoadEvents(context.Background(), session.ID(), 0, nil)
	var sequence sessionquery.RequestSequence
	for _, event := range events {
		if event.Event.Kind() == EventModelStart {
			sequence, _ = sessionquery.ParseRequestSequence(strconv.FormatUint(uint64(event.Sequence), 10))
			break
		}
	}
	for _, phase := range []string{"events", "messages"} {
		for _, failure := range []string{"error", "panic", "cancel"} {
			t.Run(phase+"-"+failure, func(t *testing.T) {
				store.eventRead = nil
				store.messageRead = nil
				marker := errors.New("backing failure")
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				fail := func(ctx context.Context) error {
					catchupStatus(session) // Reading must release persistence locks.
					switch failure {
					case "panic":
						panic("private panic payload")
					case "cancel":
						<-ctx.Done()
						return ctx.Err()
					default:
						return marker
					}
				}
				if phase == "events" {
					store.eventRead = func(ctx context.Context, id SessionID, after EventOrdinal, limit *int) ([]SessionEventRecord, error) {
						if id != session.ID() || after != 0 || limit != nil {
							t.Fatal("incorrect event selector")
						}
						return nil, fail(ctx)
					}
				} else {
					store.messageRead = func(ctx context.Context, id SessionID, epoch *TranscriptEpoch) ([]protocol.Message, error) {
						if id != session.ID() || epoch == nil || *epoch != 1 {
							t.Fatal("incorrect historical selector")
						}
						return nil, fail(ctx)
					}
				}
				result, err := session.ReconstructRequest(ctx, sequence)
				if !errors.Is(err, ErrRequestReconstruction) || result.Kind() != sessionquery.ReconstructionInvalid || session.PersistenceStatus().Error != nil {
					t.Fatal(result, err, session.PersistenceStatus())
				}
				if failure == "error" && !errors.Is(err, marker) || failure == "cancel" && !errors.Is(err, context.DeadlineExceeded) || failure == "panic" && strings.Contains(err.Error(), "private panic payload") {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestReconstructRequestPendingForeignLeaseDoesNotRepairOrClaim(t *testing.T) {
	store := newRuntimeStateStore()
	partial := []protocol.Message{{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("unanswered", "echo prior effect"))}}
	row := seedRestore(t, store, partial)
	store.holders[row.SessionID] = "foreign-process"
	_, err := store.AppendEvent(context.Background(), row.SessionID, SessionEventRecord{
		Sequence: 7, SessionID: row.SessionID, TranscriptEpoch: 2,
		Event: SessionEvent{kind: EventModelStart, modelStart: ModelStartEvent{Model: "historical", MessageCount: 1, MaxTokens: 8}},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := restoreManager(t, store, &FakeProvider{})
	sessions, err := manager.RestoreSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := sessions[0]
	claims := store.acquireCalls
	result, err := session.ReconstructRequest(context.Background(), sessionquery.NewRequestSequence(7))
	if err != nil {
		t.Fatal(err)
	}
	projection, found := result.Request()
	if !found || !requestArchive(t, projection.Messages).PythonEqual(requestArchive(t, partial)) {
		t.Fatal("historical crash tail was repaired or replaced")
	}
	if !session.PersistenceStatus().RestorePending || store.acquireCalls != claims || store.holders[row.SessionID] != "foreign-process" || len(session.PersistenceStatus().RepairedToolUses) != 0 {
		t.Fatal("read changed lease or restore state")
	}
}
