package sqlitestate

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"io"
	"strings"
)

var ErrSessionMetadata = errors.New("stored session metadata is outside the typed record contract")

// Typed flat arrays do not inherit the generic archival container-size limit:
// Source metadata and our caller-owned SessionRecord slices are unbounded.
func decodeMetadataArray[T any](text string) ([]T, error) {
	if text == "" {
		text = "[]"
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	var items []T
	if err := decoder.Decode(&items); err != nil {
		return nil, err
	}
	if items == nil {
		return nil, ErrSessionMetadata
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, ErrSessionMetadata
	}
	return items, nil
}

type todoFields struct {
	Content    jsonvalue.Value `json:"content"`
	Status     jsonvalue.Value `json:"status"`
	ActiveForm jsonvalue.Value `json:"activeForm"`
}

// Decode each small object separately while the outer typed list stays unbounded.
// Exact property names avoid encoding/json's case-insensitive field matching.
func (fields *todoFields) UnmarshalJSON(data []byte) error {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	if value.Kind() != jsonvalue.Object {
		return ErrSessionMetadata
	}
	for _, key := range value.Keys() {
		field, _ := value.Lookup(key)
		switch key {
		case "content":
			fields.Content = field
		case "status":
			fields.Status = field
		case "activeForm":
			fields.ActiveForm = field
		default:
			return ErrSessionMetadata
		}
	}
	return nil
}

func decodeTodos(text string) ([]protocol.TodoItem, error) {
	items, err := decodeMetadataArray[todoFields](text)
	if err != nil {
		return nil, errors.Join(ErrSessionMetadata, err)
	}
	result := make([]protocol.TodoItem, 0, len(items))
	for _, fields := range items {
		content, contentOK := fields.Content.Text()
		status, statusOK := fields.Status.Text()
		activeForm, activeOK := fields.ActiveForm.Text()
		if !contentOK || !statusOK || !activeOK {
			return nil, ErrSessionMetadata
		}
		result = append(result, protocol.TodoItem{Content: content, Status: protocol.TodoStatus(status), ActiveForm: activeForm})
	}
	return result, nil
}

func decodeSteering(text string) ([]string, error) {
	items, err := decodeMetadataArray[jsonvalue.Value](text)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.Text()
		if !ok {
			return nil, ErrSessionMetadata
		}
		result = append(result, text)
	}
	return result, nil
}

// sqliteTruth lowers the driver's finite scalar types using Python bool(row).
// A textual "false" is nonempty historical metadata, not an authority decision.
type sqliteTruth bool

func (v *sqliteTruth) Scan(raw any) error {
	switch value := raw.(type) {
	case nil:
		*v = false
	case int64:
		*v = sqliteTruth(value != 0)
	case float64:
		*v = sqliteTruth(value != 0)
	case string:
		*v = sqliteTruth(value != "")
	case []byte:
		*v = sqliteTruth(len(value) != 0)
	default:
		return ErrSessionMetadata
	}
	return nil
}
