package reload

import (
	"context"
	"io"
)

// EventBatch distinguishes a batch containing only excluded names from no batch.
// Paths are sorted, unique, lexical observer paths, never leaf-stat-resolved.
type EventBatch struct{ Paths []string }

// NativeEvents owns one OS subscription. Callers must Close it, including after
// a cancelled Next. Only one Next may be active; Close joins native cleanup.
// This explicit SDK does not select environment overrides or activate the CLI.
type NativeEvents struct{ *eventReader }

func NewNativeEvents(roots []string, language Language) (*NativeEvents, error) {
	return newNativeEvents(context.Background(), roots, language, nil, "")
}

func newNativeEvents(ctx context.Context, roots []string, language Language, diagnostic io.Writer, excludedWorkspace string) (*NativeEvents, error) {
	filter, err := NewEventFilter(language)
	if err != nil {
		return nil, err
	}
	owned, err := captureEventRoots(roots)
	if err != nil {
		return nil, err
	}
	source := &NativeEvents{eventReader: newEventReader(filter, NativeBackend, diagnostic)}
	source.excludedWorkspace = excludedWorkspace
	if err := source.start(ctx, owned); err != nil {
		return nil, err
	}
	return source, nil
}
