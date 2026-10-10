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
