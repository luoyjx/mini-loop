package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type GoalID string
type GoalRevision = protocol.GoalRevision
type GoalPhase string

const (
	GoalActive           GoalPhase = "active"
	GoalPaused           GoalPhase = "paused"
	GoalBlocked          GoalPhase = "blocked"
	GoalComplete         GoalPhase = "complete"
	DefaultGoalMaxRounds           = 10
	GoalMaxRoundsCeiling           = 100
)

type GoalBlockCode string
type GoalBlockReason struct {
	Code    GoalBlockCode `json:"code"`
	Message string        `json:"message"`
}
type GoalRecord struct {
	ID            GoalID           `json:"id"`
	Revision      GoalRevision     `json:"revision"`
	Objective     string           `json:"objective"`
	Phase         GoalPhase        `json:"phase"`
	RoundsStarted int              `json:"rounds_started"`
	MaxRounds     int              `json:"max_rounds"`
	Blocked       *GoalBlockReason `json:"blocked"`
}

func (g GoalRecord) Clone() GoalRecord {
	if g.Blocked != nil {
		b := *g.Blocked
		g.Blocked = &b
	}
	return g
}

var goalCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Python's dollar anchor permits a single trailing newline. Keep the actual
// source acceptance, including trailing/repeated hyphens, rather than its label.
func (c GoalBlockCode) valid() bool {
	return goalCodePattern.MatchString(strings.TrimSuffix(string(c), "\n"))
}
func (g GoalRecord) Validate() error {
	if g.ID == "" || g.Revision < 1 || g.MaxRounds < 1 || g.MaxRounds > GoalMaxRoundsCeiling || g.RoundsStarted < 0 || g.RoundsStarted > g.MaxRounds {
		return errors.New("invalid goal identity/revision/round budget")
	}
	switch g.Phase {
	case GoalActive, GoalPaused, GoalBlocked, GoalComplete:
	default:
		return errors.New("unsupported goal phase")
	}
	if g.Blocked != nil && (!g.Blocked.Code.valid() || strings.TrimFunc(g.Blocked.Message, pytext.Space) == "") {
		return errors.New("invalid goal blocked reason")
	}
	if (g.Phase == GoalBlocked) != (g.Blocked != nil) {
		return errors.New("goal phase and blocked reason disagree")
	}
	return nil
}

type GoalOperation string

const (
	GoalCreateOperation   GoalOperation    = "create"
	GoalRoundOperation    GoalOperation    = "round"
	GoalBlockOperation    GoalOperation    = "block"
	GoalCompleteOperation GoalOperation    = "complete"
	GoalResumeOperation   GoalOperation    = "resume"
	GoalClearOperation    GoalOperation    = "clear"
	EventGoalChange       SessionEventKind = "goal_change"
)

type GoalChangeEvent struct {
	Operation GoalOperation `json:"operation"`
	Goal      *GoalRecord   `json:"goal"`
}

func (e GoalChangeEvent) Clone() GoalChangeEvent {
	if e.Goal != nil {
		g := e.Goal.Clone()
		e.Goal = &g
	}
	return e
}
func (e GoalChangeEvent) Validate() error {
	switch e.Operation {
	case GoalClearOperation:
		return nil
	case GoalCreateOperation, GoalRoundOperation, GoalBlockOperation, GoalCompleteOperation, GoalResumeOperation:
	default:
		return errors.New("unsupported goal operation")
	}
	if e.Goal == nil {
		return errors.New("goal_change requires a snapshot")
	}
	return e.Goal.Validate()
}
func (e SessionEvent) GoalChange() (GoalChangeEvent, bool) {
	return e.goalChange.Clone(), e.kind == EventGoalChange
}

// FoldGoal reconstructs facts only; clear is a tombstone and no event arms it.
func FoldGoal(records []SessionEventRecord) *GoalRecord {
	var goal *GoalRecord
	for _, r := range records {
		if v, ok := r.Event.GoalChange(); ok {
			if v.Operation == GoalClearOperation {
				goal = nil
			} else if v.Goal != nil {
				g := v.Goal.Clone()
				goal = &g
			}
		}
	}
	return goal
}

