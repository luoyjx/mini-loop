package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTurnOutcomeCommitsAsOne breaks the last write of a turn's end, the
// trigger's status, the way a crash between the writes would. Before Settle
// the reply was already committed, so the restart ran the turn again and the
// user got the reply twice. Now nothing of the outcome is written, the batch
// is retried, and once the fault is gone the user sees the reply once.
func TestTurnOutcomeCommitsAsOne(t *testing.T) {
	ctx := context.Background()
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	if _, err := app.Store.db.ExecContext(ctx, `CREATE TRIGGER inject_fault BEFORE UPDATE OF status ON triggers
		WHEN NEW.status='done' BEGIN SELECT RAISE(ABORT, 'injected fault'); END`); err != nil {
		t.Fatal(err)
	}
	chat, err := app.Store.ChatFromUser(ctx, "今天日程", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()

	retried := waitTrigger(t, app.Store, chat, func(trigger Trigger) bool {
		return trigger.Status == TriggerPending && trigger.Attempts == 1
	})
	if !strings.Contains(retried.Decision, "injected fault") {
		t.Fatalf("retry decision %q", retried.Decision)
	}
	if replies := repliesTo(t, app.Store, chat); replies != 0 {
		t.Fatalf("a failed settlement posted %d replies", replies)
	}
	entries, err := app.Store.Ledger(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.TriggerID == chat && (entry.Kind == LedgerChannel || entry.Kind == LedgerReport) {
			t.Fatalf("a failed settlement kept a ledger row: %+v", entry)
		}
	}

	if _, err := app.Store.db.ExecContext(ctx, `DROP TRIGGER inject_fault`); err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(time.Minute))
	app.Router.Wake()
	done := triggerByID(t, settle(t, app.Store), chat)
	if done.Status != TriggerDone || done.Attempts != 2 {
		t.Fatalf("after the fault: %+v", done)
	}
	if replies := repliesTo(t, app.Store, chat); replies != 1 {
		t.Fatalf("%d replies, want 1", replies)
	}

	// A batch that is no longer running is not settled again.
	_, err = app.Store.Settle(ctx, Settlement{Batch: []Trigger{done}, Status: TriggerDone, Decision: "again",
		Messages: []ChannelMessage{{Role: RoleAssistant, Text: "again", TriggerID: chat}}}, clock.Now())
	if !errors.Is(err, errSettled) {
		t.Fatalf("settling a done trigger: %v", err)
	}
	if replies := repliesTo(t, app.Store, chat); replies != 1 {
		t.Fatalf("a refused settlement posted: %d replies", replies)
	}
}

func waitTrigger(t *testing.T, store *Store, id int64, ok func(Trigger) bool) Trigger {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		triggers, err := store.Triggers(context.Background(), 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, trigger := range triggers {
			if trigger.ID == id && ok(trigger) {
				return trigger
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("trigger %d did not reach the expected state", id)
	return Trigger{}
}

func repliesTo(t *testing.T, store *Store, trigger int64) int {
	t.Helper()
	lines, err := store.Channel(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range lines {
		if line.Role == RoleAssistant && line.TriggerID == trigger {
			count++
		}
	}
	return count
}
