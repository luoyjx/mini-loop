package efficiency

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// RequestDocument is the provider-neutral optimization document. Its values are
// closed immutable JSON variants, not a service map or an opaque serialized bag.
// A provider consumer must lower the result into its concrete protocol request.
type RequestDocument struct {
	value jsonvalue.Value
	valid bool
}

func NewRequestDocument(value jsonvalue.Value) (RequestDocument, error) {
	if value.Kind() != jsonvalue.Object {
		return RequestDocument{}, errors.New("request must be a mapping")
	}
	return RequestDocument{value, true}, nil
}
func (d RequestDocument) Value() (jsonvalue.Value, error) {
	if !d.valid {
		return jsonvalue.Value{}, errors.New("uninitialized request document")
	}
	return d.value, nil
}
func (d RequestDocument) MarshalJSON() ([]byte, error) {
	v, e := d.Value()
	if e != nil {
		return nil, e
	}
	return v.MarshalJSON()
}

type OptimizationContextConfig struct {
	Request              RequestDocument
	FrozenPrefixMessages *big.Int
	OptimizedBy          []string
}
type OptimizationContext struct {
	config OptimizationContextConfig
	valid  bool
}

func NewOptimizationContext(c OptimizationContextConfig) (OptimizationContext, error) {
	if _, err := c.Request.Value(); err != nil {
		return OptimizationContext{}, err
	}
	if c.FrozenPrefixMessages == nil {
		c.FrozenPrefixMessages = big.NewInt(0)
	}
	if c.FrozenPrefixMessages.Sign() < 0 {
		return OptimizationContext{}, errors.New("frozen_prefix_messages cannot be negative")
	}
	c.FrozenPrefixMessages = copyInteger(c.FrozenPrefixMessages)
	c.OptimizedBy = deduplicateMarkers(c.OptimizedBy)
	return OptimizationContext{c, true}, nil
}
func deduplicateMarkers(markers []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, marker := range markers {
		if !seen[marker] {
			out = append(out, marker)
			seen[marker] = true
		}
	}
	return out
}
func (c OptimizationContext) Snapshot() (OptimizationContextConfig, error) {
	if !c.valid {
		return OptimizationContextConfig{}, errors.New("uninitialized optimization context")
	}
	out := c.config
	out.FrozenPrefixMessages = copyInteger(out.FrozenPrefixMessages)
	out.OptimizedBy = append([]string{}, out.OptimizedBy...)
	return out, nil
}

type RequestOptimizationConfig struct {
	Request  RequestDocument
	Warnings []string
}
type RequestOptimizationResult struct {
	config RequestOptimizationConfig
	valid  bool
}

func NewRequestOptimization(c RequestOptimizationConfig) (RequestOptimizationResult, error) {
	if _, err := c.Request.Value(); err != nil {
		return RequestOptimizationResult{}, errors.New("optimized request must be a mapping")
	}
	if err := validateWarnings(c.Warnings); err != nil {
		return RequestOptimizationResult{}, err
	}
	c.Warnings = append([]string{}, c.Warnings...)
	return RequestOptimizationResult{c, true}, nil
}
func (r RequestOptimizationResult) Snapshot() (RequestOptimizationConfig, error) {
	if !r.valid {
		return RequestOptimizationConfig{}, errors.New("request optimizer returned the wrong result type")
	}
	out := r.config
	out.Warnings = append([]string{}, out.Warnings...)
	return out, nil
}

type RequestOptimizationOptions struct{ BudgetTokens *big.Int }
type RequestOptimizer interface {
	DescriptorComponent
	Optimize(context.Context, OptimizationContext, RequestOptimizationOptions) (RequestOptimizationResult, error)
}
type RequestBindingConfig struct {
	Optimizer RequestOptimizer
	Mode      *Mode
}
type RequestPipelineConfig struct {
	Bindings                               []RequestBindingConfig
	DefaultMode                            Mode
	InflationGuard, PreventDoubleReduction bool
	TokenCounter                           TokenCounter
}

func DefaultRequestPipelineConfig() RequestPipelineConfig {
	return RequestPipelineConfig{DefaultMode: Off, InflationGuard: true, PreventDoubleReduction: true, TokenCounter: func(s string) (*big.Int, error) { return EstimateObservationTokens(s), nil }}
}

type requestBinding struct {
	*componentBinding
	optimizer RequestOptimizer
}
type RequestPipeline struct {
	*componentRuntime
	bindings          []*requestBinding
	inflation, double bool
}

