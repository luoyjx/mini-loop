package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLifecycleSharedBindingsCancellationAndOperations(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	alice, _ := Bind(store, "alice")
	bob, _ := Bind(store, "bob")
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- alice.WithLifecycle(ctx, func() error {
			close(entered)
			<-release
			if _, err := alice.Write(ctx, Input{Name: "a", Body: "durable"}); err != nil {
				return err
			}
			if _, err := alice.List(ctx); err != nil {
				return err
			}
			return alice.ReplaceAll(ctx, []Input{{Name: "a", Body: "replacement"}}, Consolidated)
		})
	}()
	<-entered
	waiting, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	called := false
	err = bob.WithLifecycle(waiting, func() error { called = true; return nil })
	close(release)
	if err != context.DeadlineExceeded || called {
		t.Fatalf("waiting callback ran: called=%v err=%v", called, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ordinary scoped operations deadlocked inside lifecycle")
	}
	sentinel := errors.New("callback failure")
	if err := bob.WithLifecycle(ctx, func() error { return sentinel }); err != sentinel {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != sentinel {
				t.Errorf("callback panic changed: %v", recovered)
			}
		}()
		_ = bob.WithLifecycle(ctx, func() error { panic(sentinel) })
	}()
	next, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := alice.WithLifecycle(next, func() error { return nil }); err != nil {
		t.Fatal("callback failure retained lock", err)
	}
	cancelled, stopCancelled := context.WithCancel(ctx)
	stopCancelled()
	if err := alice.WithLifecycle(cancelled, func() error { t.Error("cancelled callback ran"); return nil }); err != context.Canceled {
		t.Fatal(err)
	}
	if err := alice.WithLifecycle(ctx, nil); err == nil {
		t.Fatal("nil callback accepted")
	}
}

func TestLifecycleSeparateStoresDoNotShareLock(t *testing.T) {
	ctx := context.Background()
	first, err := NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(ctx, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Bind(first, "same-owner")
	b, _ := Bind(second, "same-owner")
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := a.WithLifecycle(bounded, func() error {
		return b.WithLifecycle(bounded, func() error { return nil })
	}); err != nil {
		t.Fatal("independent stores blocked each other", err)
	}
}
