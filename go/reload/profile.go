package reload

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"strings"
	"unicode/utf8"
)

// ObserverBackend is the selected WatchFiles observation strategy. StatReload
// remains a separate fallback contract, never an alias for forced polling.
type ObserverBackend string

const (
	NativeBackend  ObserverBackend = "native"
	PollingBackend ObserverBackend = "polling"
)

type ObserverHost struct{ System, Release string }

// ObserverProfile holds a captured selection, without retaining environment or
// probing the host. Its zero value is invalid for observer construction.
type ObserverProfile struct {
	backend               ObserverBackend
	pollDelayMilliseconds uint64
	debug                 bool
}

func (profile ObserverProfile) Backend() ObserverBackend      { return profile.backend }
func (profile ObserverProfile) PollDelayMilliseconds() uint64 { return profile.pollDelayMilliseconds }
func (profile ObserverProfile) Debug() bool                   { return profile.debug }

// SelectObserverProfile follows the pinned WatchFiles environment helpers and
// Uvicorn's explicit ignore-permission policy. Nil environment means empty;
// callers capture host/environment once. Explicit force selection wins over
// environment, while poll-delay conversion still applies to native selection.
func SelectObserverProfile(environment map[string]string, host ObserverHost, forcePolling *bool) (ObserverProfile, error) {
	polling := false
	if forcePolling != nil {
		polling = *forcePolling
	} else if value := environment["WATCHFILES_FORCE_POLLING"]; value != "" {
		switch pytext.Lower(value) {
		case "false", "disable", "disabled":
		default:
			polling = true
		}
	} else {
		polling = pytext.Lower(host.System) == "linux" && strings.Contains(pytext.Lower(host.Release), "microsoft-standard")
	}
	delay := uint64(300)
	if value, decimal := pytext.Decimal(environment["WATCHFILES_POLL_DELAY_MS"]); decimal {
		if utf8.RuneCountInString(environment["WATCHFILES_POLL_DELAY_MS"]) > 4300 {
			return ObserverProfile{}, errors.New("poll delay exceeds Source's decimal integer digit limit")
		}
		// RustNotify accepts a u64 millisecond parameter for either backend.
		if !value.IsUint64() {
			return ObserverProfile{}, errors.New("poll delay does not fit Source's uint64 milliseconds")
		}
		delay = value.Uint64()
	}
	backend := NativeBackend
	if polling {
		backend = PollingBackend
	}
	return ObserverProfile{backend: backend, pollDelayMilliseconds: delay, debug: environment["WATCHFILES_DEBUG"] != ""}, nil
}
