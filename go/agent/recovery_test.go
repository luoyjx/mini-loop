package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/protocol"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recoveryFixtureStep struct {
	Text           string
	Stop           protocol.StopReason
	Tool           bool
	Error, Message string
	Status         int
	After          json.RawMessage
}
type recoveryFixtureCase struct {
	Name, Model, Fallback   string
	Budget                  int
	Steps                   []recoveryFixtureStep
	Direct, Streaming, Long bool
	Escalate                *bool
	Retries, Continuations  *int
	Calls                   []protocol.ModelRequest
	Events                  []struct {
		Type          SessionEventKind
		Action        RecoveryAction
		Error         string
		Attempt       *int
		MaxTokens     *int `json:"max_tokens"`
		Capped        *bool
		Model, Reason *string
	}
	Waits         []float64
	Final         *protocol.ModelReply
	ErrorClass    *string `json:"error_class"`
	Live          []protocol.Message
	ModelOverride *string `json:"model_override"`
}
type recoveryFixtureError struct{ detail protocol.ModelFailure }

func (e *recoveryFixtureError) Error() string                          { return e.detail.Message }
func (e *recoveryFixtureError) RecoveryFailure() protocol.ModelFailure { return e.detail }

type recoveryTestWaiter struct {
	waits []float64
	check func()
}

