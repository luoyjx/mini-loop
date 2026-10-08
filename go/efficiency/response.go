package efficiency

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type ResponsePlanner interface {
	DescriptorComponent
	Plan(context.Context, ResponseContext) (ResponsePlan, error)
}
type ResponseBindingConfig struct {
	Planner ResponsePlanner
	Mode    *Mode
}
type ResponsePipelineConfig struct {
	Bindings               []ResponseBindingConfig
	DefaultMode            Mode
	PreventDoubleReduction bool
	TokenCounter           TokenCounter
}

func DefaultResponsePipelineConfig() ResponsePipelineConfig {
	return ResponsePipelineConfig{DefaultMode: Off, PreventDoubleReduction: true, TokenCounter: func(s string) (*big.Int, error) { return EstimateObservationTokens(s), nil }}
}

type responseBinding struct {
	*componentBinding
	planner ResponsePlanner
}
type ResponsePipeline struct {
	*componentRuntime
	bindings []*responseBinding
	double   bool
}

func NewResponsePipeline(c ResponsePipelineConfig) (*ResponsePipeline, error) {
	if e := c.DefaultMode.Validate(); e != nil {
		return nil, e
	}
	if c.TokenCounter == nil {
		return nil, errors.New("token_counter must be callable")
	}
	p := &ResponsePipeline{componentRuntime: newComponentRuntime(c.TokenCounter), double: c.PreventDoubleReduction}
	seen := map[string]bool{}
	for _, entry := range c.Bindings {
		if entry.Planner == nil {
			return nil, errors.New("component must expose a ComponentDescriptor")
		}
		d, e := entry.Planner.Descriptor().Snapshot()
		if e != nil {
			return nil, errors.New("component must expose a ComponentDescriptor")
		}
		mode := c.DefaultMode
		if entry.Mode != nil {
			mode = *entry.Mode
			if e := mode.Validate(); e != nil {
				return nil, e
			}
		}
		if d.Stage != ResponsePolicy {
			return nil, fmt.Errorf("component %s belongs to %s, not response_policy", d.ID, d.Stage)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("duplicate token-efficiency component id: %s", d.ID)
		}
		seen[d.ID] = true
		b := newComponentBinding(entry.Planner, d, mode)
		p.componentRuntime.bindings = append(p.componentRuntime.bindings, b)
		p.bindings = append(p.bindings, &responseBinding{b, entry.Planner})
	}
	return p, nil
}
func (p *ResponsePipeline) Clone() *ResponsePipeline { clone := *p; return &clone }

type ResponseOutcome struct {
	context  ResponseContext
	receipts []Receipt
}
type ResponseOutcomeSnapshot struct {
	Context  ResponseContextConfig
	Receipts []ReceiptProjection
}

func (o ResponseOutcome) Context() ResponseContext { return o.context }
func (o ResponseOutcome) Snapshot() (ResponseOutcomeSnapshot, error) {
	c, e := o.context.Snapshot()
	if e != nil {
		return ResponseOutcomeSnapshot{}, e
	}
	out := ResponseOutcomeSnapshot{c, []ReceiptProjection{}}
	for _, r := range o.receipts {
		projection, e := r.Projection()
		if e != nil {
			return ResponseOutcomeSnapshot{}, e
		}
		out.Receipts = append(out.Receipts, projection)
	}
	return out, nil
}
func (b *responseBinding) invoke(ctx context.Context, c ResponseContext) (result ResponsePlan, err error) {
	if e := acquireLifecycle(ctx, b.serial); e != nil {
		return result, e
	}
	defer func() { <-b.serial }()
	err = callLifecycleHook(ctx, invocationDuration(b.descriptor.TimeoutMS), func(child context.Context) error {
		var callErr error
		result, callErr = b.planner.Plan(child, c)
		return callErr
	})
	return result, err
}
func (p *ResponsePipeline) PlanResponse(ctx context.Context, input ResponseContext) (ResponseOutcome, error) {
	reason := p.lifecycle.enter(false)
	if reason == "" {
		defer p.lifecycle.leave(false)
	}
	c, e := input.Snapshot()
	if e != nil {
		return ResponseOutcome{}, e
	}
	current := input
	out := ResponseOutcome{context: input, receipts: []Receipt{}}
	for _, b := range p.bindings {
		before, e := c.Settings.representation()
		if e != nil {
			return ResponseOutcome{}, e
		}
		skip := reason
		if skip == "" {
			if e := ctx.Err(); e != nil {
				return ResponseOutcome{}, e
			}
			if b.mode == Off {
				skip = "mode_off"
			} else if p.double && len(c.AppliedBy) > 0 {
				skip = "double_reduction_guard:" + c.AppliedBy[len(c.AppliedBy)-1]
			}
		}
		if skip != "" {
			receipt, e := p.receipt(b.componentBinding, Passthrough, skip, before, before, receiptReferences{}, nil, 0)
			if e != nil {
				return ResponseOutcome{}, e
			}
			out.receipts = append(out.receipts, receipt)
			continue
		}
		start := time.Now()
		result, callErr := b.invoke(ctx, current)
		if escaped := cancellation(callErr, ctx); escaped != nil {
			return ResponseOutcome{}, escaped
		}
		status, detail, after := Error, componentReason(callErr), before
		warnings := []string{}
		var candidate ResponsePlanConfig
		if callErr == nil {
			candidate, callErr = result.Snapshot()
			if callErr != nil {
				callErr = &ComponentFault{Class: "TypeError", Cause: callErr}
			}
			if callErr == nil {
				settings, e := candidate.Settings.Snapshot()
				if e != nil {
					callErr = &ComponentFault{Class: "TypeError", Cause: e}
				} else {
					candidate.Settings, e = NewResponseSettings(settings)
					if e != nil {
						callErr = &ComponentFault{Class: "ValueError", Cause: e}
					}
				}
			}
			if callErr == nil {
				if e := validateWarnings(candidate.Warnings); e != nil {
					callErr = &ComponentFault{Class: "ValueError", Cause: e}
				}
			}
			if callErr == nil {
				after, callErr = candidate.Settings.representation()
				if callErr == nil {
					warnings = candidate.Warnings
					ceiling := minimumOutputLimit(c.Settings.config.MaxOutputTokens, c.BudgetTokens)
					limit := candidate.Settings.config.MaxOutputTokens
					switch {
					case candidate.Settings.equal(c.Settings):
						status, detail = Passthrough, "component_passthrough"
					case b.mode == Shadow:
						status, detail = Shadowed, "shadow_candidate"
					case ceiling != nil && (limit == nil || limit.Cmp(ceiling) > 0):
						status, detail = Degraded, "max_output_tokens_inflation"
					default:
						status, detail = Applied, "policy_applied"
					}
				}
			}
		}
		if callErr != nil {
			status, detail, after = Error, componentReason(callErr), before
			warnings = []string{}
		}
		receipt, e := p.receipt(b.componentBinding, status, detail, before, after, receiptReferences{}, warnings, float64(time.Since(start))/float64(time.Millisecond))
		if e != nil {
			return ResponseOutcome{}, e
		}
		out.receipts = append(out.receipts, receipt)
		if status == Applied {
			c.Settings = candidate.Settings
			c.AppliedBy = append(c.AppliedBy, b.descriptor.ID)
			current, e = NewResponseContext(c)
			if e != nil {
				return ResponseOutcome{}, e
			}
		}
	}
	out.context = current
	return out, nil
}
