package httpapi

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Tool observation SSE has Source's legacy numeric vocabulary. REST responses
// and subsequent model requests retain standard finite encoding.
func toolObservationStreamJSON(s *Server, record agent.SessionEventRecord) ([]byte, error) {
	data, err := record.MarshalArchiveJSON()
	if err != nil {
		return nil, err
	}
	if json.Valid(data) {
		return recordingJSON(s, record)
	}
	if len(data) > protocol.MaxProjectionBytes {
		return nil, errors.New("event stream projection exceeds byte bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, err
	}
	if masker := s.manager.RecordingMasker(); masker != nil {
		value = value.MapStrings(masker.MaskText)
	}
	data, err = jsonvalue.AppendLegacyDefault(value)
	if err != nil {
		return nil, err
	}
	if len(data) > protocol.MaxProjectionBytes {
		return nil, errors.New("event stream projection exceeds byte bound")
	}
	return data, nil
}
