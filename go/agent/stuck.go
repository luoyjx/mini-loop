package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const StuckWindow = 20
const DefaultStuckAdvice = "Stop repeating this call. Either change your approach, use a different tool, or explain to the user why you cannot proceed."

type StepHash string

func shortStepHash(encoded string) StepHash {
	sum := sha256.Sum256([]byte(encoded))
	return StepHash(hex.EncodeToString(sum[:])[:16])
}
func InputStepHash(input protocol.ToolInput) (StepHash, error) {
	encoded, err := input.SortedPythonJSON()
	if err != nil {
		return "", err
	}
	return shortStepHash(encoded), nil
}
func OutputStepHash(output string) (StepHash, error) {
	encoded, err := protocol.PythonJSON(output, false, false)
	if err != nil {
		return "", err
	}
	return shortStepHash(encoded), nil
}

type ToolStep struct {
	Name       protocol.ToolName `json:"name"`
	InputHash  StepHash          `json:"input_hash"`
	OutputHash StepHash          `json:"output_hash"`
	Failed     bool              `json:"failed"`
	Denied     bool              `json:"denied"`
}

func (step ToolStep) Unproductive() bool { return step.Failed || step.Denied }
func (step ToolStep) SameCall(other ToolStep) bool {
	return step.Name == other.Name && step.InputHash == other.InputHash
}
func (step ToolStep) SameOutcome(other ToolStep) bool {
	return step.SameCall(other) && step.OutputHash == other.OutputHash
}

type StuckPattern string

const (
	StuckRepeatResult     StuckPattern = "repeat_action_result"
	StuckRepeatError      StuckPattern = "repeat_action_error"
	StuckUnproductiveTool StuckPattern = "unproductive_tool"
	StuckAlternating      StuckPattern = "alternating"
	StuckMonologue        StuckPattern = "monologue"
)

type StuckSignal struct {
	Pattern StuckPattern       `json:"pattern"`
	Detail  string             `json:"detail"`
	Tool    *protocol.ToolName `json:"tool_name"`
	Advice  string             `json:"advice"`
}

func (signal StuckSignal) clone() StuckSignal {
	if signal.Tool != nil {
		name := *signal.Tool
		signal.Tool = &name
	}
	return signal
}
func (signal StuckSignal) Reminder() string {
	return fmt.Sprintf("<stuck pattern=\"%s\">%s %s</stuck>", signal.Pattern, signal.Detail, signal.Advice)
}

type StuckThresholds struct {
	RepeatActionResult int `json:"repeat_action_result"`
	RepeatActionError  int `json:"repeat_action_error"`
	Alternating        int `json:"alternating"`
	Monologue          int `json:"monologue"`
	UnproductiveTool   int `json:"unproductive_tool"`
	MaxNudges          int `json:"max_nudges"`
}

func DefaultStuckThresholds() StuckThresholds { return StuckThresholds{4, 3, 6, 3, 5, 1} }

type StuckState struct {
	Steps              []ToolStep
	RoundsWithoutTools int
}
type StuckDetector interface {
	Inspect(StuckState) (*StuckSignal, error)
	MaxNudges() int
}
type NullStuckDetector struct{}

func (NullStuckDetector) Inspect(StuckState) (*StuckSignal, error) { return nil, nil }
func (NullStuckDetector) MaxNudges() int                           { return 0 }

type DefaultStuckDetector struct{ thresholds StuckThresholds }

