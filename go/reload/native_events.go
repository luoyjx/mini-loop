package reload

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// EventBatch distinguishes a native batch containing only excluded names from
// no batch. Paths are sorted, unique, lexical kernel paths, never stat-resolved.
type EventBatch struct{ Paths []string }

// NativeEvents owns one OS subscription. Callers must Close it, including after
// a cancelled Next. Only one Next may be active; Close joins native cleanup.
// This explicit SDK does not select environment overrides or activate the CLI.
type NativeEvents struct {
	mu        sync.Mutex
	nextMu    sync.Mutex
	pending   map[string]struct{}
	seen      bool
	fault     error
	filter    EventFilter
	wake      chan struct{}
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

var ErrNativeEventsClosed = errors.New("native event subscription closed")

func NewNativeEvents(roots []string, language Language) (*NativeEvents, error) {
	filter, err := NewEventFilter(language)
	if err != nil {
		return nil, err
	}
	if len(roots) == 0 {
		return nil, errors.New("native events require explicit roots")
	}
	owned := make([]string, 0, len(roots))
	for _, root := range roots {
		path, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("native event root is not a directory: %s", path)
		}
		owned = append(owned, path)
	}
	source := &NativeEvents{pending: make(map[string]struct{}), filter: filter, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	if err := source.start(owned); err != nil {
		return nil, err
	}
	return source, nil
}

func (source *NativeEvents) record(path string, flags uint32) {
	source.mu.Lock()
	source.seen = true
	if flags&7 != 0 {
		source.fault = errors.New("native filesystem events dropped; rescan required")
	}
	if source.filter.Matches(path) {
		if _, exists := source.pending[path]; !exists && len(source.pending) >= 4096 {
			source.fault = errors.New("native filesystem event capacity exceeded")
		} else {
			source.pending[path] = struct{}{}
		}
	}
	select {
	case source.wake <- struct{}{}:
	default:
	}
	source.mu.Unlock()
}

// Next waits for events, coalescing until 50ms quiet or 1600ms total, matching
// the captured WatchFiles defaults. Context cancellation leaves ownership intact.
func (source *NativeEvents) Next(ctx context.Context) (EventBatch, error) {
	source.nextMu.Lock()
	defer source.nextMu.Unlock()
	// A cancelled batch wait must remain available to the next caller.
	defer func() {
		source.mu.Lock()
		defer source.mu.Unlock()
		if source.seen {
			select {
			case source.wake <- struct{}{}:
			default:
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return EventBatch{}, err
	}
	select {
	case <-ctx.Done():
		return EventBatch{}, ctx.Err()
	case <-source.stop:
		return EventBatch{}, ErrNativeEventsClosed
	case <-source.wake:
	}
	quiet := time.NewTimer(50 * time.Millisecond)
	limit := time.NewTimer(1600 * time.Millisecond)
	defer quiet.Stop()
	defer limit.Stop()
	for {
		select {
		case <-ctx.Done():
			return EventBatch{}, ctx.Err()
		case <-source.stop:
			return EventBatch{}, ErrNativeEventsClosed
		case <-source.wake:
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(50 * time.Millisecond)
		case <-quiet.C:
			return source.take()
		case <-limit.C:
			return source.take()
		}
	}
}

func (source *NativeEvents) take() (EventBatch, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	batch := EventBatch{Paths: make([]string, 0, len(source.pending))}
	for path := range source.pending {
		batch.Paths = append(batch.Paths, path)
	}
	sort.Strings(batch.Paths)
	clear(source.pending)
	source.seen = false
	select {
	case <-source.wake:
	default:
	}
	return batch, source.fault
}

func (source *NativeEvents) Close() error {
	source.closeOnce.Do(func() { close(source.stop) })
	<-source.done
	return nil
}
