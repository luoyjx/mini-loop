package httpapi

import "github.com/luoyjx/mini-loop/go/internal/jsonsyntax"

type requestJSONFailureKind = jsonsyntax.Code

const (
	jsonExpectedValue        = jsonsyntax.ExpectedValue
	jsonExpectedProperty     = jsonsyntax.ExpectedProperty
	jsonExpectedColon        = jsonsyntax.ExpectedColon
	jsonExpectedComma        = jsonsyntax.ExpectedComma
	jsonExtraData            = jsonsyntax.ExtraData
	jsonUnterminatedString   = jsonsyntax.UnterminatedString
	jsonInvalidEscape        = jsonsyntax.InvalidEscape
	jsonInvalidUnicodeEscape = jsonsyntax.InvalidUnicodeEscape
	jsonInvalidControl       = jsonsyntax.InvalidControl
	jsonNestingLimit         = jsonsyntax.NestingLimit
	jsonIntegerLimit         = jsonsyntax.IntegerLimit
)

type requestJSONFailure struct {
	kind     requestJSONFailureKind
	position int
}

func (e requestJSONFailure) message() string {
	// The shared scanner owns diagnostic wording; HTTP retains its wire projection.
	return e.kind.Message()
}

func requestJSONSyntax(raw []byte) *requestJSONFailure {
	failure := jsonsyntax.Scan(raw, jsonsyntax.Limits{Containers: requestMaxContainerDepth, IntegerDigits: requestIntegerDigitLimit})
	if failure == nil {
		return nil
	}
	return &requestJSONFailure{kind: failure.Code(), position: failure.Position()}
}