func NewDefaultStuckDetector() DefaultStuckDetector {
	detector, _ := NewStuckDetector(DefaultStuckThresholds())
	return detector
}
func NewStuckDetector(thresholds StuckThresholds) (DefaultStuckDetector, error) {
	if thresholds.RepeatActionResult < 1 || thresholds.RepeatActionError < 1 || thresholds.Monologue < 1 || thresholds.Alternating < 0 || thresholds.UnproductiveTool < 0 || thresholds.MaxNudges < 0 {
		return DefaultStuckDetector{}, errors.New("invalid stuck detection thresholds")
	}
	return DefaultStuckDetector{thresholds}, nil
}
func (detector DefaultStuckDetector) MaxNudges() int {
	if detector.thresholds == (StuckThresholds{}) {
		return 1
	}
	return detector.thresholds.MaxNudges
}
func defaultSignal(pattern StuckPattern, detail string, tool *protocol.ToolName) *StuckSignal {
	return &StuckSignal{pattern, detail, tool, DefaultStuckAdvice}
}
func (detector DefaultStuckDetector) Inspect(state StuckState) (*StuckSignal, error) {
	t := detector.thresholds
	if t == (StuckThresholds{}) {
		t = DefaultStuckThresholds()
	}
	steps := state.Steps
	if state.RoundsWithoutTools >= t.Monologue {
		return defaultSignal(StuckMonologue, fmt.Sprintf("The model produced %d consecutive turns with no tool call, and a stop hook keeps resuming it.", state.RoundsWithoutTools), nil), nil
	}
	if n := t.RepeatActionError; len(steps) >= n {
		window := steps[len(steps)-n:]
		same, bad, denied := true, true, true
		for _, step := range window {
			same = same && window[0].SameCall(step)
			bad = bad && step.Unproductive()
			denied = denied && step.Denied
		}
		if same && bad {
			kind := "failed"
			if denied {
				kind = "denied"
			}
			name := window[0].Name
			return defaultSignal(StuckRepeatError, fmt.Sprintf("The last %d tool calls were all `%s` with identical input and every one of them %s.", n, name, kind), &name), nil
		}
	}
	if n := t.UnproductiveTool; n > 0 && len(steps) >= n {
		bad := make(map[protocol.ToolName]int)
		good := make(map[protocol.ToolName]bool)
		order := make([]protocol.ToolName, 0)
		for _, step := range steps {
			if step.Unproductive() {
				if bad[step.Name] == 0 {
					order = append(order, step.Name)
				}
				bad[step.Name]++
			} else {
				good[step.Name] = true
			}
		}
		for _, name := range order {
			if count := bad[name]; count >= n && !good[name] {
				signal := defaultSignal(StuckUnproductiveTool, fmt.Sprintf("`%s` failed or was denied %d times in the last %d tool calls and never once succeeded, however it was called.", name, count, len(steps)), &name)
				signal.Advice = fmt.Sprintf("`%s` is not going to start working. Do not call it again with different arguments. Either reach the goal another way, or tell the user what is blocking you.", name)
				return signal, nil
			}
		}
	}
	if n := t.RepeatActionResult; len(steps) >= n {
		window := steps[len(steps)-n:]
		same := true
		for _, step := range window {
			same = same && window[0].SameOutcome(step)
		}
		if same {
			name := window[0].Name
			return defaultSignal(StuckRepeatResult, fmt.Sprintf("The last %d tool calls were all `%s` with identical input and identical output.", n, name), &name), nil
		}
	}
	if n := t.Alternating; n >= 4 && len(steps) >= n {
		window := steps[len(steps)-n:]
		same := true
		for i := 0; i < n-2; i++ {
			same = same && window[i].SameOutcome(window[i+2])
		}
		if same && !window[0].SameOutcome(window[1]) {
			name := window[0].Name
			return defaultSignal(StuckAlternating, fmt.Sprintf("The last %d tool calls alternated between `%s` and `%s` with identical inputs and identical outputs each cycle.", n, window[0].Name, window[1].Name), &name), nil
		}
	}
	return nil, nil
}

// StopHook can request another tool-less round. An empty continuation is valid.
// It receives a snapshot and cannot enter the locked session recursively.
type StopContext struct {
	session   *Session
	Authority ToolAuthority
	Messages  []protocol.Message
	LastText  string
}
type StopHook interface {
	Stop(context.Context, StopContext) (*string, error)
}

func (s *Session) stuckState() StuckState {
	return StuckState{append([]ToolStep(nil), s.recentSteps...), s.roundsWithoutTools}
}
func (s *Session) recordToolStep(name protocol.ToolName, outcome ToolOutcome) error {
	output, err := OutputStepHash(outcome.Output)
	if err != nil {
		return err
	}
	if outcome.inputHash == "" {
		return errors.New("tool outcome missing final input step hash")
	}
	s.recentSteps = append(s.recentSteps, ToolStep{name, outcome.inputHash, output, outcome.Failed, outcome.Denied})
	if len(s.recentSteps) > StuckWindow {
		s.recentSteps = append([]ToolStep(nil), s.recentSteps[len(s.recentSteps)-StuckWindow:]...)
	}
	s.publishLive()
	return nil
}
func (s *Session) nudgeOrHalt(signal StuckSignal) (bool, string) {
	halted := s.stuckNudges >= s.stuckDetector.MaxNudges()
	s.events.append(SessionEvent{kind: EventStuck, stuck: StuckEvent{signal.clone(), halted, s.stuckNudges}})
	if halted {
		return false, "[stopped: " + signal.Detail + "]"
	}
	s.stuckNudges++
	s.recentSteps = nil
	s.roundsWithoutTools = 0
	s.publishLive()
	return true, ""
}
