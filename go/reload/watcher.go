// Package reload provides explicitly selected development reload components.
// A watcher observes source files; it neither builds code nor owns a server.
package reload

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Language string

const (
	GoSource     Language = "go"
	PythonSource Language = "python"
)

type WatchConfig struct {
	Roots    []string
	Language Language
	// Nil selects Source's 250ms default. Explicit zero permits immediate polling.
	Delay *time.Duration
}

// Change's zero value means no changed file. A nonzero path is absolute.
type Change struct{ Path string }

type Watcher struct {
	mu        sync.Mutex
	roots     []string
	extension string
	delay     time.Duration
	mtimes    map[string]float64
}

func NewWatcher(config WatchConfig) (*Watcher, error) {
	extension := ".go"
	switch config.Language {
	case "", GoSource:
	case PythonSource:
		extension = ".py"
	default:
		return nil, errors.New("reload watcher requires Go or Python source")
	}
	delay := 250 * time.Millisecond
	if config.Delay != nil {
		delay = *config.Delay
	}
	if delay < 0 || len(config.Roots) == 0 {
		return nil, errors.New("reload watcher requires roots and nonnegative delay")
	}
	watcher := &Watcher{extension: extension, delay: delay, mtimes: make(map[string]float64)}
	for _, root := range config.Roots {
		if root == "" {
			return nil, errors.New("reload root must not be empty")
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
			return nil, errors.New("reload root must be a directory")
		}
		watcher.roots = append(watcher.roots, path)
	}
	return watcher, nil
}

// Scan follows Source StatReload: first sight records mtime; only an increase
// triggers a change. Deletions and new paths do not trigger; a change remains
// observable until Reset. Directory symlinks are not traversed. File symlinks
// resolve to their actual path, as in Source. Roots are trusted operator input.
func (watcher *Watcher) Scan(ctx context.Context) (Change, error) {
	if err := ctx.Err(); err != nil {
		return Change{}, err
	}
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	var change Change
	for _, root := range watcher.roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil || !strings.HasSuffix(entry.Name(), watcher.extension) {
				return nil
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return nil
			}
			old, found := watcher.mtimes[resolved]
			// Source compares stat.st_mtime's floating-point seconds.
			stamp := info.ModTime()
			mtime := float64(stamp.Unix()) + float64(stamp.Nanosecond())/1e9
			if !found {
				watcher.mtimes[resolved] = mtime
			} else if mtime > old {
				change.Path = resolved
				return fs.SkipAll
			}
			return nil
		})
		if err != nil {
			return Change{}, err
		}
		if change.Path != "" {
			return change, nil
		}
	}
	return change, ctx.Err()
}

func (watcher *Watcher) Next(ctx context.Context) (Change, error) {
	timer := time.NewTimer(watcher.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Change{}, ctx.Err()
	case <-timer.C:
		return watcher.Scan(ctx)
	}
}

// Reset establishes fresh baselines after the supervisor replaces its child.
func (watcher *Watcher) Reset() {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	watcher.mtimes = make(map[string]float64)
}
