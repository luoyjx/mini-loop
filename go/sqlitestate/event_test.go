package sqlitestate

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
)

func TestPhysicalEventOrdinalsTailAndUntrustedReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	for index, sequence := range []int{100, 201} {
		record, err := agent.DecodeStoredEvent([]byte(fmt.Sprintf(`{"seq":%d,"ts":10,"session":"s","transcript_epoch":2,"type":"status","status":"running"}`, sequence)))
		if err != nil {
			t.Fatal(err)
		}
		ordinal, err := left.AppendEvent(ctx, "s", record)
		if err != nil || ordinal != agent.EventOrdinal(index+1) {
			t.Fatalf("ordinal %d/%v", ordinal, err)
		}
	}
	for _, recipe := range []struct {
		after agent.EventOrdinal
		limit *int
		count int
	}{
		{0, nil, 2}, {1, nil, 1}, {0, intPointer(0), 0}, {0, intPointer(1), 1}, {0, intPointer(-1), 2},
	} {
		rows, err := right.LoadEvents(ctx, "s", recipe.after, recipe.limit)
		if err != nil || len(rows) != recipe.count {
			t.Fatalf("tail %d/%v", len(rows), err)
		}
		for _, row := range rows {
			if row.Scope.RunContext.Authority() != agent.AuthorityUntrusted {
				t.Fatal("restored live authority")
			}
		}
	}
	if cursor, err := right.EventCursor(ctx, "s"); err != nil || cursor != 2 {
		t.Fatalf("cursor %d/%v", cursor, err)
	}
	if _, err := left.conn.ExecContext(ctx, "CREATE TRIGGER reject_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'reject event'); END"); err != nil {
		t.Fatal(err)
	}
	record, _ := agent.DecodeStoredEvent([]byte(`{"seq":300,"ts":10,"session":"s","transcript_epoch":2,"type":"done","text":"done"}`))
	if ordinal, err := left.AppendEvent(ctx, "s", record); err == nil || ordinal != 0 {
		t.Fatalf("failed event %d/%v", ordinal, err)
	}
	if cursor, err := right.EventCursor(ctx, "s"); err != nil || cursor != 2 {
		t.Fatalf("failed cursor %d/%v", cursor, err)
	}
	if err := left.Close(); err != nil {
		t.Fatal(err)
	}
	if err := right.Close(); err != nil {
		t.Fatal(err)
	}
	left = openStore(t, path)
	rows, err := left.LoadEvents(ctx, "s", 0, nil)
	if err != nil || len(rows) != 2 || rows[1].Sequence != 201 {
		t.Fatalf("reopen %d/%v", len(rows), err)
	}
	if _, err := left.LoadEvents(ctx, "s", agent.EventOrdinal(math.MaxUint64), nil); !errors.Is(err, ErrOrdinalRange) {
		t.Fatal(err)
	}
}

func intPointer(value int) *int { return &value }
