package protocol

import (
	"encoding/json"
	"errors"
)

type MemoryType string

const (
	MemoryUser      MemoryType = "user"
	MemoryFeedback  MemoryType = "feedback"
	MemoryProject   MemoryType = "project"
	MemoryReference MemoryType = "reference"
)

func (v MemoryType) Valid() bool {
	return v == MemoryUser || v == MemoryFeedback || v == MemoryProject || v == MemoryReference
}

// Memory inputs contain no owner or filesystem root supplied by the model.
type RememberInput struct {
	Name        string      `json:"name"`
	Content     string      `json:"content"`
	Type        *MemoryType `json:"type,omitempty"`
	Description *string     `json:"description,omitempty"`
}
type RecallInput struct {
	Query *string `json:"query,omitempty"`
}

func cloneRemember(v RememberInput) RememberInput {
	if v.Type != nil {
		t := *v.Type
		v.Type = &t
	}
	if v.Description != nil {
		d := *v.Description
		v.Description = &d
	}
	return v
}
func cloneRecall(v RecallInput) RecallInput {
	if v.Query != nil {
		q := *v.Query
		v.Query = &q
	}
	return v
}
func RememberToolInput(v RememberInput) ToolInput {
	return ToolInput{name: ToolRemember, remember: cloneRemember(v)}
}
func RecallToolInput(v RecallInput) ToolInput {
	return ToolInput{name: ToolRecall, recall: cloneRecall(v)}
}
func (v ToolInput) Remember() (RememberInput, bool) {
	return cloneRemember(v.remember), v.name == ToolRemember
}
func (v ToolInput) Recall() (RecallInput, bool) { return cloneRecall(v.recall), v.name == ToolRecall }

func decodeMemoryInput(name ToolName, data []byte) (ToolInput, error) {
	if name == ToolRecall {
		var wire RecallInput
		if err := decodeToolObject(data, &wire); err != nil {
			return ToolInput{}, err
		}
		result := RecallToolInput(wire)
		if err := json.Unmarshal(data, &result.nulls); err != nil {
			return ToolInput{}, err
		}
		return result, nil
	}
	var wire struct {
		Name        *string     `json:"name"`
		Content     *string     `json:"content"`
		Type        *MemoryType `json:"type"`
		Description *string     `json:"description"`
	}
	if err := decodeToolObject(data, &wire); err != nil {
		return ToolInput{}, err
	}
	if wire.Name == nil || wire.Content == nil {
		return ToolInput{}, errors.New("remember requires name and content")
	}
	result := RememberToolInput(RememberInput{*wire.Name, *wire.Content, wire.Type, wire.Description})
	if err := json.Unmarshal(data, &result.nulls); err != nil {
		return ToolInput{}, err
	}
	return result, result.Validate()
}
func (input ToolInput) marshalMemoryJSON() ([]byte, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	if input.name == ToolRecall {
		return json.Marshal(struct {
			Query *wireOptional[string] `json:"query,omitempty"`
		}{optionalWire(input.recall.Query, input.nulls.MemoryQuery)})
	}
	v := input.remember
	return json.Marshal(struct {
		Name        string                    `json:"name"`
		Content     string                    `json:"content"`
		Type        *wireOptional[MemoryType] `json:"type,omitempty"`
		Description *wireOptional[string]     `json:"description,omitempty"`
	}{v.Name, v.Content, optionalWire(v.Type, input.nulls.MemoryType), optionalWire(v.Description, input.nulls.TaskDescription)})
}
func MemorySchemas() []ToolSchema {
	remember := SchemaProperties{"name": {Type: SchemaString}, "content": {Type: SchemaString}, "description": {Type: SchemaString}, "type": {Type: SchemaString, Enum: []string{"user", "feedback", "project", "reference"}}}
	recall := SchemaProperties{"query": {Type: SchemaString}}
	return []ToolSchema{
		{Name: ToolRemember, Description: "Save a durable fact to the current user's memory (survives across sessions).", InputSchema: InputSchema{Type: SchemaObject, Properties: &remember, Required: []string{"name", "content"}}},
		{Name: ToolRecall, Description: "Recall the current user's memories matching a query (or list all).", InputSchema: InputSchema{Type: SchemaObject, Properties: &recall}},
	}
}
