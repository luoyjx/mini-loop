package reload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"
)

type observationKind uint8

const (
	nativeObservation observationKind = iota
	addedObservation
	modifiedObservation
	deletedObservation
)

type eventFingerprint struct {
	path  string
	flags uint32
	kind  observationKind
}

type eventReader struct {
	mu                sync.Mutex
	nextMu            sync.Mutex
	pending           map[string]struct{}
	fingerprints      map[eventFingerprint]struct{}
	seen              bool
	fault             error
	filter            EventFilter
	wake              chan struct{}
	stop              chan struct{}
	done              chan struct{}
	closeOnce         sync.Once
	backend           ObserverBackend
	diagnostic        io.Writer
	excludedWorkspace string
}

var ErrEventsClosed = errors.New("event subscription closed")

// ErrNativeEventsClosed retains the earlier native SDK identity.
var ErrNativeEventsClosed = ErrEventsClosed

func newEventReader(filter EventFilter, backend ObserverBackend, diagnostic io.Writer) *eventReader {
	return &eventReader{pending: make(map[string]struct{}), fingerprints: make(map[eventFingerprint]struct{}), filter: filter, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), backend: backend, diagnostic: diagnostic}
}
func (source *eventReader) writeDiagnostic(path string, flags uint32) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("event diagnostic writer panicked")
		}
	}()
	_, err = fmt.Fprintf(source.diagnostic, "reload %s event: path=%q flags=%d\n", source.backend, path, flags)
	return err
}
func (source *eventReader) fail(err error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.fault = err
	source.seen = true
	select {
	case source.wake <- struct{}{}:
	default:
	}
}

func (source *eventReader) record(path string, flags uint32) {
	source.recordEvent(eventFingerprint{path: path, flags: flags, kind: nativeObservation})
}
func (source *eventReader) recordPolling(path string, kind observationKind) {
	source.recordEvent(eventFingerprint{path: path, kind: kind})
}
func (source *eventReader) recordEvent(event eventFingerprint) {
	path, flags := event.path, event.flags
	source.mu.Lock()
	if insideBuildWorkspace(source.excludedWorkspace, path) && flags&7 == 0 {
		source.mu.Unlock()
		return
	}
	source.seen = true
	if source.diagnostic != nil {
		if err := source.writeDiagnostic(path, flags); err != nil {
			source.fault = err
		}
	}
	if flags&7 != 0 {
		source.fault = errors.New("native filesystem events dropped; rescan required")
	}
	if source.filter.Matches(path) {
		if _, exists := source.pending[path]; !exists && len(source.pending) >= 4096 {
			source.fault = errors.New("filesystem event capacity exceeded")
		} else {
			source.pending[path] = struct{}{}
		}
	}
	// Source coalesces a set of raw changes. Duplicates do not extend its
	// quiet window, even when polling produces them faster than 50ms.
	if _, duplicate := source.fingerprints[event]; !duplicate {
		if len(source.fingerprints) < 4096 {
			source.fingerprints[event] = struct{}{}
		}
		select {
		case source.wake <- struct{}{}:
		default:
		}
	}
	source.mu.Unlock()
}

// Next waits for events, coalescing until 50ms quiet or 1600ms total, matching
// the captured WatchFiles defaults. Context cancellation leaves ownership intact.
func (source *eventReader) Next(ctx context.Context) (EventBatch, error) {
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

func (source *eventReader) take() (EventBatch, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	batch := EventBatch{Paths: make([]string, 0, len(source.pending))}
	for path := range source.pending {
		batch.Paths = append(batch.Paths, path)
	}
	sort.Strings(batch.Paths)
	clear(source.pending)
	clear(source.fingerprints)
	source.seen = false
	select {
	case <-source.wake:
	default:
	}
	return batch, source.fault
}

func (source *eventReader) Close() error {
	source.closeOnce.Do(func() { close(source.stop) })
	<-source.done
	return nil
}
