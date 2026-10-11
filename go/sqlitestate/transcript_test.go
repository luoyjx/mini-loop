package sqlitestate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestTranscriptEpochsRollbackAndReopenMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-state-store.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Messages struct {
			AppendCounts []int                 `json:"append_counts"`
			Original     []protocol.Message    `json:"original"`
			Current      []protocol.Message    `json:"current"`
			Old          []protocol.Message    `json:"old"`
			Absent       []protocol.Message    `json:"absent"`
			Epoch        agent.TranscriptEpoch `json:"epoch"`
			Rollback     struct {
				Failed  bool
				Count   int
				Current []protocol.Message
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	recipes := []struct {
		store *Store
		rows  []protocol.Message
		epoch agent.TranscriptEpoch
	}{
		{left, fixture.Messages.Original[:2], 1}, {right, fixture.Messages.Original[2:], 1},
		{left, fixture.Messages.Current, 2}, {left, nil, 1}, {left, nil, 3},
	}
	for index, recipe := range recipes {
		count, err := recipe.store.AppendMessages(ctx, "s", recipe.rows, recipe.epoch)
		if err != nil || count != fixture.Messages.AppendCounts[index] {
			t.Fatalf("append %d: %d/%v", index, count, err)
		}
	}
	for _, recipe := range []struct {
		epoch    *agent.TranscriptEpoch
		expected []protocol.Message
	}{
		{nil, fixture.Messages.Current}, {epochPointer(1), fixture.Messages.Old}, {epochPointer(3), fixture.Messages.Absent},
	} {
		got, err := right.LoadMessages(ctx, "s", recipe.epoch)
		if err != nil || !reflect.DeepEqual(got, recipe.expected) {
			t.Fatalf("load: %#v / %v", got, err)
		}
	}
	if _, err := left.conn.ExecContext(ctx, "CREATE TRIGGER reject_row BEFORE INSERT ON messages WHEN instr(NEW.payload,'reject')>0 BEGIN SELECT RAISE(ABORT,'rejected probe row'); END"); err != nil {
		t.Fatal(err)
	}
	_, err = left.AppendMessages(ctx, "s", []protocol.Message{
		{Role: protocol.RoleUser, Content: protocol.PlainContent("first")},
		{Role: protocol.RoleAssistant, Content: protocol.PlainContent("reject")},
	}, 2)
	if (err != nil) != fixture.Messages.Rollback.Failed {
		t.Fatal(err)
	}
	if count, err := right.MessageCount(ctx, "s", nil); err != nil || count != fixture.Messages.Rollback.Count {
		t.Fatalf("rollback count: %d/%v", count, err)
	}
	if err := left.Close(); err != nil {
		t.Fatal(err)
	}
	if err := right.Close(); err != nil {
		t.Fatal(err)
	}
	left = openStore(t, path)
	if epoch, err := left.TranscriptEpoch(ctx, "s"); err != nil || epoch != fixture.Messages.Epoch {
		t.Fatalf("epoch: %d/%v", epoch, err)
	}
	got, err := left.LoadMessages(ctx, "s", nil)
	if err != nil || !reflect.DeepEqual(got, fixture.Messages.Rollback.Current) {
		t.Fatalf("reopen: %#v/%v", got, err)
	}
}

func epochPointer(value agent.TranscriptEpoch) *agent.TranscriptEpoch { return &value }

func TestLargeStoredMessagesDoNotWidenNetworkFrames(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	blocks := make([]protocol.Block, 1501)
	for index := range blocks {
		blocks[index] = protocol.NewTextBlock(fmt.Sprintf("汉字 %d", index))
	}
	rows := []protocol.Message{
		{Role: protocol.RoleUser, Content: protocol.PlainContent(strings.Repeat("汉字😀", protocol.MaxWireBytes/4))},
		{Role: protocol.RoleAssistant, Content: protocol.BlockContent(blocks...)},
		{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("orphan", strings.Repeat("output", protocol.MaxWireBytes), false))},
	}
	if _, err := store.AppendMessages(ctx, "s", rows, 1); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadMessages(ctx, "s", nil)
	if err != nil || !reflect.DeepEqual(got, rows) {
		t.Fatalf("large archival round trip: %v", err)
	}
	data, err := rows[0].MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var network protocol.Message
	if err := json.Unmarshal(data, &network); err == nil {
		t.Fatal("archival decoding widened network ingress")
	}
	// Source explicitly treats epoch zero as empty even when rows were inserted.
	if count, err := store.AppendMessages(ctx, "zero", rows[:1], 0); err != nil || count != 0 {
		t.Fatalf("epoch zero count: %d/%v", count, err)
	}
	if got, err := store.LoadMessages(ctx, "zero", epochPointer(0)); err != nil || len(got) != 0 {
		t.Fatalf("epoch zero load: %d/%v", len(got), err)
	}
}

func TestConcurrentTranscriptAppendsAreContiguous(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	stores := []*Store{openStore(t, path), openStore(t, path)}
	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for index, store := range stores {
		wait.Add(1)
		go func(index int, store *Store) {
			defer wait.Done()
			for item := 0; item < 12; item++ {
				_, err := store.AppendMessages(ctx, "s", []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(fmt.Sprintf("%d-%d", index, item))}}, 1)
				if err != nil {
					errors <- err
					return
				}
			}
		}(index, store)
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	ordinals := stringRows(t, rawDatabase(t, path), "SELECT ordinal FROM messages ORDER BY ordinal")
	if len(ordinals) != 24 {
		t.Fatal(ordinals)
	}
	for index, value := range ordinals {
		if value != fmt.Sprint(index+1) {
			t.Fatal(ordinals)
		}
	}
}
