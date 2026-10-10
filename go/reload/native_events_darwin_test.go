//go:build darwin && cgo

package reload

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeEventsCloseAndCancellation(t *testing.T) {
	root := t.TempDir()
	observer, err := NewNativeEvents([]string{root}, GoSource)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = observer.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := observer.Next(context.Background()); result <- err }()
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrNativeEventsClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Next did not close")
	}
	nativeRegistry.Lock()
	remaining := len(nativeRegistry.sources)
	nativeRegistry.Unlock()
	if remaining != 0 {
		t.Fatal("native registry leaked", remaining)
	}
	if _, err = NewNativeEvents(nil, GoSource); err == nil {
		t.Fatal("implicit roots accepted")
	}
	if _, err = NewNativeEvents([]string{filepath.Join(root, "missing")}, GoSource); err == nil {
		t.Fatal("missing root accepted")
	}
	if _, err = NewNativeEvents([]string{root}, Language("unknown")); err == nil {
		t.Fatal("unknown language accepted")
	}
}

// Close may arrive as soon as construction publishes readiness, before the
// owner enters CFRunLoopRun. Concurrent callers must join the same cleanup.
func TestNativeEventsImmediateConcurrentClose(t *testing.T) {
	root := t.TempDir()
	for iteration := 0; iteration < 32; iteration++ {
		observer, err := NewNativeEvents([]string{root}, GoSource)
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 4)
		for caller := 0; caller < cap(finished); caller++ {
			go func() { finished <- observer.Close() }()
		}
		deadline := time.NewTimer(time.Second)
		for caller := 0; caller < cap(finished); caller++ {
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-deadline.C:
				t.Fatalf("iteration %d: immediate concurrent Close did not join", iteration)
			}
		}
		deadline.Stop()
	}
	nativeRegistry.Lock()
	remaining := len(nativeRegistry.sources)
	nativeRegistry.Unlock()
	if remaining != 0 {
		t.Fatal("native registry leaked", remaining)
	}
}

func TestDarwinObservationKeepsOnlySubscribedChangePaths(t *testing.T) {
	filter, _ := NewEventFilter(GoSource)
	source := &NativeEvents{eventReader: newEventReader(filter, NativeBackend, nil), roots: []string{"/watched"}}
	// notify 8 filters callback paths by the subscription before translating
	// flags. A lexical sibling must not become an admitted source change.
	source.recordDarwin("/watched-sibling/foreign.go", 0x100)
	source.recordDarwin("/watched/sub/local.go", 0x100)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	batch, err := source.Next(ctx)
	if err != nil || len(batch.Paths) != 1 || batch.Paths[0] != "/watched/sub/local.go" {
		t.Fatal("callback escaped its explicit roots", batch, err)
	}
}

func TestDarwinSentinelsDoNotConsumeNextWait(t *testing.T) {
	filter, _ := NewEventFilter(GoSource)
	source := &NativeEvents{eventReader: newEventReader(filter, NativeBackend, nil), roots: []string{"/watched"}}
	// HistoryDone is ignored before every other bit by notify; item type,
	// OwnEvent and ID-wrap flags alone do not translate to a Source change.
	for _, flags := range []uint32{0x10 | 0x100, 0x10000, 0x20000, 0x40000, 0x80000, 0x8} {
		source.recordDarwin("/watched/sentinel.go", flags)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := source.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("non-change callback consumed the wait", err)
	}
	source.recordDarwin("/watched/removed.go", 0x200)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	batch, err := source.Next(ctx2)
	if err != nil || len(batch.Paths) != 1 || batch.Paths[0] != "/watched/removed.go" {
		t.Fatal("real deletion was lost after ignored callbacks", batch, err)
	}
}

func TestDarwinLossRemainsStickyWithinSubscription(t *testing.T) {
	filter, _ := NewEventFilter(GoSource)
	source := &NativeEvents{eventReader: newEventReader(filter, NativeBackend, nil), roots: []string{"/watched"}}
	source.recordDarwin("/unrelated/file.go", 7)
	source.recordDarwin("/watched/file.go", 0x100)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := source.Next(ctx); err != nil {
		t.Fatal("unrelated callback poisoned subscription", err)
	}
	source.recordDarwin("/watched", 7)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if _, err := source.Next(ctx2); err == nil {
		t.Fatal("subscribed event loss silently accepted")
	}
}
