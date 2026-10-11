// Package benchmark measures completed task effects and motion. Statistics confer
// no authority to launch an arm or admit a draft into the judged workload.
package benchmark

type Dimension string

const (
	Duration             Dimension = "duration_ms"
	ContextTokens        Dimension = "context_tokens_estimate"
	Rounds               Dimension = "rounds"
	ToolCalls            Dimension = "tool_calls"
	ToolErrors           Dimension = "tool_errors"
	RepeatedReads        Dimension = "repeated_reads"
	DimensionWarnPercent           = 25.0
)

var dimensions = [...]Dimension{Duration, ContextTokens, Rounds, ToolCalls, ToolErrors, RepeatedReads}

func Dimensions() []Dimension { return append([]Dimension{}, dimensions[:]...) }

type Measurements struct {
	DurationMilliseconds  *Number `json:"duration_ms,omitempty"`
	ContextTokensEstimate *Number `json:"context_tokens_estimate,omitempty"`
	Rounds                *Number `json:"rounds,omitempty"`
	ToolCalls             *Number `json:"tool_calls,omitempty"`
	ToolErrors            *Number `json:"tool_errors,omitempty"`
	RepeatedReads         *Number `json:"repeated_reads,omitempty"`
}

type TaskResult struct {
	Arm    string  `json:"arm"`
	Task   string  `json:"task"`
	Passed bool    `json:"passed"`
	Error  *string `json:"error"`
	Measurements
}

type AggregateResult struct {
	TaskResult
	PassRate Number `json:"pass_rate"`
	Repeats  int    `json:"repeats"`
}

type Verdict string

const (
	Regression  Verdict = "regression"
	Improvement Verdict = "improvement"
	NotWorse    Verdict = "not_worse"
)

type DimensionChange struct {
	Baseline     Number  `json:"baseline"`
	Candidate    Number  `json:"candidate"`
	DeltaPercent *Number `json:"delta_pct"`
}
type Comparison struct {
	Tasks             int                           `json:"tasks"`
	BaselinePassed    int                           `json:"baseline_passed"`
	CandidatePassed   int                           `json:"candidate_passed"`
	Wins              []string                      `json:"wins"`
	Regressions       []string                      `json:"regressions"`
	Dimensions        map[Dimension]DimensionChange `json:"dimensions"`
	DimensionWarnings []string                      `json:"dimension_warnings"`
	Verdict           Verdict                       `json:"verdict"`
}

func (m *Measurements) measurement(d Dimension) **Number {
	switch d {
	case Duration:
		return &m.DurationMilliseconds
	case ContextTokens:
		return &m.ContextTokensEstimate
	case Rounds:
		return &m.Rounds
	case ToolCalls:
		return &m.ToolCalls
	case ToolErrors:
		return &m.ToolErrors
	case RepeatedReads:
		return &m.RepeatedReads
	default:
		panic("unknown benchmark dimension")
	}
}
