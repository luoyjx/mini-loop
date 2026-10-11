package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/provider"
)

func TestProviderRunErrorUsesOnlyObservedEncodingDetail(t *testing.T) {
	for _, err := range []error{errors.New("plain failure"), &provider.Failure{Kind: provider.FailureProtocol, Message: "cannot encode provider request"}} {
		if got := providerRunError(err); got != boundedError(err) {
			t.Fatal("generic detail changed", got)
		}
	}
	failure := &provider.Failure{Kind: provider.FailureNonfiniteRequest, Message: "Out of range float values are not JSON compliant"}
	wrapped := fmt.Errorf("outer: %w", failure)
	if got := providerRunError(wrapped); got != "ValueError: Out of range float values are not JSON compliant" {
		t.Fatal(got)
	}
	failure.Message = strings.Repeat("界", 600)
	if got := providerRunError(failure); len([]rune(got)) != 500 {
		t.Fatal("diagnostic bound changed")
	}
}
