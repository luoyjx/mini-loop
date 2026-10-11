package agent

import (
	"context"
	"encoding/json"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/sessionquery"
)

// The adapter carries the configured store, not a model-selected identity.
// A manager with no state store matches Source's NullStateStore search surface.
type transcriptSearchReader struct{ store TranscriptStore }

func (reader transcriptSearchReader) TranscriptEpoch(ctx context.Context, id sessionquery.SessionID) (sessionquery.Epoch, error) {
	if reader.store == nil {
		return 0, nil
	}
	epoch, err := reader.store.TranscriptEpoch(ctx, SessionID(id))
	return sessionquery.Epoch(epoch), err
}
func (reader transcriptSearchReader) LoadMessages(ctx context.Context, id sessionquery.SessionID, epoch sessionquery.Epoch) ([]sessionquery.Message, error) {
	if reader.store == nil {
		return nil, nil
	}
	selected := TranscriptEpoch(epoch)
	stored, err := reader.store.LoadMessages(ctx, SessionID(id), &selected)
	if err != nil {
		return nil, err
	}
	messages := make([]sessionquery.Message, len(stored))
	for index, message := range stored {
		data, err := json.Marshal(message.Content)
		if err != nil {
			return nil, err
		}
		content, err := jsonvalue.Decode(string(data))
		if err != nil {
			return nil, err
		}
		messages[index] = sessionquery.Message{Role: jsonvalue.TextValue(string(message.Role)), Content: content}
	}
	return messages, nil
}
