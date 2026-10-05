package agent

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/luoyjx/mini-loop/go/protocol"
)

var ErrTranscriptRead = errors.New("stored transcript read failed")

// TranscriptSelection's zero value selects the current epoch. Its optional
// exact integer is owned and detached; even an out-of-range HTTP integer retains
// its identity for the source-compatible not-found response.
type TranscriptSelection struct {
	epoch *big.Int
}

func SelectTranscriptEpoch(epoch TranscriptEpoch) TranscriptSelection {
	return TranscriptSelection{epoch: big.NewInt(int64(epoch))}
}

func SelectTranscriptEpochNumber(epoch *big.Int) TranscriptSelection {
	if epoch == nil {
		return TranscriptSelection{}
	}
	return TranscriptSelection{epoch: new(big.Int).Set(epoch)}
}

type TranscriptEpochNotFound struct {
	Requested string
	Current   TranscriptEpoch
}

func (err *TranscriptEpochNotFound) Error() string {
	return fmt.Sprintf("no epoch %s (current: %d)", err.Requested, err.Current)
}

// TranscriptSnapshot is the persisted projection, including superseded epochs.
// It is not live history, and a crash tail may contain unanswered tool uses.
type TranscriptSnapshot struct {
	Session  SessionID          `json:"session"`
	Epoch    TranscriptEpoch    `json:"epoch"`
	Epochs   TranscriptEpoch    `json:"epochs"`
	Messages []protocol.Message `json:"messages"`
}

// ReadTranscript reads the configured backend without taking turn admission,
// flushing live history, acquiring a lease or repairing historical messages.
// The caller owns authorization. Backend reads must honor the supplied context.
func (session *ManagedSession) ReadTranscript(ctx context.Context, selection TranscriptSelection) (TranscriptSnapshot, error) {
	var result TranscriptSnapshot
	if err := ctx.Err(); err != nil {
		return result, err
	}
	p := session.core.persistence
	var store TranscriptStore
	if p != nil {
		p.mu.Lock()
		if !p.disabled {
			store = p.store
		}
		p.mu.Unlock()
	}
	var current TranscriptEpoch
	if store != nil {
		if err := stateFault(func() error { var err error; current, err = store.TranscriptEpoch(ctx, session.ID()); return err }); err != nil {
			return result, fmt.Errorf("%w: %w", ErrTranscriptRead, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if current < 0 {
		return result, fmt.Errorf("%w: negative current epoch", ErrTranscriptRead)
	}
	target := big.NewInt(int64(current))
	if selection.epoch != nil {
		target.Set(selection.epoch)
	}
	if target.Sign() < 1 || target.Cmp(big.NewInt(int64(current))) > 0 {
		return result, &TranscriptEpochNotFound{Requested: target.String(), Current: current}
	}
	// A valid target is bounded by the concrete backend's current epoch, so this
	// conversion cannot overflow TranscriptEpoch on the current platform.
	epoch := TranscriptEpoch(target.Int64())
	var messages []protocol.Message
	if err := stateFault(func() error { var err error; messages, err = store.LoadMessages(ctx, session.ID(), &epoch); return err }); err != nil {
		return result, fmt.Errorf("%w: %w", ErrTranscriptRead, err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result = TranscriptSnapshot{Session: session.ID(), Epoch: TranscriptEpoch(target.Int64()), Epochs: current, Messages: make([]protocol.Message, len(messages))}
	for i, message := range messages {
		if err := ctx.Err(); err != nil {
			return TranscriptSnapshot{}, err
		}
		if err := message.Validate(); err != nil {
			return TranscriptSnapshot{}, fmt.Errorf("%w: invalid stored message: %w", ErrTranscriptRead, err)
		}
		result.Messages[i] = protocol.Message{Role: message.Role, Content: message.Content.Clone()}
	}
	return result, nil
}
