package sqlitestate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
)

func TestLeaseStepsAndUpsertRetentionMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-state-store.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Leases struct {
			Steps []struct {
				Name      string
				Operation string
				Owner     agent.LeaseOwner
				Now       float64
				TTL       float64
				Result    *bool
				Holder    *agent.LeaseOwner
			}
			HolderAfterUpsert agent.LeaseOwner `json:"holder_after_upsert"`
			MissingClaim      bool             `json:"missing_claim"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Leases.Steps) != 13 {
		t.Fatalf("incomplete Source lease steps: %d", len(fixture.Leases.Steps))
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	record := sourceSessionFixture(t).Input
	record.SessionID = "s"
	if err := left.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	var now float64
	left.now = func() float64 { return now }
	right.now = left.now
	for _, step := range fixture.Leases.Steps {
		now = step.Now
		target := left
		if step.Owner == "process-b" {
			target = right
		}
		var got bool
		var err error
		switch step.Operation {
		case "acquire":
			got, err = target.AcquireLease(ctx, "s", step.Owner, time.Duration(step.TTL*float64(time.Second)))
		case "renew":
			got, err = target.RenewLease(ctx, "s", step.Owner, time.Duration(step.TTL*float64(time.Second)))
		case "release":
			if step.Name == "holder-release" {
				record.CreatedAt = 999
				if err := left.UpsertSession(ctx, record); err != nil {
					t.Fatal(err)
				}
				holder, held, err := right.LeaseHolder(ctx, "s")
				if err != nil || !held || holder != fixture.Leases.HolderAfterUpsert {
					t.Fatalf("upsert changed lease: %q/%v/%v", holder, held, err)
				}
			}
			err = target.ReleaseLease(ctx, "s", step.Owner)
		default:
			t.Fatal(step.Operation)
		}
		if err != nil {
			t.Fatalf("%s: %v", step.Name, err)
		}
		if step.Result != nil && got != *step.Result {
			t.Fatalf("%s: claim result %v, want %v", step.Name, got, *step.Result)
		}
		holder, held, err := left.LeaseHolder(ctx, "s")
		if err != nil {
			t.Fatal(err)
		}
		if step.Holder == nil {
			if held {
				t.Fatalf("%s: unexpectedly held by %q", step.Name, holder)
			}
		} else if !held || holder != *step.Holder {
			t.Fatalf("%s: holder %q/%v, want %q", step.Name, holder, held, *step.Holder)
		}
	}
	got, err := left.AcquireLease(ctx, "missing", "process-a", 10*time.Second)
	if err != nil || got != fixture.Leases.MissingClaim {
		t.Fatalf("missing claim: %v/%v", got, err)
	}
	if err := left.Close(); err != nil {
		t.Fatal(err)
	}
	if err := right.Close(); err != nil {
		t.Fatal(err)
	}
	left = openStore(t, path)
	left.now = func() float64 { return 141 }
	holder, held, err := left.LeaseHolder(ctx, "s")
	if err != nil || !held || holder != "process-a" {
		t.Fatalf("lease lost on reopen: %q/%v/%v", holder, held, err)
	}
}

func TestConcurrentConnectionsCannotBothClaim(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	left, right := openStore(t, path), openStore(t, path)
	left.now = func() float64 { return 100 }
	right.now = left.now
	record := sourceSessionFixture(t).Input
	if err := left.UpsertSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		start := make(chan struct{})
		type outcome struct {
			owner    agent.LeaseOwner
			acquired bool
			err      error
		}
		results := make(chan outcome, 2)
		var wait sync.WaitGroup
		for index, store := range []*Store{left, right} {
			owner := agent.LeaseOwner([]string{"process-a", "process-b"}[index])
			wait.Add(1)
			go func(store *Store, owner agent.LeaseOwner) {
				defer wait.Done()
				<-start
				acquired, err := store.AcquireLease(ctx, record.SessionID, owner, time.Minute)
				results <- outcome{owner, acquired, err}
			}(store, owner)
		}
		close(start)
		wait.Wait()
		close(results)
		winners := 0
		var winner agent.LeaseOwner
		for result := range results {
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.acquired {
				winners++
				winner = result.owner
			}
		}
		if winners != 1 {
			t.Fatalf("concurrent claim had %d winners", winners)
		}
		holder, held, err := right.LeaseHolder(ctx, record.SessionID)
		if err != nil || !held || holder != winner {
			t.Fatalf("winner mismatch: %q/%v/%v", holder, held, err)
		}
		if err := left.ReleaseLease(ctx, record.SessionID, winner); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLeaseClosedAndCancelled(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "state.db"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.AcquireLease(ctx, "s", "owner", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	ctx = context.Background()
	if _, err := store.AcquireLease(ctx, "s", "owner", time.Second); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, err := store.RenewLease(ctx, "s", "owner", time.Second); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if err := store.ReleaseLease(ctx, "s", "owner"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, _, err := store.LeaseHolder(ctx, "s"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}
