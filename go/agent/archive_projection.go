package agent

import (
	"encoding/json"
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Callers supply bytes from concrete archival encoders. This projection masks
// decoded keys and strings without permitting nonfinite request JSON.
func maskedLegacyArchive(data []byte, mask func(string) string) ([]byte, error) {
	if len(data) > protocol.MaxProjectionBytes {
		return nil, errors.New("archive projection exceeds byte bound")
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return nil, err
	}
	if mask != nil {
		value = value.MapStrings(mask)
	}
	data, err = value.MarshalLegacyUTF8()
	if err != nil {
		return nil, err
	}
	if len(data) > protocol.MaxProjectionBytes {
		return nil, errors.New("archive projection exceeds byte bound")
	}
	return data, nil
}

func maskedEventArchive(record SessionEventRecord, mask func(string) string) ([]byte, error) {
	data, err := record.MarshalArchiveJSON()
	if err != nil {
		return nil, err
	}
	if json.Valid(data) {
		text, err := protocol.MaskedPythonJSON(record, mask, false, true)
		return []byte(text), err
	}
	return maskedLegacyArchive(data, mask)
}

func maskedMessageArchive(message protocol.Message, mask func(string) string) ([]byte, error) {
	data, err := message.MarshalArchiveJSON()
	if err != nil {
		return nil, err
	}
	if json.Valid(data) {
		text, err := protocol.MaskedPythonJSON(message, mask, false, true)
		return []byte(text), err
	}
	return maskedLegacyArchive(data, mask)
}
