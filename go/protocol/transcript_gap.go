package protocol

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// UnansweredToolUsesError identifies the immediate pairing failure without
// weakening the ordinary public transcript validator.
type UnansweredToolUsesError struct {
	index int
	ids   []string
}

func (e *UnansweredToolUsesError) Error() string {
	if len(e.ids) == 0 {
		return "unanswered tool-use batch"
	}
	return fmt.Sprintf("messages.%d: tool_use %q has no immediately following tool_result", e.index, e.ids[0])
}

func (e *UnansweredToolUsesError) SameBatch(other *UnansweredToolUsesError) bool {
	if e == nil || other == nil || e.index != other.index || len(e.ids) == 0 || len(e.ids) != len(other.ids) {
		return false
	}
	for i, id := range e.ids {
		if id != other.ids[i] {
			return false
		}
	}
	return true
}

// SourceDetail is the fixed fake SDK's diagnostic for this same pairing error.
func (e *UnansweredToolUsesError) SourceDetail() string {
	ids := make([]string, len(e.ids))
	for i, id := range e.ids {
		ids[i] = pytext.Repr(id)
	}
	return fmt.Sprintf("messages.%d: `tool_use` ids were found without `tool_result` blocks immediately after: [%s]", e.index, strings.Join(ids, ", "))
}

// UnpublishedToolBatch observes an exact retained history whose final assistant
// batch did not reach result publication. It grants no execution authority and
// has no JSON representation. Session restore, fork and child construction do
// not reconstruct this process-local observation.
type UnpublishedToolBatch struct {
	count  int
	prefix [sha256.Size]byte
}

// ObserveUnpublishedToolBatch requires a single complete final unpaired batch.
// Ordinary malformed history and partial result publication cannot be registered.
func ObserveUnpublishedToolBatch(messages []Message) (UnpublishedToolBatch, error) {
	var missing *UnansweredToolUsesError
	if err := ValidateTranscript(messages); !errors.As(err, &missing) || missing.index != len(messages)-1 || messages[missing.index].Role != RoleAssistant {
		return UnpublishedToolBatch{}, errors.New("unpublished batch requires a final unpaired assistant tool batch")
	}
	if err := validateTranscript(messages, len(messages)-1); err != nil {
		return UnpublishedToolBatch{}, err
	}
	digest, err := transcriptDigest(messages)
	if err != nil {
		return UnpublishedToolBatch{}, err
	}
	return UnpublishedToolBatch{count: len(messages), prefix: digest}, nil
}

// ValidateTranscript admits only the registered missing batch, with every byte
// of the original history unchanged. All later messages retain normal shape and
// pairing checks; an additional gap is never covered by this observation.
func (batch UnpublishedToolBatch) ValidateTranscript(messages []Message) error {
	if batch.count < 1 || len(messages) < batch.count {
		return errors.New("unpublished batch history is absent")
	}
	digest, err := transcriptDigest(messages[:batch.count])
	if err != nil {
		return err
	}
	if digest != batch.prefix {
		return errors.New("unpublished batch history changed")
	}
	return validateTranscript(messages, batch.count-1)
}

func transcriptDigest(messages []Message) ([sha256.Size]byte, error) {
	hash := sha256.New()
	for _, message := range messages {
		data, err := message.MarshalArchiveJSON()
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(data)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(data)
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}
