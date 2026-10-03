package protocol

// StreamDelta is validated progress, not a completed content block. Unsigned
// thinking and incomplete tool JSON never enter a transcript through this seam.
type StreamDeltaKind string

const (
	DeltaText     StreamDeltaKind = "text"
	DeltaThinking StreamDeltaKind = "thinking"
)

type StreamDelta struct {
	Kind StreamDeltaKind
	Text string
}
