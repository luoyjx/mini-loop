// Package efficiency defines the typed token-efficiency component contracts.
// Runtime admission and observation/request/response consumers are separate slices.
package efficiency

import (
	"fmt"
	"regexp"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type Stage string

const (
	Observation    Stage = "observation"
	RequestContext Stage = "request_context"
	ResponsePolicy Stage = "response_policy"
)

type Mode string

const (
	Off     Mode = "off"
	Shadow  Mode = "shadow"
	Enforce Mode = "enforce"
)

type Status string

const (
	Applied     Status = "applied"
	Passthrough Status = "passthrough"
	Shadowed    Status = "shadowed"
	Degraded    Status = "degraded"
	Error       Status = "error"
)

type Lossiness string

const (
	Lossless    Lossiness = "lossless"
	Recoverable Lossiness = "recoverable"
	Lossy       Lossiness = "lossy"
)

type CostTier string

const (
	Fast   CostTier = "fast"
	ML     CostTier = "ml"
	Remote CostTier = "remote"
)

func enumError(value, name string) error {
	return fmt.Errorf("%s is not a valid %s", pytext.Repr(value), name)
}
func (stage Stage) Validate() error {
	switch stage {
	case Observation, RequestContext, ResponsePolicy:
		return nil
	}
	return enumError(string(stage), "ComponentStage")
}
func (mode Mode) Validate() error {
	switch mode {
	case Off, Shadow, Enforce:
		return nil
	}
	return enumError(string(mode), "OptimizationMode")
}
func (status Status) Validate() error {
	switch status {
	case Applied, Passthrough, Shadowed, Degraded, Error:
		return nil
	}
	return enumError(string(status), "OptimizationStatus")
}
func (lossiness Lossiness) Validate() error {
	switch lossiness {
	case Lossless, Recoverable, Lossy:
		return nil
	}
	return enumError(string(lossiness), "Lossiness")
}

var (
	componentID      = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\z`)
	componentVersion = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9_.+-]{0,63}\z`)
	warningCode      = regexp.MustCompile(`\A[a-z][a-z0-9_.-]{0,63}(?::[0-9]{1,12})?\z`)
	receiptReason    = regexp.MustCompile(`\A[A-Za-z][A-Za-z0-9_.-]{0,127}(?::[A-Za-z0-9_.-]{1,128})?\z`)
	sha256Digest     = regexp.MustCompile(`\Asha256:[0-9a-f]{64}\z`)
	rawReference     = regexp.MustCompile(`\Araw_[A-Za-z0-9_-]{43}\z`)
)
