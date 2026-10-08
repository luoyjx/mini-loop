package protocol

import (
	"encoding/json"
	"errors"
	"math/big"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

const ToolReadTokenArtifact ToolName = "read_token_artifact"

type ReadTokenArtifactInput struct {
	RawRef string   `json:"raw_ref"`
	Offset *big.Int `json:"offset,omitempty"`
	Limit  *big.Int `json:"limit,omitempty"`
}

func copyArtifactInteger(v *big.Int) *big.Int {
	if v == nil {
		return nil
	}
	return new(big.Int).Set(v)
}
func ReadTokenArtifactToolInput(v ReadTokenArtifactInput) ToolInput {
	v.Offset = copyArtifactInteger(v.Offset)
	v.Limit = copyArtifactInteger(v.Limit)
	return ToolInput{name: ToolReadTokenArtifact, readTokenArtifact: v}
}
func (input ToolInput) ReadTokenArtifact() (ReadTokenArtifactInput, bool) {
	v := input.readTokenArtifact
	v.Offset = copyArtifactInteger(v.Offset)
	v.Limit = copyArtifactInteger(v.Limit)
	return v, input.name == ToolReadTokenArtifact
}

// Explicit null stays distinct from an absent default, both for execution and replay.
func (input ToolInput) TokenArtifactNullPagination() (offset, limit bool) {
	return input.name == ToolReadTokenArtifact && bool(input.nulls.Offset), input.name == ToolReadTokenArtifact && bool(input.nulls.Limit)
}
func decodeTokenArtifact(data []byte) (ToolInput, error) {
	var wire struct {
		RawRef        *string `json:"raw_ref"`
		Offset, Limit *big.Int
	}
	if err := decodeToolObject(data, &wire); err != nil {
		return ToolInput{}, err
	}
	if wire.RawRef == nil {
		return ToolInput{}, errors.New("read_token_artifact requires raw_ref")
	}
	return ReadTokenArtifactToolInput(ReadTokenArtifactInput{*wire.RawRef, wire.Offset, wire.Limit}), nil
}

type artifactInputProjection struct {
	Limit  *wireOptional[big.Int] `json:"limit,omitempty"`
	Offset *wireOptional[big.Int] `json:"offset,omitempty"`
	RawRef string                 `json:"raw_ref"`
}

func (input ToolInput) artifactProjection() artifactInputProjection {
	v := input.readTokenArtifact
	return artifactInputProjection{optionalWire(v.Limit, input.nulls.Limit), optionalWire(v.Offset, input.nulls.Offset), v.RawRef}
}
func (input ToolInput) marshalTokenArtifact() ([]byte, error) {
	return json.Marshal(input.artifactProjection())
}
func ReadTokenArtifactSchema() ToolSchema {
	zero, one, maximum := 0, 1, 50000
	offsetDefault, limitDefault := jsonvalue.IntegerValue(0), jsonvalue.IntegerValue(50000)
	properties := SchemaProperties{
		"raw_ref": {Type: SchemaString, Description: "Opaque raw_ref emitted with a reduced tool result."},
		"offset":  {Type: SchemaInteger, Minimum: &zero, Default: &offsetDefault, Description: "Character offset for bounded recovery."},
		"limit":   {Type: SchemaInteger, Minimum: &one, Maximum: &maximum, Default: &limitDefault, Description: "Requested characters; the complete rendered page is hard-capped at 50000 UTF-8 bytes."},
	}
	return ToolSchema{Name: ToolReadTokenArtifact, Description: "Read one bounded page of an already-masked tool result by session-scoped raw_ref.", InputSchema: InputSchema{Type: SchemaObject, Properties: &properties, Required: []string{"raw_ref"}}}
}
