package protocol

import (
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"math"
	"strconv"
)

// BackgroundTimeoutObservation retains model data without making it executable.
type BackgroundTimeoutObservation struct{ observed jsonvalue.Value }
type BackgroundTimeoutKind uint8

const (
	BackgroundTimeoutDefault BackgroundTimeoutKind = iota
	BackgroundTimeoutNumeric
	BackgroundTimeoutUnbounded
	BackgroundTimeoutImmediate
	BackgroundTimeoutTextFault
	BackgroundTimeoutArrayFault
	BackgroundTimeoutObjectFault
	BackgroundTimeoutIntegerOverflow
)

// BackgroundTimeoutPlan is a concrete consumer projection, separate from archives.
type BackgroundTimeoutPlan struct {
	Kind    BackgroundTimeoutKind
	Seconds float64
	Label   string
}

func (input ToolInput) BackgroundTimeout() (BackgroundTimeoutPlan, bool) {
	if input.name != ToolBackgroundRun || input.backgroundModel == nil || input.backgroundModel.timeout == nil {
		return BackgroundTimeoutPlan{}, false
	}
	value := input.backgroundModel.timeout.observed
	plan := BackgroundTimeoutPlan{Label: value.PythonString()}
	if !value.Truth() {
		return plan, true
	}
	switch value.Kind() {
	case jsonvalue.Integer:
		text, _ := value.Integer()
		seconds, err := strconv.ParseFloat(text, 64)
		if err != nil && len(text) > 0 && text[0] == '-' {
			plan.Kind = BackgroundTimeoutNumeric
			plan.Seconds = math.Inf(-1)
			return plan, true
		}
		if err != nil {
			plan.Kind = BackgroundTimeoutIntegerOverflow
			return plan, true
		}
		plan.Seconds = seconds
	case jsonvalue.Float:
		plan.Seconds, _ = value.Float()
	case jsonvalue.Boolean:
		plan.Seconds = 1
	case jsonvalue.Text:
		plan.Kind = BackgroundTimeoutTextFault
		return plan, true
	case jsonvalue.Array:
		plan.Kind = BackgroundTimeoutArrayFault
		return plan, true
	case jsonvalue.Object:
		plan.Kind = BackgroundTimeoutObjectFault
		return plan, true
	}
	plan.Kind = BackgroundTimeoutNumeric
	if math.IsNaN(plan.Seconds) {
		plan.Kind = BackgroundTimeoutImmediate
		plan.Seconds = 0
	} else if math.IsInf(plan.Seconds, 1) {
		plan.Kind = BackgroundTimeoutUnbounded
	}
	return plan, true
}

func (observation BackgroundTimeoutObservation) integer() *int {
	text, ok := observation.observed.Integer()
	if !ok {
		return nil
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		return nil
	}
	return &value
}