type GoalStateSnapshot struct {
	Goal  *GoalRecord
	Armed bool
}
type goalState struct {
	mu      sync.Mutex
	current *GoalRecord
	armed   bool
}

func (state *goalState) snapshot() GoalStateSnapshot {
	state.mu.Lock()
	defer state.mu.Unlock()
	v := GoalStateSnapshot{Armed: state.armed}
	if state.current != nil {
		g := state.current.Clone()
		v.Goal = &g
	}
	return v
}
func (s *Session) GoalSnapshot() GoalStateSnapshot        { return s.goals.snapshot() }
func (s *ManagedSession) GoalSnapshot() GoalStateSnapshot { return s.core.GoalSnapshot() }
func (s *ManagedSession) PlanModeActive() bool            { return s.core.PlanModeActive() }
func renderGoal(goal *GoalRecord, armed bool) string {
	if goal == nil {
		return "No current goal."
	}
	activation := "disarmed"
	if armed {
		activation = "armed"
	}
	text := fmt.Sprintf("goal %s rev %d: %s (%s)\nobjective: %s\nrounds: %d/%d", goal.ID, goal.Revision, goal.Phase, activation, goal.Objective, goal.RoundsStarted, goal.MaxRounds)
	if goal.Blocked != nil {
		text += fmt.Sprintf("\nblocked [%s]: %s", goal.Blocked.Code, goal.Blocked.Message)
	}
	return text
}
func goalReferenceError(goal *GoalRecord, revision GoalRevision) string {
	if goal == nil {
		return "Error: no current goal"
	}
	if revision != goal.Revision {
		return fmt.Sprintf("Error: stale revision %d; the goal is at revision %d. Read goal_status and retry with the current revision.", revision, goal.Revision)
	}
	return ""
}

const goalHumanRefusal = "Error: goal_create and goal_resume require explicit human authority; this run is not carrying it"

