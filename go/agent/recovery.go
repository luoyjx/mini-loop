package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

const (
	RecoveryMaxRetries       = 10
	RecoveryMaxContinuations = 3
	RecoveryEscalatedTokens  = 64000
	RecoveryTotalWait        = 300 * time.Second
	ContinuationPrompt       = "Continue exactly where the truncated response stopped. Do not repeat completed content."
	ReactiveCompactMarker    = "[Reactive compact: older turns dropped to fit context.]"
)

type ModelCall func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error)
type RecoveryInput struct {
	Request     protocol.ModelRequest
	LiveHistory []protocol.Message
	Streaming   bool
}
type RecoveryServices struct {
	Call           ModelCall
	Emit           func(RecoveryEvent)
	ReplaceHistory func([]protocol.Message) error
	SetModel       func(string)
}
type Recovery interface {
	Recover(context.Context, RecoveryInput, RecoveryServices) (protocol.ModelReply, error)
}
type DirectRecovery struct{}

func (DirectRecovery) Recover(ctx context.Context, in RecoveryInput, services RecoveryServices) (protocol.ModelReply, error) {
	if services.Call == nil {
		return protocol.ModelReply{}, errors.New("recovery requires a model call")
	}
	return services.Call(ctx, in.Request.Clone())
}

type RecoveryWaiter interface {
	Wait(context.Context, time.Duration) error
}
type recoveryTimer struct{}

func (recoveryTimer) Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type RecoveryConfig struct {
	FallbackModel                string
	MaxRetries, MaxContinuations *int
	Escalate                     *bool
	Waiter                       RecoveryWaiter
	Jitter                       func() float64
}
type DefaultRecovery struct {
	fallback               string
	retries, continuations int
	escalate               bool
	wait                   RecoveryWaiter
	jitter                 func() float64
}

