package efficiency

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// ReceiptConfig carries metrics/references only, never observation or request text.
type ReceiptConfig struct {
	ComponentID          string    `json:"component_id"`
	ComponentVersion     string    `json:"component_version"`
	Stage                Stage     `json:"stage"`
	Mode                 Mode      `json:"mode"`
	Status               Status    `json:"status"`
	Reason               string    `json:"reason"`
	RawBytes             *big.Int  `json:"raw_bytes"`
	ProjectedBytes       *big.Int  `json:"projected_bytes"`
	TokensBeforeEstimate *big.Int  `json:"tokens_before_estimate"`
	TokensAfterEstimate  *big.Int  `json:"tokens_after_estimate"`
	InputDigest          string    `json:"input_digest"`
	OutputDigest         string    `json:"output_digest"`
	Lossiness            Lossiness `json:"lossiness"`
	Deterministic        bool      `json:"deterministic"`
	RawRef               *string   `json:"raw_ref"`
	RawDigest            *string   `json:"raw_digest"`
	Warnings             []string  `json:"warnings"`
	ElapsedMS            float64   `json:"elapsed_ms"`
}

type Receipt struct {
	config ReceiptConfig
	valid  bool
}
type ReceiptProjection struct {
	ReceiptConfig
	Changed bool `json:"changed"`
}

func copyText(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
func (config ReceiptConfig) clone() ReceiptConfig {
	config.RawBytes = copyInteger(config.RawBytes)
	config.ProjectedBytes = copyInteger(config.ProjectedBytes)
	config.TokensBeforeEstimate = copyInteger(config.TokensBeforeEstimate)
	config.TokensAfterEstimate = copyInteger(config.TokensAfterEstimate)
	config.RawRef, config.RawDigest = copyText(config.RawRef), copyText(config.RawDigest)
	config.Warnings = append([]string{}, config.Warnings...)
	return config
}

func validateWarnings(warnings []string) error {
	if len(warnings) > 32 {
		return errors.New("warnings must contain at most 32 entries")
	}
	for _, warning := range warnings {
		if jsonvalue.RuneCount(warning) > 256 {
			return errors.New("warning entries must be at most 256 characters")
		}
	}
	for _, warning := range warnings {
		if !warningCode.MatchString(warning) {
			return errors.New("warnings must contain bounded machine-readable codes")
		}
	}
	return nil
}

func NewReceipt(config ReceiptConfig) (Receipt, error) {
	if !componentID.MatchString(config.ComponentID) {
		return Receipt{}, errors.New("receipt component id is invalid")
	}
	if !componentVersion.MatchString(config.ComponentVersion) {
		return Receipt{}, errors.New("receipt component version is invalid")
	}
	if !receiptReason.MatchString(config.Reason) {
		return Receipt{}, errors.New("receipt reason must be a bounded machine-readable code")
	}
	if err := config.Stage.Validate(); err != nil {
		return Receipt{}, err
	}
	if err := config.Mode.Validate(); err != nil {
		return Receipt{}, err
	}
	if err := config.Status.Validate(); err != nil {
		return Receipt{}, err
	}
	if err := config.Lossiness.Validate(); err != nil {
		return Receipt{}, err
	}
	if err := validateWarnings(config.Warnings); err != nil {
		return Receipt{}, err
	}
	for _, metric := range []struct {
		name  string
		value *big.Int
	}{
		{"raw_bytes", config.RawBytes}, {"projected_bytes", config.ProjectedBytes},
		{"tokens_before_estimate", config.TokensBeforeEstimate}, {"tokens_after_estimate", config.TokensAfterEstimate},
	} {
		if metric.value == nil {
			return Receipt{}, fmt.Errorf("%s must be an integer", metric.name)
		}
		if metric.value.Sign() < 0 {
			return Receipt{}, fmt.Errorf("%s cannot be negative", metric.name)
		}
	}
	if config.ElapsedMS < 0 || math.IsInf(config.ElapsedMS, 0) || math.IsNaN(config.ElapsedMS) {
		return Receipt{}, errors.New("elapsed_ms must be finite and non-negative")
	}
	if !sha256Digest.MatchString(config.InputDigest) {
		return Receipt{}, errors.New("input_digest must be a sha256 digest")
	}
	if !sha256Digest.MatchString(config.OutputDigest) {
		return Receipt{}, errors.New("output_digest must be a sha256 digest")
	}
	if (config.RawRef == nil) != (config.RawDigest == nil) {
		return Receipt{}, errors.New("raw_ref and raw_digest must be set together")
	}
	if config.RawRef != nil && !rawReference.MatchString(*config.RawRef) {
		return Receipt{}, errors.New("raw_ref is invalid")
	}
	if config.RawDigest != nil && !sha256Digest.MatchString(*config.RawDigest) {
		return Receipt{}, errors.New("raw_digest must be a sha256 digest")
	}
	return Receipt{config: config.clone(), valid: true}, nil
}

func (receipt Receipt) Changed() bool {
	return receipt.config.InputDigest != receipt.config.OutputDigest
}
func (receipt Receipt) Projection() (ReceiptProjection, error) {
	if !receipt.valid {
		return ReceiptProjection{}, errors.New("uninitialized optimization receipt")
	}
	return ReceiptProjection{ReceiptConfig: receipt.config.clone(), Changed: receipt.Changed()}, nil
}
func (receipt Receipt) MarshalJSON() ([]byte, error) {
	projection, err := receipt.Projection()
	if err != nil {
		return nil, err
	}
	return json.Marshal(projection)
}
