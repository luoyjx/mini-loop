package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const DecisionToolTimeout = 60 * time.Second

func maskedDecisionRequest(request decisions.Request, masker TextMasker) (decisions.Request, error) {
	if _, err := decisions.NewRequest(request.State(), request.Questions()); err != nil {
		return decisions.Request{}, err
	}
	v := request.Value()
	if masker != nil {
		v = v.MapStrings(masker.MaskText)
	}
	encoded, err := v.MarshalJSON()
	if err != nil {
		return decisions.Request{}, err
	}
	return decisions.DecodeRequest(encoded)
}

func decisionProviderFault(call func() (decisions.Result, error)) (result decisions.Result, err error) {
	defer func() {
		if recover() != nil {
			result = decisions.Result{}
			err = errors.New("decision provider panicked")
		}
	}()
	return call()
}

func (handler *runtimeHandler) executeDecision(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	if handler.session == nil {
		return "", errors.New("decision requires a bound session")
	}
	session := handler.session
	request, ok := input.Decision()
	if !ok {
		return "", errors.New("invalid decision tool variant")
	}
	request, err := maskedDecisionRequest(request, session.secrets)
	if err != nil {
		handler.events.append(SessionEvent{kind: EventDecisionFailed, decisionFailed: DecisionFailedEvent{decisions.ValidationError}})
		return "", fmt.Errorf("Invalid decision request: %w", err)
	}
	backend := session.decisionProvider
	if backend == nil {
		backend, err = NewLLMDecisionProvider(session, authority.RunContext, session.decisionLLM)
		if err != nil {
			return "", err
		}
	}
	_, managedLLM := backend.(*LLMDecisionProvider)
	started := time.Now()
	span := SpanID("")
	if !managedLLM {
		name, err := newSpan("model_", 16)
		if err != nil {
			return "", err
		}
		span = SpanID(name)
		model := "custom-decision"
		if named, ok := backend.(interface{ Model() string }); ok {
			model = named.Model()
		}
		start := DecisionModelStartEvent{SpanID: span, Purpose: protocol.PurposeDecision, Model: model, MessageCount: 1}
		handler.events.appendRecorded(SessionEvent{kind: EventModelStart, decisionModelStart: &start}, trajectoryDetails{kind: EventModelStart, decisionRequest: &request})
	}
	callContext, cancel := context.WithTimeout(ctx, DecisionToolTimeout)
	defer cancel()
	result, err := decisionProviderFault(func() (decisions.Result, error) {
		if !managedLLM {
			lease, err := session.modelLimiter.Acquire(callContext)
			if err != nil {
				return decisions.Result{}, err
			}
			defer lease.Release()
		}
		result, err := backend.Evaluate(callContext, request.Clone())
		if err == nil {
			err = callContext.Err()
		}
		if err == nil {
			err = decisions.ValidateResult(request, result)
		}
		return result, err
	})
	if err == nil {
		err = callContext.Err()
	}
	if err != nil {
		if !managedLLM {
			status := ModelError
			if ctx.Err() != nil {
				status = ModelCancelled
			}
			end := DecisionModelEndEvent{SpanID: span, Purpose: protocol.PurposeDecision, Status: status, DurationMS: float64(time.Since(started).Microseconds()) / 1000}
			handler.events.append(SessionEvent{kind: EventModelEnd, decisionModelEnd: &end})
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		kind, detail := decisions.ErrorKind("RuntimeError"), "RuntimeError"
		var contract *decisions.Error
		if callContext.Err() == context.DeadlineExceeded {
			kind, detail = decisions.ErrorKind("TimeoutError"), "TimeoutError"
		} else if errors.As(err, &contract) {
			kind, detail = contract.Kind(), contract.Error()
		}
		handler.events.append(SessionEvent{kind: EventDecisionFailed, decisionFailed: DecisionFailedEvent{kind}})
		return "", errors.New("Decision failed: " + detail)
	}
	usage := decisionUsage(result.Usage())
	if !managedLLM {
		prompt := int64(0)
		if u, ok := usage.Counts(); ok {
			prompt = u.InputTokens
		}
		model := result.Model()
		end := DecisionModelEndEvent{SpanID: span, Purpose: protocol.PurposeDecision, Status: ModelCompleted, DurationMS: float64(time.Since(started).Microseconds()) / 1000, ServedModel: &model, Usage: &usage, PromptTokens: &prompt}
		handler.events.append(SessionEvent{kind: EventModelEnd, decisionModelEnd: &end})
	}
	handler.events.append(SessionEvent{kind: EventDecisionCompleted, decisionCompleted: DecisionCompletedEvent{Provider: result.Provider(), Model: result.Model(), ProbabilitySource: result.ProbabilitySource(), QuestionCount: len(request.Questions()), Usage: usage}})
	encoded, err := result.MarshalJSON()
	return string(encoded), err
}
