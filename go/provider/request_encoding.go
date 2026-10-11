package provider

import (
	"errors"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func requestEncodingFailure(err error, fallback string) *Failure {
	if errors.Is(err, jsonvalue.ErrNonfinite) {
		return &Failure{Kind: FailureNonfiniteRequest, Message: "Out of range float values are not JSON compliant"}
	}
	return &Failure{Kind: FailureProtocol, Message: fallback}
}

// ProviderErrorDetail supplies the observed SDK diagnostic only for the captured
// nonfinite request failure. Other failures retain their existing native detail.
func (e *Failure) ProviderErrorDetail() (string, bool) {
	if e.Kind != FailureNonfiniteRequest {
		return "", false
	}
	return string(e.Class()) + ": " + e.Message, true
}