func (w *recoveryTestWaiter) Wait(ctx context.Context, d time.Duration) error {
	if w.check != nil {
		w.check()
	}
	w.waits = append(w.waits, d.Seconds())
	return ctx.Err()
}
func TestRecoveryMatchesActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-recovery.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Cases []recoveryFixtureCase }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, v := range fixture.Cases {
		t.Run(v.Name, func(t *testing.T) {
			waiter := &recoveryTestWaiter{}
			r, err := NewDefaultRecovery(RecoveryConfig{FallbackModel: v.Fallback, MaxRetries: v.Retries, MaxContinuations: v.Continuations, Escalate: v.Escalate, Waiter: waiter, Jitter: func() float64 { return 0 }})
			if err != nil {
				t.Fatal(err)
			}
			var policy Recovery = r
			if v.Direct {
				policy = DirectRecovery{}
			}
			input := v.Calls[0].Clone()
			input.Purpose = protocol.PurposeAgentTurn
			live := append([]protocol.Message(nil), input.Messages...)
			var model *string
			var events []RecoveryEvent
			calls := 0
			final, err := policy.Recover(context.Background(), RecoveryInput{Request: input, LiveHistory: live, Streaming: v.Streaming}, RecoveryServices{
				Call: func(ctx context.Context, q protocol.ModelRequest) (protocol.ModelReply, error) {
					if calls >= len(v.Calls) {
						t.Fatal("extra attempt")
					}
					want := v.Calls[calls].Clone()
					want.Purpose = protocol.PurposeAgentTurn
					if !reflect.DeepEqual(q, want) {
						t.Fatalf("call %d differs %#v / %#v", calls, q, want)
					}
					step := v.Steps[min(calls, len(v.Steps)-1)]
					calls++
					if step.Error != "" {
						f := protocol.ModelFailure{Kind: protocol.ModelFailureOther, Class: step.Error, Message: step.Message, Status: step.Status}
						if strings.Contains(strings.ToLower(step.Error), "connection") {
							f.Kind = protocol.ModelFailureConnection
						}
						if strings.Contains(strings.ToLower(step.Error), "timeout") {
							f.Kind = protocol.ModelFailureTimeout
						}
						var seconds float64
						if len(step.After) > 0 && string(step.After) != "null" && json.Unmarshal(step.After, &seconds) == nil && seconds >= 0 {
							f.RetryAfterSeconds = &seconds
						}
						return protocol.ModelReply{}, &recoveryFixtureError{f}
					}
					content := []protocol.Block{protocol.NewTextBlock(step.Text)}
					if step.Tool {
						content = append(content, protocol.NewToolUseWithoutCaller("u", protocol.BashToolInput(protocol.BashInput{Command: "pwd"})))
					}
					return protocol.ModelReply{ID: "message", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: "served", Content: content, StopReason: step.Stop, Usage: recoveryFixtureUsage()}, nil
				},
				Emit: func(e RecoveryEvent) { events = append(events, e.clone()) },
				ReplaceHistory: func(m []protocol.Message) error {
					live = append([]protocol.Message(nil), m...)
					return protocol.ValidateTranscript(live)
				},
				SetModel: func(m string) { model = &m },
			})
			if v.ErrorClass == nil {
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(final, *v.Final) {
					t.Fatalf("final differs %#v / %#v", final, *v.Final)
				}
			} else {
				var failure *recoveryFixtureError
				if !errors.As(err, &failure) || failure.detail.Class != *v.ErrorClass {
					t.Fatalf("wrong final error: %v / %s", err, *v.ErrorClass)
				}
			}
			if calls != len(v.Calls) || !reflect.DeepEqual(live, v.Live) || !reflect.DeepEqual(model, v.ModelOverride) {
				t.Fatal("state mismatch", calls, live, model)
			}
			if len(waiter.waits) != len(v.Waits) {
				t.Fatal(waiter.waits, v.Waits)
			}
			for i, d := range waiter.waits {
				if d != v.Waits[i] {
					t.Fatal(waiter.waits, v.Waits)
				}
			}
			if len(events) != len(v.Events) {
				t.Fatal("events", events, v.Events)
			}
			for i, e := range events {
				want := v.Events[i]
				if e.Action != want.Action || !reflect.DeepEqual(e.Attempt, want.Attempt) || !reflect.DeepEqual(e.MaxTokens, want.MaxTokens) || !reflect.DeepEqual(e.Capped, want.Capped) || !reflect.DeepEqual(e.Model, want.Model) || !reflect.DeepEqual(e.Reason, want.Reason) {
					t.Fatal("event differs", e, want)
				}
				if e.Action == RecoveryRetry && e.Error != want.Error {
					t.Fatal(e, want)
				}
			}
		})
	}
}
func TestRecoveryBackoffAndConfigBounds(t *testing.T) {
	for _, n := range []int{-1, 101} {
		if _, err := NewDefaultRecovery(RecoveryConfig{MaxRetries: &n}); err == nil {
			t.Fatal(n)
		}
	}
	r, _ := NewDefaultRecovery(RecoveryConfig{Jitter: func() float64 { return 1 }})
	if r.delay(0, nil) != 625*time.Millisecond || r.delay(20, nil) != 40*time.Second {
		t.Fatal("jitter/cap")
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), -1} {
		if r.delay(0, &f) != 625*time.Millisecond {
			t.Fatal(f)
		}
	}
	zero := 0.0
	if r.delay(3, &zero) != 0 {
		t.Fatal("zero header ignored")
	}
	big := 10000.0
	if r.delay(0, &big) != RecoveryTotalWait {
		t.Fatal("header cap")
	}
}
func TestRecoveryCancellationAndDetachedMetadata(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	waiter := &recoveryTestWaiter{check: cancel}
	r, _ := NewDefaultRecovery(RecoveryConfig{Waiter: waiter})
	calls := 0
	q := protocol.ModelRequest{Model: "model", MaxTokens: 10, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("go")}}, Purpose: protocol.PurposeAgentTurn}
	_, err := r.Recover(ctx, RecoveryInput{Request: q}, RecoveryServices{Call: func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		return protocol.ModelReply{}, &recoveryFixtureError{protocol.ModelFailure{Kind: protocol.ModelFailureConnection, Class: "ConnectionError"}}
	}})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(err, calls)
	}
	n := 1
	reason := "reason"
	original := RecoveryEvent{Action: RecoveryRetry, Attempt: &n, Reason: &reason}
	copy := original.clone()
	*copy.Attempt = 99
	*copy.Reason = "changed"
	if n != 1 || reason != "reason" {
		t.Fatal("metadata aliases")
	}
}

func recoveryFixtureUsage() protocol.TokenUsage {
	zero := 0
	tier := protocol.ServiceTier("standard")
	return protocol.TokenUsage{InputTokens: 21, OutputTokens: 4, CacheReadInputTokens: &zero, CacheCreationInputTokens: &zero, ServiceTier: &tier}
}
