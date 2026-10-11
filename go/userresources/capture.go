package userresources

import (
	"sync"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

const MaxCapturedMessages = 64

type CaptureError string

const (
	CaptureScreeningUnavailable CaptureError = "secret_screening_unavailable"
	CaptureFailed               CaptureError = "capture_failed"
)

type CaptureSnapshot struct {
	CompactedHistoryExcluded bool            `json:"compacted_history_excluded"`
	Error                    CaptureError    `json:"error"`
	Established              bool            `json:"established"`
	Messages                 []ProjectedText `json:"messages"`
	Omitted                  int             `json:"omitted"`
}

// CaptureLedger is process-local evidence. Only a trusted caller may Record a
// successfully completed authenticated turn; model text cannot grant admission.
// Its zero value is an empty ledger. Copies must not be made after first use.
type CaptureLedger struct {
	mu    sync.Mutex
	state CaptureSnapshot
}

func captureScreeningAvailable(masker memory.Masker) (available bool) {
	defer func() {
		if recover() != nil {
			available = false
		}
	}()
	names, ok := masker.(interface{ Names() []secrets.Name })
	if !ok || len(names.Names()) == 0 {
		return true
	}
	reports, ok := masker.(interface {
		Unresolved() []secrets.Name
		ShortValues() []secrets.Name
	})
	return ok && len(reports.Unresolved()) == 0 && len(reports.ShortValues()) == 0
}

// Record contains screening/masking failures without changing the completed
// turn. A screening error is sticky for this agent, including after recovery.
func (ledger *CaptureLedger) Record(user, final string, history []protocol.Message, masker memory.Masker) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	defer func() {
		if recover() != nil {
			ledger.state.Error = CaptureFailed
		}
	}()
	if !utf8.ValidString(user) || !utf8.ValidString(final) {
		ledger.state.Error = CaptureFailed
		return
	}
	user, final = pytext.Strip(user), pytext.Strip(final)
	if user == "" || final == "" {
		return
	}
	pair := make([]ProjectedText, 2)
	for i, message := range []ProjectedText{{Role: "user", Content: user}, {Role: "assistant", Content: final}} {
		masked, err := maskProjectionText(message, masker)
		if err != nil {
			ledger.state.Error = CaptureFailed
			return
		}
		pair[i] = masked
	}
	if !captureScreeningAvailable(masker) {
		ledger.state.Error = CaptureScreeningUnavailable
		return
	}
	ledger.state.Established = true
	ledger.state.Messages = append(ledger.state.Messages, pair...)
	for len(ledger.state.Messages) > 0 {
		wire, err := protocol.PythonJSON(ledger.state.Messages, false, true)
		if err != nil {
			ledger.state.Error = CaptureFailed
			return
		}
		if len(ledger.state.Messages) <= MaxCapturedMessages && utf8.RuneCountInString(wire) <= MaxProjectionChars {
			break
		}
		ledger.state.Messages = append([]ProjectedText(nil), ledger.state.Messages[1:]...)
		ledger.state.Omitted++
	}
	for _, message := range history {
		if text, ok := message.Content.Plain(); ok && projectionBoundary(projectionGap, text) {
			ledger.state.CompactedHistoryExcluded = true
			break
		}
	}
}

func (ledger *CaptureLedger) Snapshot() CaptureSnapshot {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	state := ledger.state
	state.Messages = append([]ProjectedText{}, state.Messages...)
	return state
}

// capturedField interprets only the two known source display fields after key
// masking. Content wins a key collision, as in the source dictionary.
func capturedField(message ProjectedText, key string) string {
	contentKey, roleKey := "content", "role"
	if message.maskedKeys {
		contentKey, roleKey = message.contentKey, message.roleKey
	}
	if contentKey == key {
		return message.Content
	}
	if roleKey == key {
		return string(message.Role)
	}
	return ""
}

func (ledger *CaptureLedger) Project(masker memory.Masker, maxChars int) (SkillProjection, error) {
	state := ledger.Snapshot()
	if state.Error != "" {
		return SkillProjection{}, draftError(DraftCaptureSourceUnavailable, 503)
	}
	rows := make([]AuthenticatedText, 0, len(state.Messages))
	for _, message := range state.Messages {
		rows = append(rows, AuthenticatedText{Role: protocol.Role(capturedField(message, "role")), Content: capturedField(message, "content")})
	}
	return ProjectAuthenticatedText(rows, masker, ProjectionOptions{MaxChars: maxChars, PriorOmitted: state.Omitted, CompactedHistoryExcluded: state.CompactedHistoryExcluded})
}
