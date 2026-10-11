package protocol

// ModelFailure is transport-independent recovery evidence. Adapters expose
// bounded diagnostics and finite Retry-After seconds; no HTTP handles survive.
type ModelFailureKind string

const (
	ModelFailureOther             ModelFailureKind = "other"
	ModelFailureOverloaded        ModelFailureKind = "overloaded"
	ModelFailureRateLimit         ModelFailureKind = "rate_limit"
	ModelFailureConnection        ModelFailureKind = "connection"
	ModelFailureTimeout           ModelFailureKind = "timeout"
	ModelFailureStatus            ModelFailureKind = "status"
	ModelFailureStreamingRequired ModelFailureKind = "streaming_required"
	ModelFailureProtocol          ModelFailureKind = "protocol"
	ModelFailureLimit             ModelFailureKind = "wire_limit"
)

type ModelFailure struct {
	Kind              ModelFailureKind
	Class, Message    string
	Status            int
	RetryAfterSeconds *float64
}

// ListedNonStreamingCeiling is the SDK's explicit model map, not its generic
// duration estimate. Recovery must attempt and handle unknown-model refusal.
func ListedNonStreamingCeiling(model string) (int, bool) {
	switch model {
	case "claude-opus-4-20250514", "claude-opus-4-0", "claude-4-opus-20250514", "anthropic.claude-opus-4-20250514-v1:0", "claude-opus-4@20250514", "claude-opus-4-1-20250805", "anthropic.claude-opus-4-1-20250805-v1:0", "claude-opus-4-1@20250805":
		return 8192, true
	default:
		return 0, false
	}
}
