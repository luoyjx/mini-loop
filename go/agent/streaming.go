package agent

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// StreamingProvider is an optional consumer-owned seam. A direct Provider
// remains one-shot. Calls and callbacks are synchronous and context-owned;
// implementations must stop emitting before returning, including on failure.
type StreamingProvider interface {
	CompleteStream(context.Context, protocol.ModelRequest, func(protocol.StreamDelta) error) (protocol.ModelReply, error)
}

type StreamID string

const DefaultDeltaCoalesceChars = 200
const DefaultDeltaCoalesceDuration = 200 * time.Millisecond

type StreamStartEvent struct {
	StreamID    StreamID
	Phase       TextPhase
	Provisional bool
}

func (event SessionEvent) StreamStart() (StreamStartEvent, bool) {
	return event.streamStart, event.kind == EventStreamStart
}

// Progress coalescing is per session generation, not provider-global state.
// Time is checked when a fragment arrives, matching Python (no timer flush).
func (s *Session) streamingComplete(ctx context.Context, provider StreamingProvider, request protocol.ModelRequest) (protocol.ModelReply, error) {
	id, err := newSpan("stream_", 16)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	s.lastStreamID = StreamID(id)
	s.streamedText = ""
	s.events.append(SessionEvent{kind: EventStreamStart, streamStart: StreamStartEvent{s.lastStreamID, PhaseCommentary, true}})
	var pending, answer, shown strings.Builder
	totalBytes := 0
	chars := 0
	lastFlush := time.Now()
	flush := func() {
		if pending.Len() == 0 {
			return
		}
		shown.WriteString(answer.String())
		s.streamedText = shown.String()
		s.events.append(SessionEvent{kind: EventAssistantDelta, delta: AssistantDeltaEvent{Text: pending.String(), StreamID: s.lastStreamID, Phase: PhaseCommentary, Provisional: true}})
		pending.Reset()
		answer.Reset()
		chars = 0
		lastFlush = time.Now()
	}
	reply, err := provider.CompleteStream(ctx, request, func(delta protocol.StreamDelta) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if (delta.Kind != protocol.DeltaText && delta.Kind != protocol.DeltaThinking) || !utf8.ValidString(delta.Text) {
			return errors.New("invalid streaming progress")
		}
		totalBytes += len(delta.Text)
		if totalBytes > protocol.MaxWireBytes {
			return errors.New("streaming progress exceeds protocol limit")
		}
		pending.WriteString(delta.Text)
		chars += utf8.RuneCountInString(delta.Text)
		if delta.Kind == protocol.DeltaText {
			answer.WriteString(delta.Text)
		}
		if chars >= DefaultDeltaCoalesceChars || time.Since(lastFlush) >= DefaultDeltaCoalesceDuration {
			flush()
		}
		return nil
	})
	if err != nil {
		return protocol.ModelReply{}, err
	}
	// Only flushed answer text is recoverable on interruption. Thinking retains
	// its signature only in the final reply, never as synthetic assistant text.
	if err = ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	if err = reply.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	flush()
	s.streamedText = "" // including successful internal compaction calls
	return reply, nil
}
