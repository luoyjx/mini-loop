package reload

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNativeBatchCancellationRetainsEvents(t *testing.T) {
	filter, _ := NewEventFilter(GoSource)
	source := &NativeEvents{pending: make(map[string]struct{}), filter: filter, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	source.record("/root/a.go", 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := source.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	batch, err := source.Next(ctx2)
	if err != nil || len(batch.Paths) != 1 || batch.Paths[0] != "/root/a.go" {
		t.Fatal(batch, err)
	}
	ctx3, cancel3 := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel3()
	if _, err = source.Next(ctx3); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stale batch", err)
	}
	source.record("/root/ignored.txt", 7)
	if _, err = source.Next(context.Background()); err == nil {
		t.Fatal("kernel drop silently accepted")
	}
}