// State mutations share the runtime's exclusive effect gate. Refusals are source
// textual tool results. A mutex also protects concurrent detached operator reads.
func (h *runtimeHandler) executeGoal(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	s := h.session
	if s == nil {
		return "", errors.New("goal has no bound session")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if input.Name() == protocol.ToolGoalStatus {
		v := s.GoalSnapshot()
		return renderGoal(v.Goal, v.Armed), nil
	}
	if (input.Name() == protocol.ToolGoalCreate || input.Name() == protocol.ToolGoalResume) && authority.RunContext.Authority() != AuthorityExplicitHuman {
		return goalHumanRefusal, nil
	}
	state := &s.goals
	state.mu.Lock()
	goal := state.current
	var next GoalRecord
	var operation GoalOperation
	armed := false
	if input.Name() == protocol.ToolGoalCreate {
		if goal != nil && goal.Phase != GoalComplete {
			state.mu.Unlock()
			return fmt.Sprintf("Error: a goal already exists in phase '%s' (rev %d); complete, block, or clear it first", goal.Phase, goal.Revision), nil
		}
		v, _ := input.CreateGoal()
		cap := DefaultGoalMaxRounds
		if v.MaxRounds != nil && *v.MaxRounds != 0 {
			cap = *v.MaxRounds
		}
		if cap < 1 || cap > GoalMaxRoundsCeiling {
			state.mu.Unlock()
			return fmt.Sprintf("Error: max_rounds must be 1..%d", GoalMaxRoundsCeiling), nil
		}
		id, err := newSpan("goal_", 4)
		if err != nil {
			state.mu.Unlock()
			return "", err
		}
		next = GoalRecord{ID: GoalID(id), Revision: 1, Objective: v.Objective, Phase: GoalActive, MaxRounds: cap}
		operation = GoalCreateOperation
		armed = true
	} else {
		v, _ := input.GoalReference()
		revision := v.Revision
		block, isBlock := input.BlockGoal()
		if isBlock {
			revision = block.Revision
		}
		if refusal := goalReferenceError(goal, revision); refusal != "" {
			state.mu.Unlock()
			return refusal, nil
		}
		next = goal.Clone()
		switch input.Name() {
		case protocol.ToolGoalComplete:
			if goal.Phase == GoalComplete {
				state.mu.Unlock()
				return "Error: the goal is already complete", nil
			}
			next.Phase, next.Blocked = GoalComplete, nil
			operation = GoalCompleteOperation
		case protocol.ToolGoalBlock:
			if !GoalBlockCode(block.Code).valid() {
				state.mu.Unlock()
				return "Error: code must be stable lower-kebab-case (e.g. needs-credentials)", nil
			}
			message := strings.TrimFunc(block.Message, pytext.Space)
			if message == "" {
				state.mu.Unlock()
				return "Error: a blocked goal needs a human-readable message", nil
			}
			next.Phase = GoalBlocked
			next.Blocked = &GoalBlockReason{Code: GoalBlockCode(block.Code), Message: message}
			operation = GoalBlockOperation
		case protocol.ToolGoalResume:
			if goal.Phase == GoalComplete {
				state.mu.Unlock()
				return "Error: a completed goal is finished; create a new one", nil
			}
			if goal.RoundsStarted >= goal.MaxRounds {
				state.mu.Unlock()
				return "Error: the round budget is exhausted; raise max_rounds via a new goal", nil
			}
			next.Phase, next.Blocked = GoalActive, nil
			operation = GoalResumeOperation
			armed = true
		default:
			state.mu.Unlock()
			return "", errors.New("unsupported goal mutation")
		}
		if next.Revision == GoalRevision(math.MaxInt64) {
			state.mu.Unlock()
			return "", errors.New("goal revision exhausted")
		}
		next.Revision++
	}
	state.current, state.armed = &next, armed
	event := GoalChangeEvent{Operation: operation, Goal: &next}.Clone()
	state.mu.Unlock()
	s.events.append(SessionEvent{kind: EventGoalChange, goalChange: event})
	return renderGoal(event.Goal, armed), nil
}

// GoalContinuation is stateless and may be shared. The live stop boundary binds
// its private session; restored state alone never authorizes continuation.
type GoalContinuation struct{}

func (GoalContinuation) Stop(ctx context.Context, value StopContext) (*string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if value.session == nil {
		return nil, nil
	}
	s := value.session
	state := &s.goals
	state.mu.Lock()
	goal := state.current
	if goal == nil || goal.Phase != GoalActive || !state.armed {
		state.mu.Unlock()
		return nil, nil
	}
	if goal.Revision == GoalRevision(math.MaxInt64) {
		state.mu.Unlock()
		return nil, errors.New("goal revision exhausted")
	}
	next := goal.Clone()
	next.Revision++
	operation := GoalRoundOperation
	var continuation *string
	if next.RoundsStarted >= next.MaxRounds {
		next.Phase = GoalBlocked
		next.Blocked = &GoalBlockReason{Code: "round-cap-exhausted", Message: fmt.Sprintf("the goal used all %d continuation rounds without completing; a human can raise the cap with goal_edit and resume", next.MaxRounds)}
		operation = GoalBlockOperation
		state.armed = false
	} else {
		next.RoundsStarted++
		text := fmt.Sprintf("[Goal round %d/%d] Objective: %s\nContinue working toward it. Call goal_complete (with the current revision) when it is finished, or goal_block if you cannot proceed.", next.RoundsStarted, next.MaxRounds, next.Objective)
		continuation = &text
	}
	state.current = &next
	event := GoalChangeEvent{Operation: operation, Goal: &next}.Clone()
	state.mu.Unlock()
	s.events.append(SessionEvent{kind: EventGoalChange, goalChange: event})
	return continuation, nil
}
