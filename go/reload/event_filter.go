package reload

import (
	"errors"
	"path/filepath"
	"strings"
)

// EventFilter implements the captured POSIX default WatchFiles filename policy, adapted
// to Go sources or its explicit Python differential profile. It is lexical:
// removed paths and suffix-matching directories can match. It grants no root,
// filesystem or execution authority. The zero value matches nothing.
type EventFilter struct{ extension string }

func NewEventFilter(language Language) (EventFilter, error) {
	switch language {
	case "", GoSource:
		return EventFilter{extension: ".go"}, nil
	case PythonSource:
		return EventFilter{extension: ".py"}, nil
	default:
		return EventFilter{}, errors.New("unknown reload source language")
	}
}

func (filter EventFilter) Matches(path string) bool {
	name := filepath.Base(path)
	// Default excludes .* and ~* reject these basenames; .py[cod] and .sw.*
	// are already covered by the dot prefix. Hidden parent directories remain
	// eligible because Uvicorn passes watch_filter=None to watchfiles.watch.
	return filter.extension != "" && strings.HasSuffix(name, filter.extension) &&
		!strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "~")
}
