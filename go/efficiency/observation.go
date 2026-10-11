package efficiency

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ObservationMetadata preserves ordered, possibly repeated keys. Values are a
// closed immutable JSON sum, including command status scalars and nested values.
// Arbitrary Python objects are outside this typed construction boundary.
type ObservationMetadata struct {
	Key   string
	Value jsonvalue.Value
}

func (entry ObservationMetadata) MarshalJSON() ([]byte, error) {
	return jsonvalue.ArrayValue([]jsonvalue.Value{jsonvalue.TextValue(entry.Key), entry.Value}).MarshalJSON()
}
func (entry *ObservationMetadata) UnmarshalJSON(data []byte) error {
	var value jsonvalue.Value
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	pair, ok := value.Array()
	if !ok || len(pair) != 2 {
		return errors.New("observation metadata must be a key/value pair")
	}
	key, ok := pair[0].Text()
	if !ok {
		return errors.New("observation metadata key must be text")
	}
	*entry = ObservationMetadata{key, pair[1]}
	return nil
}

type ObservationConfig struct {
	Content     string                `json:"content"`
	ContentType string                `json:"content_type"`
	ReducedBy   []string              `json:"reduced_by"`
	RawRef      *string               `json:"raw_ref"`
	RawDigest   *string               `json:"raw_digest"`
	Metadata    []ObservationMetadata `json:"metadata"`
}

func DefaultObservation(content string) ObservationConfig {
	return ObservationConfig{Content: content, ContentType: "text/plain", ReducedBy: []string{}, Metadata: []ObservationMetadata{}}
}
func (c ObservationConfig) clone() ObservationConfig {
	c.ReducedBy = append([]string{}, c.ReducedBy...)
	c.RawRef = copyText(c.RawRef)
	c.RawDigest = copyText(c.RawDigest)
	c.Metadata = append([]ObservationMetadata{}, c.Metadata...)
	return c
}

// MaskedObservation asserts that application-level redaction already ran. Its
// immutable snapshot is suitable for trusted reducers; it does not perform masking.
type MaskedObservation struct {
	config ObservationConfig
	valid  bool
}

func NewObservation(c ObservationConfig) (MaskedObservation, error) {
	if c.ContentType == "" || !strings.Contains(c.ContentType, "/") {
		return MaskedObservation{}, errors.New("content_type must be MIME-like")
	}
	c = c.clone()
	dedup := make([]string, 0, len(c.ReducedBy))
	seen := make(map[string]bool)
	for _, id := range c.ReducedBy {
		if !seen[id] {
			seen[id] = true
			dedup = append(dedup, id)
		}
	}
	c.ReducedBy = dedup
	if (c.RawRef == nil) != (c.RawDigest == nil) {
		return MaskedObservation{}, errors.New("raw_ref and raw_digest must be set together")
	}
	// Source validates pairing here, not the ref/digest patterns. Receipts and the
	// raw-store recovery boundary carry their own stricter validations.
	return MaskedObservation{c, true}, nil
}
func (o MaskedObservation) Snapshot() (ObservationConfig, error) {
	if !o.valid {
		return ObservationConfig{}, errors.New("masked observation is not initialized")
	}
	return o.config.clone(), nil
}
func (o MaskedObservation) MarshalJSON() ([]byte, error) {
	c, err := o.Snapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

type ReductionConfig struct {
	Content  string   `json:"content"`
	Warnings []string `json:"warnings"`
}
type ObservationReduction struct {
	config ReductionConfig
	valid  bool
}

func NewReduction(c ReductionConfig) (ObservationReduction, error) {
	if err := validateWarnings(c.Warnings); err != nil {
		return ObservationReduction{}, err
	}
	c.Warnings = append([]string{}, c.Warnings...)
	return ObservationReduction{c, true}, nil
}
func (r ObservationReduction) Snapshot() (ReductionConfig, error) {
	if !r.valid {
		return ReductionConfig{}, errors.New("observation reduction is not initialized")
	}
	c := r.config
	c.Warnings = append([]string{}, c.Warnings...)
	return c, nil
}
func (r ObservationReduction) MarshalJSON() ([]byte, error) {
	c, err := r.Snapshot()
	if err != nil {
		return nil, err
	}
	return json.Marshal(c)
}

type ReductionOptions struct {
	Query        *string
	BudgetTokens *big.Int
}

// ObservationReducer is a concrete-result SPI; no generic map or dynamic result
// object can cross it. Invocation admission/lifecycle belong to the runtime.
type ObservationReducer interface {
	Descriptor() Descriptor
	Reduce(context.Context, MaskedObservation, ReductionOptions) (ObservationReduction, error)
}
