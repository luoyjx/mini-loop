package reload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// PollingEvents owns the explicit WatchFiles PollWatcher profile. It detects
// additions/deletions and increasing whole-second mtime; it compares no content.
// Directory/file links retain lexical names. Native transient-event fidelity
// does not follow from polling; a path absent at both scans is unobservable.
type PollingEvents struct{ *eventReader }

type pollMetadata struct {
	modifiedSeconds int64
	missing         bool
}

func newPollingEvents(ctx context.Context, roots []string, language Language, delay uint64, diagnostic io.Writer, excludedWorkspace string) (*PollingEvents, error) {
	filter, err := NewEventFilter(language)
	if err != nil {
		return nil, err
	}
	owned, err := captureEventRoots(roots)
	if err != nil {
		return nil, err
	}
	previous, err := pollSnapshotOwned(ctx, owned, nil, excludedWorkspace)
	if err != nil {
		return nil, err
	}
	source := &PollingEvents{eventReader: newEventReader(filter, PollingBackend, diagnostic)}
	source.excludedWorkspace = excludedWorkspace
	go func() {
		defer close(source.done)
		ctx := context.Background()
		// The scanning worker observes stop directly between filesystem operations;
		// no extra cancellation goroutine is left behind.
		for waitPollDelay(source.stop, delay) {
			current, err := pollSnapshotOwned(ctx, owned, source.stop, excludedWorkspace)
			if err != nil {
				if !errors.Is(err, ErrEventsClosed) {
					source.fail(err)
				}
				return
			}
			for path, metadata := range current {
				old, exists := previous[path]
				switch {
				case metadata.missing:
					source.recordPolling(path, deletedObservation)
				case !exists || old.missing:
					source.recordPolling(path, addedObservation)
				case metadata.modifiedSeconds > old.modifiedSeconds:
					source.recordPolling(path, modifiedObservation)
				}
			}
			for path := range previous {
				if _, exists := current[path]; !exists {
					source.recordPolling(path, deletedObservation)
				}
			}
			previous = current
		}
	}()
	return source, nil
}

// uint64 milliseconds is the Source constructor range. Partition long waits
// rather than overflowing Go nanoseconds; Close interrupts every partition.
func waitPollDelay(stop <-chan struct{}, milliseconds uint64) bool {
	const maxMilliseconds = uint64((1<<63 - 1) / int64(time.Millisecond))
	for {
		part := milliseconds
		if part > maxMilliseconds {
			part = maxMilliseconds
		}
		timer := time.NewTimer(time.Duration(part) * time.Millisecond)
		select {
		case <-stop:
			timer.Stop()
			return false
		case <-timer.C:
		}
		if milliseconds <= part {
			return true
		}
		milliseconds -= part
	}
}

func pollSnapshot(ctx context.Context, roots []string) (map[string]pollMetadata, error) {
	return pollSnapshotWithStop(ctx, roots, nil)
}
func pollSnapshotWithStop(ctx context.Context, roots []string, stop <-chan struct{}) (map[string]pollMetadata, error) {
	return pollSnapshotOwned(ctx, roots, stop, "")
}
func pollSnapshotOwned(ctx context.Context, roots []string, stop <-chan struct{}, excludedWorkspace string) (map[string]pollMetadata, error) {
	snapshot := make(map[string]pollMetadata)
	var walk func(string, map[string]bool) error
	walk = func(path string, ancestors map[string]bool) error {
		if insideBuildWorkspace(excludedWorkspace, path) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-stop:
			return ErrEventsClosed
		default:
		}
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			// Source keeps registered roots and dangling directory entries as
			// missing paths, notifying deletion again on each polling pass.
			_, linkErr := os.Lstat(path)
			registered := false
			for _, root := range roots {
				if path == root {
					registered = true
					break
				}
			}
			if linkErr == nil || registered {
				snapshot[path] = pollMetadata{missing: true}
			}
			return nil
		}
		if os.IsPermission(err) {
			return nil
		}
		if err != nil {
			return err
		}
		snapshot[path] = pollMetadata{modifiedSeconds: info.ModTime().Unix()}
		if !info.IsDir() {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if ancestors[target] {
			return fmt.Errorf("polling directory symlink cycle: %s", path)
		}
		if insideBuildWorkspace(excludedWorkspace, target) {
			delete(snapshot, path)
			return nil
		}
		ancestors[target] = true
		defer delete(ancestors, target)
		entries, err := os.ReadDir(path)
		if os.IsPermission(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := walk(filepath.Join(path, entry.Name()), ancestors); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := walk(root, make(map[string]bool)); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}