func NewDefaultRecovery(cfg RecoveryConfig) (*DefaultRecovery, error) {
	r := &DefaultRecovery{fallback: cfg.FallbackModel, retries: RecoveryMaxRetries, continuations: RecoveryMaxContinuations, escalate: true, wait: cfg.Waiter, jitter: cfg.Jitter}
	if cfg.MaxRetries != nil {
		r.retries = *cfg.MaxRetries
	}
	if cfg.MaxContinuations != nil {
		r.continuations = *cfg.MaxContinuations
	}
	if cfg.Escalate != nil {
		r.escalate = *cfg.Escalate
	}
	if r.retries < 0 || r.retries > 100 || r.continuations < 0 || r.continuations > 100 || len(r.fallback) > 512 {
		return nil, errors.New("invalid recovery configuration")
	}
	if r.wait == nil {
		r.wait = recoveryTimer{}
	}
	if r.jitter == nil {
		r.jitter = rand.Float64
	}
	return r, nil
}
func recoveryFailure(err error) protocol.ModelFailure {
	var source interface{ RecoveryFailure() protocol.ModelFailure }
	if errors.As(err, &source) {
		return source.RecoveryFailure()
	}
	return protocol.ModelFailure{Kind: protocol.ModelFailureOther, Class: fmt.Sprintf("%T", err), Message: err.Error()}
}
func overloaded(f protocol.ModelFailure) bool {
	return f.Kind == protocol.ModelFailureOverloaded || f.Status == 529 || strings.Contains(strings.ToLower(f.Class+" "+f.Message), "overloaded") || strings.Contains(f.Message, "529")
}
func rateLimited(f protocol.ModelFailure) bool {
	v := strings.ToLower(f.Class + " " + f.Message)
	return f.Kind == protocol.ModelFailureRateLimit || f.Status == 429 || strings.Contains(v, "429") || strings.Contains(v, "ratelimit") || strings.Contains(v, "rate limit")
}
func transient(f protocol.ModelFailure) bool {
	return overloaded(f) || rateLimited(f) || f.Kind == protocol.ModelFailureConnection || f.Kind == protocol.ModelFailureTimeout
}
func streamingRequired(f protocol.ModelFailure) bool {
	v := strings.ToLower(f.Message)
	return f.Kind == protocol.ModelFailureStreamingRequired || strings.Contains(v, "streaming is required") || strings.Contains(v, "streaming is strongly recommended")
}
func promptTooLong(f protocol.ModelFailure) bool {
	v := strings.ToLower(f.Message)
	for _, m := range []string{"prompt is too long", "prompt_too_long", "prompt_is_too_long", "context_length_exceeded", "max_context", "too many tokens"} {
		if strings.Contains(v, m) {
			return true
		}
	}
	return false
}
func (r *DefaultRecovery) delay(attempt int, after *float64) time.Duration {
	if after != nil && !math.IsNaN(*after) && !math.IsInf(*after, 0) && *after >= 0 {
		return time.Duration(math.Min(*after, 300) * float64(time.Second))
	}
	j := r.jitter()
	if math.IsNaN(j) || math.IsInf(j, 0) || j < 0 || j > 1 {
		j = 0
	}
	base := math.Min(.5*math.Pow(2, float64(attempt)), 32)
	return time.Duration(base * (1 + j*.25) * float64(time.Second))
}
func reactiveMessages(messages []protocol.Message) ([]protocol.Message, int) {
	if len(messages) <= 6 {
		return append([]protocol.Message(nil), messages...), 0
	}
	start := len(messages) - 6
	uses, results := false, false
	before, _ := messages[start-1].Content.Blocks()
	current, _ := messages[start].Content.Blocks()
	for _, b := range before {
		if _, ok := b.ToolUse(); ok {
			uses = true
		}
	}
	for _, b := range current {
		if _, ok := b.ToolResult(); ok {
			results = true
		}
	}
	if uses && results {
		start--
	}
	out := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(ReactiveCompactMarker)}}
	return append(out, messages[start:]...), start
}
func reactiveRequest(request protocol.ModelRequest) protocol.ModelRequest {
	out := request.Clone()
	messages, start := reactiveMessages(request.Messages)
	if len(request.Messages) <= 6 {
		return out
	}
	out.Messages = messages
	out.Cache.Messages = nil
	for _, p := range request.Cache.Messages {
		if p.MessageIndex >= start {
			p.MessageIndex = p.MessageIndex - start + 1
			out.Cache.Messages = append(out.Cache.Messages, p)
		}
	}
	return out
}
func hasReplyTools(reply protocol.ModelReply) bool {
	for _, b := range reply.Content {
		if _, ok := b.ToolUse(); ok {
			return true
		}
	}
	return false
}
func continuedReply(chunks []protocol.Block, final protocol.ModelReply) protocol.ModelReply {
	reply := final.Clone()
	if len(chunks) == 0 {
		return reply
	}
	reply.Content = append(append([]protocol.Block(nil), chunks...), reply.Content...)
	return reply
}
func (r *DefaultRecovery) Recover(ctx context.Context, in RecoveryInput, services RecoveryServices) (protocol.ModelReply, error) {
	if r == nil || r.wait == nil || r.jitter == nil || services.Call == nil {
		return protocol.ModelReply{}, errors.New("uninitialized recovery")
	}
	request := in.Request.Clone()
	attempt, overloads, continuations := 0, 0, 0
	var waited time.Duration
	escalated, reactive := false, false
	priorBudget := 0
	var escalationPartial *protocol.ModelReply
	var chunks []protocol.Block
	emit := func(event RecoveryEvent) {
		if services.Emit != nil {
			services.Emit(event)
		}
	}
	fail := func(err error, reason string) (protocol.ModelReply, error) {
		event := RecoveryEvent{Action: RecoveryFailed, Error: boundedError(err)}
		if reason != "" {
			event.Reason = &reason
		}
		emit(event)
		return protocol.ModelReply{}, err
	}
	continueChunk := func(reply protocol.ModelReply) error {
		if len(reply.Content) == 0 {
			return errors.New("cannot continue an empty truncated reply")
		}
		chunks = append(chunks, reply.Content...)
		request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(ContinuationPrompt)})
		return request.Validate()
	}
	for {
		if err := ctx.Err(); err != nil {
			return protocol.ModelReply{}, err
		}
		reply, err := services.Call(ctx, request.Clone())
		if err == nil {
			err = ctx.Err()
		}
		if err == nil {
			err = reply.Validate()
		}
		if err != nil {
			if ctx.Err() != nil {
				return protocol.ModelReply{}, ctx.Err()
			}
			f := recoveryFailure(err)
			if transient(f) && attempt < r.retries {
				delay := r.delay(attempt, f.RetryAfterSeconds)
				if waited+delay > RecoveryTotalWait {
					return fail(err, "total retry wait would exceed 300s; a turn does not wait politely forever")
				}
				if overloaded(f) {
					overloads++
					if overloads >= 3 && r.fallback != "" {
						request.Model = r.fallback
						if services.SetModel != nil {
							services.SetModel(r.fallback)
						}
						overloads = 0
						model := r.fallback
						emit(RecoveryEvent{Action: RecoveryFallback, Model: &model})
					}
				}
				n := attempt + 1
				emit(RecoveryEvent{Action: RecoveryRetry, Attempt: &n, Error: f.Class})
				if err := r.wait.Wait(ctx, delay); err != nil {
					return protocol.ModelReply{}, err
				}
				waited += delay
				attempt++
				continue
			}
			if streamingRequired(f) && priorBudget != 0 {
				request.MaxTokens = priorBudget
				priorBudget = 0
				if escalationPartial != nil {
					if err := continueChunk(*escalationPartial); err != nil {
						return fail(err, "")
					}
					escalationPartial = nil
				}
				budget := request.MaxTokens
				reason := "the SDK requires streaming above this budget"
				emit(RecoveryEvent{Action: RecoveryUnescalate, MaxTokens: &budget, Reason: &reason})
				continue
			}
			if promptTooLong(f) && !reactive {
				compact := reactiveRequest(request)
				wireBefore, e := request.Wire()
				if e != nil {
					return fail(e, "")
				}
				wireAfter, e := compact.Wire()
				if e != nil {
					return fail(e, "")
				}
				a, _ := protocol.PythonJSON(wireBefore.Messages, true, false)
				b, _ := protocol.PythonJSON(wireAfter.Messages, true, false)
				if len(b)/4 >= len(a)/4 {
					return fail(err, "context overflow, and compaction could not shrink the surface; retrying the same prompt would fail identically")
				}
				if e := compact.Validate(); e != nil {
					return fail(e, "")
				}
				request = compact
				if in.LiveHistory != nil && services.ReplaceHistory != nil {
					history, _ := reactiveMessages(in.LiveHistory)
					if e := services.ReplaceHistory(history); e != nil {
						return fail(e, "")
					}
				}
				reactive = true
				emit(RecoveryEvent{Action: RecoveryReactive})
				continue
			}
			return fail(err, "")
		}
		overloads = 0
		if r.escalate && reply.StopReason == protocol.StopMaxTokens && !escalated && request.MaxTokens < RecoveryEscalatedTokens {
			target := RecoveryEscalatedTokens
			capped := false
			if !in.Streaming {
				if ceiling, ok := protocol.ListedNonStreamingCeiling(request.Model); ok && ceiling < target {
					target = ceiling
					capped = true
				}
			}
			escalated = true
			if float64(target) >= float64(request.MaxTokens)*1.5 {
				priorBudget = request.MaxTokens
				held := reply.Clone()
				escalationPartial = &held
				request.MaxTokens = target
				emit(RecoveryEvent{Action: RecoveryEscalate, MaxTokens: &target, Capped: &capped})
				continue
			}
		}
		if reply.StopReason == protocol.StopMaxTokens && hasReplyTools(reply) {
			return continuedReply(chunks, reply), nil
		}
		if reply.StopReason == protocol.StopMaxTokens && continuations < r.continuations {
			if err := continueChunk(reply); err != nil {
				return fail(err, "")
			}
			continuations++
			n := continuations
			emit(RecoveryEvent{Action: RecoveryContinue, Attempt: &n})
			continue
		}
		return continuedReply(chunks, reply), nil
	}
}

func (e RecoveryEvent) clone() RecoveryEvent {
	if e.Attempt != nil {
		v := *e.Attempt
		e.Attempt = &v
	}
	if e.MaxTokens != nil {
		v := *e.MaxTokens
		e.MaxTokens = &v
	}
	if e.Capped != nil {
		v := *e.Capped
		e.Capped = &v
	}
	if e.Model != nil {
		v := *e.Model
		e.Model = &v
	}
	if e.Reason != nil {
		v := *e.Reason
		e.Reason = &v
	}
	return e
}
