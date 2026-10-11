package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/luoyjx/mini-loop/go/memory"
)

func TestMemoryRecordsRetainsOwnerAndDetachedMutableView(t *testing.T) {
	ctx := context.Background()
	manager := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
	bob := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
	if _, err := alice.core.memory.Write(ctx, memory.Input{Name: "private", Body: "first"}); err != nil {
		t.Fatal(err)
	}
	rows, err := alice.MemoryRecords(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	rows[0].Body = "caller mutation"
	if _, err := alice.core.memory.Write(ctx, memory.Input{Name: "private", Body: "latest"}); err != nil {
		t.Fatal(err)
	}
	rows, err = alice.MemoryRecords(ctx)
	if err != nil || len(rows) != 1 || rows[0].Body != "latest" {
		t.Fatal(rows, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := alice.MemoryRecords(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// A corrupt foreign binding must be refused before returning its records.
	bob.core.memory = alice.core.memory
	if rows, err := bob.MemoryRecords(ctx); err == nil || rows != nil {
		t.Fatal(rows, err)
	}
	bob.core.memory = nil
	if rows, err := bob.MemoryRecords(ctx); err == nil || rows != nil {
		t.Fatal(rows, err)
	}
}
