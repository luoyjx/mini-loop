package agent

import "github.com/luoyjx/mini-loop/go/protocol"

type StopEventKind string

const (
	EventTurnPaused            StopEventKind = "turn_paused"
	EventProviderRefusal       StopEventKind = "provider_refusal"
	EventProviderStopUnhandled StopEventKind = "provider_stop_unhandled"
)

// ProviderStopEvent records the stop-reason outcomes exposed by the Python
// event stream. The session creates these variants; callers receive copies.
type ProviderStopEvent struct {
	kind       StopEventKind
	reason     protocol.StopReason
	resumption int
	detail     string
}

func (event ProviderStopEvent) Kind() StopEventKind         { return event.kind }
func (event ProviderStopEvent) Reason() protocol.StopReason { return event.reason }
func (event ProviderStopEvent) Resumption() int             { return event.resumption }
func (event ProviderStopEvent) Detail() string              { return event.detail }
