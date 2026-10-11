package provider

import (
	"github.com/luoyjx/mini-loop/go/protocol"
	"math"
	"strconv"
)

// This is the non-streaming preflight of the pinned Python SDK, not a model's
// output-token capability. The differential fixture checks every listed alias.
const PythonSDKBaseline = "0.107.1"

func DirectTokenCeiling(model string) int {
	if ceiling, ok := protocol.ListedNonStreamingCeiling(model); ok {
		return ceiling
	}
	return 128000 / 6
}

// Outer agent recovery reads only Retry-After seconds, ignoring the SDK's ms
// extension and date parser. Preserve that separate, finite source value.
func retryAfterSeconds(value string) *float64 {
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return nil
	}
	return &seconds
}
