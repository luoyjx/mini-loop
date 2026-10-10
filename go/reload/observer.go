package reload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Observer is the narrow owned batch subscription consumed by development reload.
// A cancelled Next does not Close it. Callers own Close after construction.
type Observer interface {
	Next(context.Context) (EventBatch, error)
	Close() error
}

type ObserverConfig struct {
	Roots    []string
	Language Language
	Profile  ObserverProfile
	// Debug output reports raw lexical observer names with native flags (or zero
	// for polling). A selected writer must return promptly. Nil uses os.Stdout.
	DiagnosticWriter io.Writer
}

func NewObserver(ctx context.Context, config ObserverConfig) (Observer, error) {
	if ctx == nil {
		return nil, errors.New("observer requires a construction context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var diagnostic io.Writer
	if config.Profile.debug {
		diagnostic = config.DiagnosticWriter
		if diagnostic == nil {
			diagnostic = os.Stdout
		}
	}
	switch config.Profile.backend {
	case NativeBackend:
		source, err := newNativeEvents(config.Roots, config.Language, diagnostic)
		if err == nil && ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), source.Close())
		}
		return source, err
	case PollingBackend:
		return newPollingEvents(ctx, config.Roots, config.Language, config.Profile.pollDelayMilliseconds, diagnostic)
	default:
		return nil, errors.New("observer requires a selected native or polling profile")
	}
}

func captureEventRoots(roots []string) ([]string, error) {
	if len(roots) == 0 {
		return nil, errors.New("events require explicit roots")
	}
	owned := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			return nil, errors.New("event root must not be empty")
		}
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
			return nil, fmt.Errorf("event root is not a directory: %s", path)
		}
		owned = append(owned, path)
	}
	return owned, nil
}
