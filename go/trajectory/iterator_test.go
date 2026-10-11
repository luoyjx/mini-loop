package trajectory

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/agent"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestActualPythonFilteredRecordIterator(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trace-view.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Iterator struct {
			ID    agent.TrajectoryID
			Raw   string
			Cases []struct {
				Types   []agent.SessionEventKind
				Limit   int
				Records []json.RawMessage
			}
		}
	}
	if json.Unmarshal(data, &fixture) != nil {
		t.Fatal("bad fixture")
	}
	store, err := New(Config{Root: t.TempDir(), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root(), string(fixture.Iterator.ID)+".jsonl")
	os.WriteFile(path, []byte(fixture.Iterator.Raw), 0600)
	for _, row := range fixture.Iterator.Cases {
		records := make([]json.RawMessage, 0)
		err := store.VisitRecords(context.Background(), fixture.Iterator.ID, agent.TrajectoryEventQuery{Types: row.Types, Limit: row.Limit}, func(raw []byte) error { records = append(records, raw); return nil })
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := json.Marshal(records)
		source, _ := json.Marshal(row.Records)
		equivalentJSON(t, actual, source)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.VisitRecords(ctx, fixture.Iterator.ID, agent.TrajectoryEventQuery{Limit: 10}, func([]byte) error { t.Fatal("cancelled iterator called visitor"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	sentinel := errors.New("visitor stopped")
	if err := store.VisitRecords(context.Background(), fixture.Iterator.ID, agent.TrajectoryEventQuery{Limit: 10}, func([]byte) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := store.VisitRecords(context.Background(), "traj_bbbbbbbbbbbbbbbbbbbbbbbb", agent.TrajectoryEventQuery{Limit: 10}, func([]byte) error { t.Fatal("missing file yielded"); return nil }); err != nil {
		t.Fatal("source missing file is an empty iterator", err)
	}
}

func TestVisitorDoesNotHoldAppendLock(t *testing.T) {
	store, err := New(Config{Root: t.TempDir(), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Start(agent.TrajectoryStart{Session: "s", Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- store.VisitRecords(context.Background(), id, agent.TrajectoryEventQuery{Limit: 1}, func([]byte) error { return store.Finish(id, agent.TrajectoryFinish{Status: agent.TrajectoryCompleted}) })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("visitor held append lock")
	}
}