func NewRequestPipeline(c RequestPipelineConfig) (*RequestPipeline, error) {
	if err := c.DefaultMode.Validate(); err != nil {
		return nil, err
	}
	if c.TokenCounter == nil {
		return nil, errors.New("token_counter must be callable")
	}
	p := &RequestPipeline{componentRuntime: newComponentRuntime(c.TokenCounter), inflation: c.InflationGuard, double: c.PreventDoubleReduction}
	seen := map[string]bool{}
	for _, entry := range c.Bindings {
		if entry.Optimizer == nil {
			return nil, errors.New("component must expose a ComponentDescriptor")
		}
		d, err := entry.Optimizer.Descriptor().Snapshot()
		if err != nil {
			return nil, errors.New("component must expose a ComponentDescriptor")
		}
		mode := c.DefaultMode
		if entry.Mode != nil {
			mode = *entry.Mode
			if err := mode.Validate(); err != nil {
				return nil, err
			}
		}
		if d.Stage != RequestContext {
			return nil, fmt.Errorf("component %s belongs to %s, not request_context", d.ID, d.Stage)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("duplicate token-efficiency component id: %s", d.ID)
		}
		seen[d.ID] = true
		b := newComponentBinding(entry.Optimizer, d, mode)
		p.componentRuntime.bindings = append(p.componentRuntime.bindings, b)
		p.bindings = append(p.bindings, &requestBinding{b, entry.Optimizer})
	}
	return p, nil
}

// Clone shares pinned component identities, call gates, counters and lifecycle.
func (p *RequestPipeline) Clone() *RequestPipeline { copy := *p; return &copy }

type RequestOutcome struct {
	context  OptimizationContext
	receipts []Receipt
}
type RequestOutcomeSnapshot struct {
	Context  OptimizationContextConfig
	Receipts []ReceiptProjection
}

