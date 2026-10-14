package provider

import (
	"errors"
	"fmt"

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
	if detail, ok := e.DispatchErrorDetail(); ok {
		return detail, true
	}
	if e.Kind != FailureNonfiniteRequest {
		return "", false
	}
	return string(e.Class()) + ": " + e.Message, true
}

// DispatchErrorDetail is bounded diagnostic text from an explicitly observed
// retained-batch request. Raw HTTP values are never retained in the failure.
func (e *Failure) DispatchErrorDetail() (string, bool) {
	return e.dispatchDetail, e.dispatchDetail != ""
}

func (c *Client) observedDispatchStatusDetail(failure *Failure, data []byte) string {
	// The captured SDK boundary is a JSON 400 response. Other HTTP failures
	// retain their existing status, retry and diagnostic behavior.
	if failure.Status != 400 {
		return ""
	}
	value, err := jsonvalue.Decode(string(data))
	if err != nil || value.Kind() != jsonvalue.Object {
		return ""
	}
	value = value.MapStrings(c.redactCredentials)
	detail := []rune(c.clean(fmt.Sprintf("%s: Error code: %d - %s", failure.Class(), failure.Status, value.PythonString()), 500))
	// clean's ordinary diagnostic suffix is outside its text budget; the
	// observed Source event/error boundary instead has a strict total limit.
	return string(detail[:min(len(detail), 500)])
}
