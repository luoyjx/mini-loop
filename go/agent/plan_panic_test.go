package agent

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

type planPanicPayload struct{ calls *atomic.Int32 }

func (p planPanicPayload) text() string                  { p.calls.Add(1); return "PANIC_PAYLOAD_MUST_NOT_APPEAR" }
func (p planPanicPayload) Format(s fmt.State, verb rune) { _, _ = io.WriteString(s, p.text()) }
func (p planPanicPayload) String() string                { return p.text() }
func (p planPanicPayload) Error() string                 { return p.text() }
func (p planPanicPayload) GoString() string              { return p.text() }

func TestPlanReviewerPanicDoesNotFormatCallerPayload(t *testing.T) {
	var calls atomic.Int32
	review, err := reviewPlan(context.Background(), planApproverFunc(func(context.Context, PlanReviewRequest) (PlanReview, error) { panic(planPanicPayload{&calls}) }), PlanReviewRequest{})
	if err == nil || review != (PlanReview{}) {
		t.Fatalf("panic granted review: %+v err=%v", review, err)
	}
	if calls.Load() != 0 || strings.Contains(err.Error(), "PANIC_PAYLOAD") || !strings.Contains(err.Error(), "planPanicPayload") {
		t.Fatalf("caller panic formatting: calls=%d err=%v", calls.Load(), err)
	}
}
