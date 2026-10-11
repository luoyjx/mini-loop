package cron

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type cronPanicPayload struct{ calls *atomic.Int32 }

func (p cronPanicPayload) text() string                  { p.calls.Add(1); return "PANIC_PAYLOAD_MUST_NOT_APPEAR" }
func (p cronPanicPayload) Format(s fmt.State, verb rune) { _, _ = io.WriteString(s, p.text()) }
func (p cronPanicPayload) String() string                { return p.text() }
func (p cronPanicPayload) Error() string                 { return p.text() }
func (p cronPanicPayload) GoString() string              { return p.text() }

type panicPayloadMask struct{ value cronPanicPayload }

func (p panicPayloadMask) Mask(string) string { panic(p.value) }

func TestCronPanicDoesNotFormatCallerPayload(t *testing.T) {
	for _, stage := range []string{"resolver", "runner", "persistence"} {
		t.Run(stage, func(t *testing.T) {
			var calls, runs atomic.Int32
			payload := cronPanicPayload{&calls}
			resolver := ResolverFunc(func(SessionID) (Runner, error) {
				if stage == "resolver" {
					panic(payload)
				}
				return RunnerFunc(func(context.Context, Invocation) error {
					runs.Add(1)
					if stage == "runner" {
						panic(payload)
					}
					return nil
				}), nil
			})
			var s *Scheduler
			if stage == "persistence" {
				path := filepath.Join(t.TempDir(), "jobs.json")
				seed(t, path, Job{ID: "mask", Cron: "* * * * *", Prompt: "safe", Session: "s", Recurring: true, Durable: true})
				s = makeScheduler(t, path, resolver)
				s.secrets = panicPayloadMask{payload}
				s.ArmAll()
			} else {
				s = makeScheduler(t, "", resolver)
				schedule(t, s, Request{Session: "s", Cron: "* * * * *", Durable: flag(false)})
			}
			s.Tick(now())
			waitRuns(t, s)
			problems := s.Problems()
			text := strings.Join(problems.Summary(), "\n")
			if calls.Load() != 0 || strings.Contains(text, "PANIC_PAYLOAD") || !strings.Contains(text, "cronPanicPayload") || problems.Total != 1 {
				t.Fatalf("caller panic formatting: calls=%d problems=%+v", calls.Load(), problems)
			}
			expectedRuns := int32(0)
			if stage == "runner" {
				expectedRuns = 1
			}
			if runs.Load() != expectedRuns || s.Jobs()[0].LastFired == "" {
				t.Fatalf("lost occurrence boundary changed: runs=%d jobs=%+v", runs.Load(), s.Jobs())
			}
		})
	}
}
