package agent

import (
	"context"
	"errors"
	"fmt"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
	"strings"
)

const PlanSection = "Plan mode is ACTIVE. Investigate and design, but do not mutate the workspace or run commands with side effects yet. When your plan is ready, present it with the `exit_plan_mode` tool -- a markdown plan starting with a `#` heading -- and wait for approval. If the reviewer asks for changes, revise the plan and present it again."
const EventPlanMode SessionEventKind = "plan_mode"

type PlanModeEvent struct {
	Active bool `json:"active"`
}

func (e SessionEvent) PlanMode() (PlanModeEvent, bool) { return e.planMode, e.kind == EventPlanMode }

// FoldPlanMode returns the last logged whole value. It grants no permissions,
// restores no reviewer, and starts no model turn.
func FoldPlanMode(records []SessionEventRecord) bool {
	active := false
	for _, r := range records {
		if v, ok := r.Event.PlanMode(); ok {
			active = v.Active
		}
	}
	return active
}
func (s *Session) PlanModeActive() bool { return s.planMode.Load() }

type PlanReviewRequest struct {
	Authority ToolAuthority
	Plan      string
}
type PlanReview struct {
	Approved bool
	Feedback string
}

// Reviewers are explicit dependencies, shared safely across sessions. They may
// inspect detached authority and PlanModeActive, but cannot reenter a live turn.
// Nil auto-approves, matching the Python headless installation.
type PlanApprover interface {
	ApprovePlan(context.Context, PlanReviewRequest) (PlanReview, error)
}

func reviewPlan(ctx context.Context, reviewer PlanApprover, request PlanReviewRequest) (review PlanReview, err error) {
	defer func() {
		if fault := recover(); fault != nil {
			review = PlanReview{}
			err = fmt.Errorf("plan reviewer panic: %v", fault)
		}
	}()
	return reviewer.ApprovePlan(ctx, request)
}

func (h *runtimeHandler) executePlanMode(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	s := h.session
	if s == nil {
		return "", errors.New("plan mode has no bound session")
	}
	if input.Name() == protocol.ToolEnterPlanMode {
		previous := s.planMode.Swap(true)
		h.events.append(SessionEvent{kind: EventPlanMode, planMode: PlanModeEvent{Active: true}})
		if previous {
			return "Already in plan mode.", nil
		}
		return "Plan mode is now active: investigate and plan; present with exit_plan_mode.", nil
	}
	// Source business refusals are text, not execution faults. Their failed bit
	// stays false through observers, journal settlement, events and loop detection.
	if !s.PlanModeActive() {
		return "Error: not in plan mode; there is no plan to present.", nil
	}
	value, _ := input.ExitPlanMode()
	text := strings.TrimFunc(value.Plan, pytext.Space)
	if !strings.HasPrefix(text, "#") {
		return "Error: present the COMPLETE plan as markdown starting with a `#` heading.", nil
	}
	if h.planApprover != nil {
		review, err := reviewPlan(ctx, h.planApprover, PlanReviewRequest{Authority: authority, Plan: text})
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !review.Approved {
			return "Error: plan not approved. Reviewer feedback: " + review.Feedback, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.planMode.Store(false)
	h.events.append(SessionEvent{kind: EventPlanMode, planMode: PlanModeEvent{Active: false}})
	return "Plan approved. Plan mode is off; proceed with the plan.", nil
}
