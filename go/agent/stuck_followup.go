package agent

import (
	"bytes"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// Only an actual built-in inspection fault records this history observation.
// It is absent from config, snapshots, child sessions and durable restore.
func (s *Session) observeUnpublishedToolBatch() {
	batch, err := protocol.ObserveUnpublishedToolBatch(s.messages)
	if err == nil {
		s.unpublishedBatch = &batch
	}
}

func (s *Session) validateDispatchTranscript(messages []protocol.Message) error {
	if s.unpublishedBatch != nil {
		return s.unpublishedBatch.ValidateTranscript(messages)
	}
	return protocol.ValidateTranscript(messages)
}

// A compactor may refuse the known pairing gap before doing work. Only its
// exact named pairing error and unchanged output may continue to the provider;
// arbitrary extension failures and transcript rewrites retain their guards.
func (s *Session) unchangedUnpublishedTranscript(messages []protocol.Message) bool {
	if s.unpublishedBatch == nil || s.unpublishedBatch.ValidateTranscript(messages) != nil || len(messages) != len(s.messages) {
		return false
	}
	for i, message := range messages {
		left, leftErr := message.MarshalArchiveJSON()
		right, rightErr := s.messages[i].MarshalArchiveJSON()
		if leftErr != nil || rightErr != nil || !bytes.Equal(left, right) {
			return false
		}
	}
	return true
}

type fakeTranscriptFailure struct {
	pairing *protocol.UnansweredToolUsesError
}

func (e *fakeTranscriptFailure) Error() string { return e.pairing.SourceDetail() }
func (e *fakeTranscriptFailure) ProviderErrorDetail() (string, bool) {
	return "InvalidTranscript: " + e.Error(), true
}
func (e *fakeTranscriptFailure) RecoveryFailure() protocol.ModelFailure {
	return protocol.ModelFailure{Kind: protocol.ModelFailureOther, Class: "InvalidTranscript", Message: e.Error()}
}

type dispatchErrorDetail interface {
	DispatchErrorDetail() (string, bool)
}

func (e *fakeTranscriptFailure) DispatchErrorDetail() (string, bool) {
	return e.ProviderErrorDetail()
}
