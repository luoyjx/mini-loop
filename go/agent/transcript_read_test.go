package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type transcriptReadStore struct {
	*runtimeStateStore
	epochRead   func(context.Context, SessionID) (TranscriptEpoch, error)
	messageRead func(context.Context, SessionID, *TranscriptEpoch) ([]protocol.Message, error)
}

func (s *transcriptReadStore) TranscriptEpoch(ctx context.Context, id SessionID) (TranscriptEpoch, error) {
	if s.epochRead != nil {
		return s.epochRead(ctx, id)
	}
	return s.runtimeStateStore.TranscriptEpoch(ctx, id)
}
func (s *transcriptReadStore) LoadMessages(ctx context.Context, id SessionID, epoch *TranscriptEpoch) ([]protocol.Message, error) {
	if s.messageRead != nil {
		return s.messageRead(ctx, id, epoch)
	}
	return s.runtimeStateStore.LoadMessages(ctx, id, epoch)
}
func transcriptSeeds(t *testing.T) [][]protocol.Message {
	t.Helper()
	var fixture struct {
		Seeds []struct {
			Name   string
			Epochs [][]protocol.Message
		}
	}
	data, err := os.ReadFile("../testdata/python-transcript.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture.Seeds[0].Epochs
}
func TestReadTranscriptActualSourceCanonicalEpochsAndCrashTail(t *testing.T) {
	epochs := transcriptSeeds(t)
	store := newRuntimeStateStore()
	s := stateManaged(t, store, &FakeProvider{}, "")
	if _, err := s.ReadTranscript(context.Background(), TranscriptSelection{}); err == nil {
		t.Fatal("empty transcript should be missing")
	}
	// Drive the actual Go flush/rewrite and microcompact, rather than pre-seeding
	// both epochs and merely checking the reader's own serialization.
	s.core.messages = epochs[0]
	if err := s.core.persistence.guard(s.core.messages); err != nil {
		t.Fatal(err)
	}
	compacted, cleared := MicroCompact(s.core.messages)
	if cleared == 0 {
		t.Fatal("compaction recipe did not rewrite")
	}
	s.core.messages = compacted
	if err := s.core.persistence.guard(s.core.messages); err != nil {
		t.Fatal(err)
	}
	for _, epoch := range []TranscriptEpoch{1, 2} {
		snapshot, err := s.ReadTranscript(context.Background(), SelectTranscriptEpoch(epoch))
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Session != s.ID() || snapshot.Epoch != epoch || snapshot.Epochs != 2 || !reflect.DeepEqual(snapshot.Messages, epochs[epoch-1]) {
			t.Fatalf("epoch %d: %+v", epoch, snapshot)
		}
	}
	latest, err := s.ReadTranscript(context.Background(), TranscriptSelection{})
	if err != nil || latest.Epoch != 2 {
		t.Fatal(latest, err)
	}
	// Later history can have a gap and an unanswered crash-time tool call.
	partial := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("crash", "echo partial"))}
	if _, err := store.AppendMessages(context.Background(), s.ID(), []protocol.Message{partial}, 4); err != nil {
		t.Fatal(err)
	}
	gap, err := s.ReadTranscript(context.Background(), SelectTranscriptEpoch(3))
	if err != nil || gap.Messages == nil || len(gap.Messages) != 0 || gap.Epochs != 4 {
		t.Fatal(gap, err)
	}
	snapshot, err := s.ReadTranscript(context.Background(), TranscriptSelection{})
	if err != nil || len(snapshot.Messages) != 1 || snapshot.Epoch != 4 {
		t.Fatal(snapshot, err)
	}
	if s.Info().RunCount != 0 || len(s.PersistenceStatus().RepairedToolUses) != 0 || store.acquireCalls != 0 {
		t.Fatal("read started/repaired/claimed state")
	}
	value, _ := new(big.Int).SetString("999999999999999999999999999999999999", 10)
	selection := SelectTranscriptEpochNumber(value)
	value.SetInt64(1)
	_, err = s.ReadTranscript(context.Background(), selection)
	var missing *TranscriptEpochNotFound
	if !errors.As(err, &missing) || missing.Requested != "999999999999999999999999999999999999" || missing.Current != 4 {
		t.Fatal(err)
	}
}
func TestReadTranscriptDetachedAndNeverFallsBackToLiveHistory(t *testing.T) {
	store := newRuntimeStateStore()
	s := stateManaged(t, store, &FakeProvider{}, "")
	stored := transcriptSeeds(t)[0]
	if _, err := store.AppendMessages(context.Background(), s.ID(), stored, 1); err != nil {
		t.Fatal(err)
	}
	s.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("LIVE-ONLY-SENTINEL")}}
	one, err := s.ReadTranscript(context.Background(), TranscriptSelection{})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(one)
	if strings.Contains(string(wire), "LIVE-ONLY-SENTINEL") || strings.Contains(string(wire), "0123456789") {
		t.Fatal(string(wire))
	}
	one.Messages[0].Content = protocol.PlainContent("caller change")
	two, err := s.ReadTranscript(context.Background(), TranscriptSelection{})
	if err != nil || !reflect.DeepEqual(two.Messages, stored) {
		t.Fatal(two, err)
	}
	if s.core.messages[0].Content.SameStorage(one.Messages[0].Content) {
		t.Fatal("live alias")
	}
	noStore := stateManaged(t, nil, &FakeProvider{}, "")
	noStore.core.messages = s.core.messages
	_, err = noStore.ReadTranscript(context.Background(), TranscriptSelection{})
	var missing *TranscriptEpochNotFound
	if !errors.As(err, &missing) || missing.Current != 0 || missing.Requested != "0" {
		t.Fatal(err)
	}
	if err := s.core.persistence.delete(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadTranscript(context.Background(), SelectTranscriptEpoch(1)); !errors.As(err, &missing) || missing.Current != 0 {
		t.Fatal(err)
	}
}
func TestReadTranscriptFaultCancellationAndConcurrentCapture(t *testing.T) {
	for _, name := range []string{"epoch-error", "epoch-panic", "negative-epoch", "load-error", "load-panic", "invalid-message"} {
		t.Run(name, func(t *testing.T) {
			backing := newRuntimeStateStore()
			store := &transcriptReadStore{runtimeStateStore: backing}
			s := stateManaged(t, store, &FakeProvider{}, "")
			if _, err := store.AppendMessages(context.Background(), s.ID(), []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("stored")}}, 1); err != nil {
				t.Fatal(err)
			}
			store.epochRead = func(ctx context.Context, id SessionID) (TranscriptEpoch, error) {
				switch name {
				case "epoch-error":
					return 0, errors.New("private epoch canary")
				case "epoch-panic":
					panic("private canary")
				case "negative-epoch":
					return -1, nil
				}
				return backing.TranscriptEpoch(ctx, id)
			}
			store.messageRead = func(ctx context.Context, id SessionID, epoch *TranscriptEpoch) ([]protocol.Message, error) {
				switch name {
				case "load-error":
					return nil, errors.New("private load canary")
				case "load-panic":
					panic("private canary")
				case "invalid-message":
					return []protocol.Message{{Role: "system", Content: protocol.PlainContent("bad role")}}, nil
				}
				return backing.LoadMessages(ctx, id, epoch)
			}
			snapshot, err := s.ReadTranscript(context.Background(), TranscriptSelection{})
			if !errors.Is(err, ErrTranscriptRead) || snapshot.Messages != nil || s.PersistenceStatus().Error != nil {
				t.Fatal(snapshot, err, s.PersistenceStatus())
			}
		})
	}
	backing := newRuntimeStateStore()
	store := &transcriptReadStore{runtimeStateStore: backing}
	s := stateManaged(t, store, &FakeProvider{}, "")
	store.epochRead = func(ctx context.Context, id SessionID) (TranscriptEpoch, error) {
		catchupStatus(s)
		<-ctx.Done()
		return 0, ctx.Err()
	} // Capture must not deadlock on the reader lock.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := s.ReadTranscript(ctx, TranscriptSelection{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	before := len(s.Events())
	_, err = s.ReadTranscript(ctx, TranscriptSelection{})
	if !errors.Is(err, context.DeadlineExceeded) || len(s.Events()) != before {
		t.Fatal(err)
	}
	if s.PersistenceStatus().Error != nil {
		t.Fatal(s.PersistenceStatus())
	}
}

func TestReadTranscriptPendingForeignLeasePreservesUnrepairedHistory(t *testing.T) {
	store := newRuntimeStateStore()
	partial := []protocol.Message{{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("unanswered", "echo prior effect"))}}
	row := seedRestore(t, store, partial)
	store.holders[row.SessionID] = "foreign-process"
	manager := restoreManager(t, store, &FakeProvider{})
	sessions, err := manager.RestoreSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session := sessions[0]
	if !session.PersistenceStatus().RestorePending {
		t.Fatal("fixture did not hold a foreign lease")
	}
	claims := store.acquireCalls
	snapshot, err := session.ReadTranscript(context.Background(), TranscriptSelection{})
	if err != nil || snapshot.Epoch != 2 || !reflect.DeepEqual(snapshot.Messages, partial) {
		t.Fatal(snapshot, err)
	}
	if store.acquireCalls != claims || store.holders[row.SessionID] != "foreign-process" || len(session.PersistenceStatus().RepairedToolUses) != 0 {
		t.Fatal("historical read changed lease or repaired effects")
	}
}