func (o RequestOutcome) Context() OptimizationContext { return o.context }
func (o RequestOutcome) Snapshot() (RequestOutcomeSnapshot, error) {
	c, e := o.context.Snapshot()
	if e != nil {
		return RequestOutcomeSnapshot{}, e
	}
	out := RequestOutcomeSnapshot{Context: c, Receipts: []ReceiptProjection{}}
	for _, receipt := range o.receipts {
		projection, e := receipt.Projection()
		if e != nil {
			return RequestOutcomeSnapshot{}, e
		}
		out.Receipts = append(out.Receipts, projection)
	}
	return out, nil
}
func containsNonfinite(v jsonvalue.Value) bool {
	if n, ok := v.Float(); ok {
		return math.IsNaN(n) || math.IsInf(n, 0)
	}
	if items, ok := v.Array(); ok {
		for _, item := range items {
			if containsNonfinite(item) {
				return true
			}
		}
	}
	for _, key := range v.Keys() {
		child, _ := v.Lookup(key)
		if containsNonfinite(child) {
			return true
		}
	}
	return false
}
func replaceSurrogateUTF8(text string) string {
	out := make([]byte, 0, len(text))
	for len(text) > 0 {
		if len(text) >= 3 && text[0] == 0xed && text[1] >= 0xa0 && text[1] <= 0xbf && text[2] >= 0x80 && text[2] <= 0xbf {
			out = append(out, '?')
			text = text[3:]
			continue
		}
		_, size := utf8.DecodeRuneInString(text)
		out = append(out, text[:size]...)
		text = text[size:]
	}
	return string(out)
}
func stableRequestText(v jsonvalue.Value) (string, error) {
	if containsNonfinite(v) {
		return replaceSurrogateUTF8(v.PythonString()), nil
	}
	text, err := v.Sorted().LegacyString()
	if err != nil {
		return "", err
	}
	return replaceSurrogateUTF8(text), nil
}
func frozenRequestPrefix(before, after RequestDocument, count *big.Int) (bool, error) {
	if count.Sign() == 0 {
		return true, nil
	}
	left, _ := before.value.Lookup("messages")
	right, _ := after.value.Lookup("messages")
	a, ok := left.Array()
	if !ok {
		return false, nil
	}
	b, ok := right.Array()
	if !ok {
		return false, nil
	}
	if count.Cmp(new(big.Int).SetUint64(uint64(len(a)))) > 0 || count.Cmp(new(big.Int).SetUint64(uint64(len(b)))) > 0 {
		return false, nil
	}
	n := int(count.Int64())
	x, err := stableRequestText(jsonvalue.ArrayValue(a[:n]))
	if err != nil {
		return false, err
	}
	y, err := stableRequestText(jsonvalue.ArrayValue(b[:n]))
	return x == y, err
}
func (b *requestBinding) invoke(ctx context.Context, c OptimizationContext, options RequestOptimizationOptions) (result RequestOptimizationResult, err error) {
	if err := acquireLifecycle(ctx, b.serial); err != nil {
		return result, err
	}
	defer func() { <-b.serial }()
	timeout, err := invocationDuration(b.descriptor.TimeoutMS)
	if err != nil {
		return result, err
	}
	err = callLifecycleHook(ctx, timeout, func(child context.Context) error {
		options.BudgetTokens = copyInteger(options.BudgetTokens)
		var callErr error
		result, callErr = b.optimizer.Optimize(child, c, options)
		return callErr
	})
	return result, err
}
func (p *RequestPipeline) OptimizeRequest(ctx context.Context, input OptimizationContext, options RequestOptimizationOptions) (RequestOutcome, error) {
	reason := p.lifecycle.enter(false)
	if reason == "" {
		defer p.lifecycle.leave(false)
	}
	c, err := input.Snapshot()
	if err != nil {
		return RequestOutcome{}, err
	}
	options.BudgetTokens = copyInteger(options.BudgetTokens)
	current := input
	out := RequestOutcome{context: input, receipts: []Receipt{}}
	for _, b := range p.bindings {
		before, err := stableRequestText(c.Request.value)
		if err != nil {
			return RequestOutcome{}, err
		}
		skip := reason
		if skip == "" {
			if err := ctx.Err(); err != nil {
				return RequestOutcome{}, err
			}
			switch {
			case b.mode == Off:
				skip = "mode_off"
			case p.double && len(c.OptimizedBy) > 0:
				skip = "double_reduction_guard:" + c.OptimizedBy[len(c.OptimizedBy)-1]
			case b.descriptor.MaxInputBytes != nil && new(big.Int).SetUint64(uint64(len(before))).Cmp(b.descriptor.MaxInputBytes) > 0:
				skip = "max_input_bytes_exceeded"
			}
		}
		if skip != "" {
			receipt, err := p.receipt(b.componentBinding, Passthrough, skip, before, before, receiptReferences{}, nil, 0)
			if err != nil {
				return RequestOutcome{}, err
			}
			out.receipts = append(out.receipts, receipt)
			continue
		}
		start := time.Now()
		result, callErr := b.invoke(ctx, current, options)
		if escaped := cancellation(callErr, ctx); escaped != nil {
			return RequestOutcome{}, escaped
		}
		status, detail, after := Error, componentReason(callErr), before
		warnings := []string{}
		var candidate RequestOptimizationConfig
		if callErr == nil {
			candidate, callErr = result.Snapshot()
			if callErr != nil {
				callErr = &ComponentFault{Class: "TypeError", Cause: callErr}
			}
			if callErr == nil {
				if err := validateWarnings(candidate.Warnings); err != nil {
					callErr = &ComponentFault{Class: "ValueError", Cause: err}
				}
			}
			if callErr == nil {
				after, callErr = stableRequestText(candidate.Request.value)
			}
			if callErr == nil {
				warnings = candidate.Warnings
				unchanged, err := frozenRequestPrefix(c.Request, candidate.Request, c.FrozenPrefixMessages)
				callErr = err
				if callErr == nil {
					switch {
					case !unchanged:
						status, detail = Degraded, "frozen_prefix_guard"
					case after == before:
						status, detail = Passthrough, "component_passthrough"
					case b.mode == Shadow:
						status, detail = Shadowed, "shadow_candidate"
					default:
						inflates := false
						if p.inflation {
							x, y := p.estimates(before, after)
							inflates = len(after) > len(before) || y.Cmp(x) > 0
						}
						if inflates {
							status, detail = Degraded, "inflation_guard"
						} else {
							status, detail = Applied, "optimized"
						}
					}
				}
			}
		}
		if callErr != nil {
			status, detail, after = Error, componentReason(callErr), before
			warnings = []string{}
		}
		receipt, err := p.receipt(b.componentBinding, status, detail, before, after, receiptReferences{}, warnings, float64(time.Since(start))/float64(time.Millisecond))
		if err != nil {
			return RequestOutcome{}, err
		}
		out.receipts = append(out.receipts, receipt)
		if status == Applied {
			c.Request = candidate.Request
			c.OptimizedBy = append(c.OptimizedBy, b.descriptor.ID)
			current, err = NewOptimizationContext(c)
			if err != nil {
				return RequestOutcome{}, err
			}
		}
	}
	out.context = current
	return out, nil
}

// PassthroughRequestOptimizer is the Source-compatible structured no-op.
type PassthroughRequestOptimizer struct{}

func (PassthroughRequestOptimizer) Descriptor() Descriptor {
	descriptor, err := NewDescriptor(DefaultDescriptor("passthrough-request", "1", RequestContext))
	if err != nil {
		panic("invalid builtin request descriptor")
	}
	return descriptor
}
func (PassthroughRequestOptimizer) Optimize(ctx context.Context, c OptimizationContext, _ RequestOptimizationOptions) (RequestOptimizationResult, error) {
	if err := ctx.Err(); err != nil {
		return RequestOptimizationResult{}, err
	}
	snapshot, err := c.Snapshot()
	if err != nil {
		return RequestOptimizationResult{}, err
	}
	return NewRequestOptimization(RequestOptimizationConfig{Request: snapshot.Request})
}
