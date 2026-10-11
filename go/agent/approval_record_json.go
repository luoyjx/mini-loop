package agent

import (
	"encoding/json"
	"errors"
)

func (record ApprovalRecord) MarshalJSON() ([]byte, error) {
	if record.ToolUseIDNull && record.ToolUseID != "" {
		return nil, errors.New("NULL approval tool-use ID also contains a value")
	}
	type fields ApprovalRecord
	var toolUse *string
	if !record.ToolUseIDNull {
		value := record.ToolUseID
		toolUse = &value
	}
	return json.Marshal(struct {
		*fields
		ToolUse *string `json:"tool_use_id"`
	}{fields: (*fields)(&record), ToolUse: toolUse})
}

func (record *ApprovalRecord) UnmarshalJSON(data []byte) error {
	type fields ApprovalRecord
	var next ApprovalRecord
	wire := struct {
		*fields
		ToolUse *string `json:"tool_use_id"`
	}{fields: (*fields)(&next)}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	next.ToolUseIDNull = wire.ToolUse == nil
	if wire.ToolUse != nil {
		next.ToolUseID = *wire.ToolUse
	}
	*record = next
	return nil
}
