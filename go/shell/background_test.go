package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBackgroundIsExcludedFromForegroundInterrupt(t *testing.T) {
	executor := makeExecutor(t, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan ProcessID, 1)
	done := make(chan error, 1)
	go func() {
		_, err := executor.ExecuteBackground(ctx, BackgroundCommand{Command: "sleep 30", Started: func(pid ProcessID) { started <- pid }})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("background never started")
	}
	if killed := executor.Interrupt(); killed != 0 {
		t.Fatal("foreground interrupt reached background work", killed)
	}
	select {
	case err := <-done:
		t.Fatal("background ended with foreground interrupt", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal("background lost caller cancellation", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("background cancellation did not join")
	}
}

func TestBackgroundStartObserverPanicEndsAndJoinsGroup(t *testing.T) {
	executor := makeExecutor(t, Config{})
	result, err := executor.ExecuteBackground(context.Background(), BackgroundCommand{Command: "sleep 30", Started: func(ProcessID) { panic("private-value") }})
	if err != nil || result.Error == nil || !strings.Contains(*result.Error, "start observer panicked") || strings.Contains(*result.Error, "private-value") || result.ExitCode == nil {
		t.Fatal("start observer panic did not yield reaped, bounded failure", result, err)
	}
}
