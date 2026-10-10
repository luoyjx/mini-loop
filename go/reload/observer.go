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
	// Workspace is the allocated compiler workspace excluded for this subscription.
	Workspace *BuildWorkspace
	// Debug output reports raw lexical observer names with native flags (or zero
	// for polling). A selected writer must return promptly. Nil uses os.Stdout.
	DiagnosticWriter io.Writer
}

func NewObserver(ctx context.Context, config ObserverConfig) (observer Observer, err error) {
	if ctx == nil {
		return nil, errors.New("observer requires a construction context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	excludedWorkspace := ""
	if config.Workspace != nil {
		excludedWorkspace, err = config.Workspace.acquire()
		if err != nil {
			return nil, err
		}
		defer func() {
			if observer == nil {
				config.Workspace.release()
			}
		}()
	}
	bind := func(source Observer) Observer {
		if config.Workspace == nil {
			return source
		}
		return &workspaceObserver{Observer: source, workspace: config.Workspace}
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
		source, err := newNativeEvents(ctx, config.Roots, config.Language, diagnostic, excludedWorkspace)
		if err == nil && ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), source.Close())
		}
		if err != nil {
			return nil, err
		}
		return bind(source), nil
	case PollingBackend:
		source, err := newPollingEvents(ctx, config.Roots, config.Language, config.Profile.pollDelayMilliseconds, diagnostic, excludedWorkspace)
		if err != nil {
			return nil, err
		}
		return bind(source), nil
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
