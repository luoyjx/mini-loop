package sessionquery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// RequestSequence is an immutable Python integer projection, independent of a
// live runtime's unsigned event cursor. Its zero value denotes sequence zero.
type RequestSequence struct{ value ArchiveValue }

func NewRequestSequence(sequence int64) RequestSequence {
	return RequestSequence{jsonvalue.IntegerValue(sequence)}
}
func ParseRequestSequence(decimal string) (RequestSequence, error) {
	value, err := jsonvalue.Decode(decimal)
	if err != nil {
		return RequestSequence{}, err
	}
	if value.Kind() != jsonvalue.Integer {
		return RequestSequence{}, errors.New("request sequence must be an integer")
	}
	return RequestSequence{value}, nil
}
func (sequence RequestSequence) number() ArchiveValue {
	if sequence.value.Kind() == jsonvalue.Null {
		return jsonvalue.IntegerValue(0)
	}
	return sequence.value
}

// ReconstructionReader returns physical event order and the exact selected
// historical epoch. ArchiveValue selectors retain Source's old scalar fields.
type ReconstructionReader interface {
	LoadRequestEvents(context.Context, SessionID) ([]RequestEvent, error)
	LoadRequestMessages(context.Context, SessionID, ArchiveValue) ([]ArchiveValue, error)
}

type CapabilityProjection struct {
	PermissionMode     ArchiveValue `json:"permission_mode"`
	Sandbox            ArchiveValue `json:"sandbox"`
	SandboxConfined    ArchiveValue `json:"sandbox_confined"`
	CatalogFingerprint ArchiveValue `json:"catalog_fingerprint"`
}
type RequestProjection struct {
	Messages   []ArchiveValue        `json:"messages"`
	System     ArchiveValue          `json:"system"`
	Tools      ArchiveValue          `json:"tools"`
	Capability *CapabilityProjection `json:"capability"`
	Model      ArchiveValue          `json:"model"`
	MaxTokens  ArchiveValue          `json:"max_tokens"`
}

func (projection RequestProjection) clone() RequestProjection {
	projection.Messages = append([]ArchiveValue{}, projection.Messages...)
	if projection.Capability != nil {
		copy := *projection.Capability
		projection.Capability = &copy
	}
	return projection
}

type ReconstructionKind uint8

const (
	ReconstructionInvalid ReconstructionKind = iota
	ReconstructionFound
	ReconstructionMissingStart
	ReconstructionLegacyEpoch
)

type Reconstruction struct {
	kind    ReconstructionKind
	request RequestProjection
	notice  string
}

func (result Reconstruction) Kind() ReconstructionKind { return result.kind }
func (result Reconstruction) Request() (RequestProjection, bool) {
	return result.request.clone(), result.kind == ReconstructionFound
}
func (result Reconstruction) MarshalJSON() ([]byte, error) {
	if result.kind == ReconstructionFound {
		return json.Marshal(result.request)
	}
	if result.kind == ReconstructionMissingStart || result.kind == ReconstructionLegacyEpoch {
		return json.Marshal(struct {
			Error string `json:"error"`
		}{result.notice})
	}
	return nil, errors.New("invalid reconstruction result")
}

type ReconstructionPhase string

const (
	CountConversion    ReconstructionPhase = "message_count"
	SequenceComparison ReconstructionPhase = "event_sequence"
)

type PythonErrorClass string

const (
	PythonTypeError     PythonErrorClass = "TypeError"
	PythonValueError    PythonErrorClass = "ValueError"
	PythonOverflowError PythonErrorClass = "OverflowError"
)

// ReconstructionFault distinguishes Source scalar failures from reader faults.
// It contains no archive payload or execution permission.
type ReconstructionFault struct {
	Phase ReconstructionPhase
	Class PythonErrorClass
}

func (fault ReconstructionFault) Error() string {
	return string(fault.Phase) + ": " + string(fault.Class)
}

func Reconstruct(ctx context.Context, reader ReconstructionReader, id SessionID, sequence RequestSequence) (Reconstruction, error) {
	if err := ctx.Err(); err != nil {
		return Reconstruction{}, err
	}
	if reader == nil {
		return Reconstruction{}, errors.New("reconstruction reader is required")
	}
	events, err := reader.LoadRequestEvents(ctx, id)
	if err != nil {
		return Reconstruction{}, err
	}
	if err := ctx.Err(); err != nil {
		return Reconstruction{}, err
	}
	target := sequence.number()
	var start *requestStart
	for index := range events {
		if err := ctx.Err(); err != nil {
			return Reconstruction{}, err
		}
		event := &events[index]
		if event.kind == RequestModelStart && event.sequence.PythonEqual(target) {
			start = &event.start
			break
		}
	}
	if start == nil {
		return Reconstruction{kind: ReconstructionMissingStart, notice: fmt.Sprintf("no model_start at seq %s", target.PythonString())}, nil
	}
	if start.epoch.Kind() == jsonvalue.Null {
		return Reconstruction{kind: ReconstructionLegacyEpoch, notice: "this model_start predates the epoch stamp"}, nil
	}
	count := start.count
	if !count.Truth() {
		count = jsonvalue.IntegerValue(0)
	}
	limit, err := count.PythonInteger()
	if err != nil {
		class := PythonTypeError
		if count.Kind() == jsonvalue.Text {
			class = PythonValueError
		}
		if number, ok := count.Float(); ok {
			class = PythonValueError
			if math.IsInf(number, 0) {
				class = PythonOverflowError
			}
		}
		return Reconstruction{}, ReconstructionFault{CountConversion, class}
	}
	projection := RequestProjection{Model: start.model, MaxTokens: start.maxTokens}
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return Reconstruction{}, err
		}
		position := event.sequence
		if !event.sequencePresent {
			position = jsonvalue.IntegerValue(0)
		}
		comparison, ordered, err := position.PythonNumberCompare(target)
		if err != nil {
			return Reconstruction{}, ReconstructionFault{SequenceComparison, PythonTypeError}
		}
		if ordered && comparison >= 0 {
			break
		}
		switch event.kind {
		case RequestToolCatalog:
			if event.catalog.fingerprint.PythonEqual(start.tools) {
				projection.Tools = event.catalog.schemas
			}
		case RequestSystemPrompt:
			if event.system.hash.PythonEqual(start.system) {
				projection.System = event.system.text
			}
		case RequestCapabilityPlan:
			if event.capability.fingerprint.PythonEqual(start.capability) {
				copy := event.capability.projection
				projection.Capability = &copy
			}
		}
	}
	messages, err := reader.LoadRequestMessages(ctx, id, start.epoch)
	if err != nil {
		return Reconstruction{}, err
	}
	if err := ctx.Err(); err != nil {
		return Reconstruction{}, err
	}
	length := big.NewInt(int64(len(messages)))
	end := new(big.Int).Set(limit)
	if end.Sign() < 0 {
		end.Add(end, length)
	}
	if end.Sign() < 0 {
		end.SetInt64(0)
	}
	if end.Cmp(length) > 0 {
		end.Set(length)
	}
	projection.Messages = append([]ArchiveValue{}, messages[:int(end.Int64())]...)
	return Reconstruction{kind: ReconstructionFound, request: projection}, nil
}
