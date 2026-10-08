package efficiency

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ComponentFault carries a safe Source exception class independently of private
// diagnostic text. Unsafe class names project to the content-free generic reason.
type ComponentFault struct {
	Class string
	Cause error
}

func (f *ComponentFault) Error() string {
	if f == nil {
		return "component fault"
	}
	if f.Cause != nil {
		return f.Cause.Error()
	}
	return f.Class
}
func (f *ComponentFault) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}

var componentFaultClass = regexp.MustCompile(`\A[A-Za-z][A-Za-z0-9_]{0,63}\z`)

func componentReason(err error) string {
	class := "RuntimeError"
	var fault *ComponentFault
	if errors.As(err, &fault) && fault != nil {
		class = fault.Class
	} else if errors.Is(err, context.DeadlineExceeded) {
		class = "TimeoutError"
	}
	if !componentFaultClass.MatchString(class) {
		return "component_error"
	}
	return "component_error:" + class
}

type TokenCounter func(string) (*big.Int, error)

func EstimateObservationTokens(text string) *big.Int {
	n := new(big.Int).SetUint64(uint64(len(text)))
	n.Add(n, big.NewInt(3))
	return n.Quo(n, big.NewInt(4))
}

type ReducerBindingConfig struct {
	Reducer ObservationReducer
	Mode    *Mode
}
type ObservationPipelineConfig struct {
	Bindings               []ReducerBindingConfig
	DefaultMode            Mode
	RawStore               *RawStore
	InflationGuard         bool
	PreventDoubleReduction bool
	TokenCounter           TokenCounter
}

func DefaultObservationPipelineConfig() ObservationPipelineConfig {
	return ObservationPipelineConfig{DefaultMode: Off, InflationGuard: true, PreventDoubleReduction: true, TokenCounter: func(s string) (*big.Int, error) { return EstimateObservationTokens(s), nil }}
}

type observationBinding struct {
	*componentBinding
	reducer ObservationReducer
}

type componentCounter struct {
	mu   sync.Mutex
	call TokenCounter
}

// ObservationPipeline is an immutable observation-stage library. Component
// lifecycle admission is shared by clones; Agent composition remains separate.
// Clones share reducer serialization, while raw storage stays explicitly bound.
type ObservationPipeline struct {
	*componentRuntime
	bindings          []*observationBinding
	rawStore          *RawStore
	inflation, double bool
}

func NewObservationPipeline(c ObservationPipelineConfig) (*ObservationPipeline, error) {
	if err := c.DefaultMode.Validate(); err != nil {
		return nil, err
	}
	if c.TokenCounter == nil {
		return nil, errors.New("token_counter must be callable")
	}
	p := &ObservationPipeline{componentRuntime: newComponentRuntime(c.TokenCounter), rawStore: c.RawStore, inflation: c.InflationGuard, double: c.PreventDoubleReduction}
	seen := make(map[string]bool)
	for _, entry := range c.Bindings {
		if entry.Reducer == nil {
			return nil, errors.New("component must expose a ComponentDescriptor")
		}
		d, err := entry.Reducer.Descriptor().Snapshot()
		if err != nil {
			return nil, errors.New("component must expose a ComponentDescriptor")
		}
		mode := c.DefaultMode
		if entry.Mode != nil {
			mode = *entry.Mode
			if err = mode.Validate(); err != nil {
				return nil, err
			}
		}
		if d.Stage != Observation {
			return nil, fmt.Errorf("component %s belongs to %s, not observation", d.ID, d.Stage)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("duplicate token-efficiency component id: %s", d.ID)
		}
		seen[d.ID] = true
		binding := newComponentBinding(entry.Reducer, d, mode)
		p.componentRuntime.bindings = append(p.componentRuntime.bindings, binding)
		p.bindings = append(p.bindings, &observationBinding{binding, entry.Reducer})
	}
	return p, nil
}
func (p *ObservationPipeline) WithRawStore(store *RawStore) *ObservationPipeline {
	copy := *p
	copy.rawStore = store
	return &copy
}
func (p *ObservationPipeline) ObservationEnforced() bool {
	for _, b := range p.bindings {
		if b.mode == Enforce {
			return true
		}
	}
	return false
}

type ObservationOutcome struct {
	observation MaskedObservation
	receipts    []Receipt
	warnings    []string
}
type ObservationOutcomeSnapshot struct {
	Observation ObservationConfig   `json:"observation"`
	Receipts    []ReceiptProjection `json:"receipts"`
	Warnings    []string            `json:"warnings"`
}

func (o ObservationOutcome) Observation() MaskedObservation { return o.observation }
func (o ObservationOutcome) Snapshot() (ObservationOutcomeSnapshot, error) {
	obs, err := o.observation.Snapshot()
	if err != nil {
		return ObservationOutcomeSnapshot{}, err
	}
	result := ObservationOutcomeSnapshot{Observation: obs, Receipts: []ReceiptProjection{}, Warnings: append([]string{}, o.warnings...)}
	for _, receipt := range o.receipts {
		r, e := receipt.Projection()
		if e != nil {
			return ObservationOutcomeSnapshot{}, e
		}
		result.Receipts = append(result.Receipts, r)
	}
	return result, nil
}
func (p *componentRuntime) estimateUnlocked(text string) (value *big.Int) {
	value = EstimateObservationTokens(text)
	defer func() {
		if recover() != nil {
			value = EstimateObservationTokens(text)
		}
	}()
	estimate, err := p.counter.call(text)
	if err == nil && estimate != nil && estimate.Sign() >= 0 {
		value = copyInteger(estimate)
	}
	return value
}
func (p *componentRuntime) estimates(before, after string) (*big.Int, *big.Int) {
	p.counter.mu.Lock()
	defer p.counter.mu.Unlock()
	return p.estimateUnlocked(before), p.estimateUnlocked(after)
}
func mergeObservationWarnings(groups ...[]string) []string {
	merged := []string{}
	seen := make(map[string]bool)
	for _, group := range groups {
		for _, warning := range group {
			if !seen[warning] {
				seen[warning] = true
				merged = append(merged, warning)
			}
			if len(merged) == 32 {
				return merged
			}
		}
	}
	return merged
}
func observationContentSupported(d DescriptorConfig, kind string) bool {
	for _, s := range d.ContentTypes {
		if s == "*/*" || s == kind || strings.HasSuffix(s, "/*") && strings.HasPrefix(kind, strings.TrimSuffix(s, "*")) {
			return true
		}
	}
	return false
}
func (p *componentRuntime) receipt(b *componentBinding, status Status, reason, before, after string, refs receiptReferences, warnings []string, elapsed float64) (Receipt, error) {
	beforeTokens, afterTokens := p.estimates(before, after)
	return NewReceipt(ReceiptConfig{ComponentID: b.descriptor.ID, ComponentVersion: b.descriptor.Version, Stage: b.descriptor.Stage, Mode: b.mode, Status: status, Reason: reason,
		RawBytes: new(big.Int).SetUint64(uint64(len(before))), ProjectedBytes: new(big.Int).SetUint64(uint64(len(after))), TokensBeforeEstimate: beforeTokens, TokensAfterEstimate: afterTokens,
		InputDigest: artifactDigest(before), OutputDigest: artifactDigest(after), Lossiness: b.descriptor.Lossiness, Deterministic: b.descriptor.Deterministic, RawRef: refs.ref, RawDigest: refs.digest, Warnings: warnings, ElapsedMS: elapsed})
}
func invocationDuration(ms *big.Int) time.Duration {
	// Source can admit a timeout larger than Go's nanosecond duration. Saturate
	// scheduling only; descriptor metadata stays unbounded. Overflow classes and
	// extremely large scheduler profiles remain a separate compatibility gate.
	max := big.NewInt(math.MaxInt64 / int64(time.Millisecond))
	if ms.Cmp(max) > 0 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(ms.Int64()) * time.Millisecond
}
func (b *observationBinding) invoke(ctx context.Context, o MaskedObservation, options ReductionOptions) (result ObservationReduction, err error) {
	select {
	case b.serial <- struct{}{}:
	case <-ctx.Done():
		return result, ctx.Err()
	}
	defer func() {
		<-b.serial
		if recover() != nil {
			err = &ComponentFault{Class: "RuntimeError", Cause: errors.New("component panic")}
		}
	}()
	child, cancel := context.WithTimeout(ctx, invocationDuration(b.descriptor.TimeoutMS))
	defer cancel()
	options.Query = copyText(options.Query)
	options.BudgetTokens = copyInteger(options.BudgetTokens)
	result, err = b.reducer.Reduce(child, o, options)
	if errors.Is(err, context.Canceled) && errors.Is(child.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		err = context.DeadlineExceeded
	}
	return result, err
}
func putObservationRaw(store *RawStore, text string) (pointer ArtifactPointer, err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("raw store panic")
		}
	}()
	return store.PutMasked(text)
}
func (p *ObservationPipeline) ReduceObservation(ctx context.Context, observation MaskedObservation, options ReductionOptions, persistMaskedRaw bool) (ObservationOutcome, error) {
	if reason := p.lifecycle.enter(false); reason != "" {
		current, err := observation.Snapshot()
		if err != nil {
			return ObservationOutcome{}, err
		}
		outcome := ObservationOutcome{observation: observation, receipts: []Receipt{}, warnings: []string{reason}}
		for _, b := range p.bindings {
			receipt, err := p.receipt(b.componentBinding, Passthrough, reason, current.Content, current.Content, receiptReferences{current.RawRef, current.RawDigest}, []string{reason}, 0)
			if err != nil {
				return ObservationOutcome{}, err
			}
			outcome.receipts = append(outcome.receipts, receipt)
		}
		return outcome, nil
	}
	defer p.lifecycle.leave(false)
	options.Query = copyText(options.Query)
	options.BudgetTokens = copyInteger(options.BudgetTokens)
	current, err := observation.Snapshot()
	if err != nil {
		return ObservationOutcome{}, err
	}
	outcome := ObservationOutcome{observation: observation, receipts: []Receipt{}, warnings: []string{}}
	for _, binding := range p.bindings {
		if err = ctx.Err(); err != nil {
			return ObservationOutcome{}, err
		}
		if !utf8.ValidString(current.Content) {
			return ObservationOutcome{}, errors.New("observation content must be valid UTF-8")
		}
		before := current.Content
		shared := mergeObservationWarnings(outcome.warnings)
		skip := ""
		switch {
		case binding.mode == Off:
			skip = "mode_off"
		case p.double && len(current.ReducedBy) > 0:
			skip = "double_reduction_guard:" + current.ReducedBy[len(current.ReducedBy)-1]
		case !observationContentSupported(binding.descriptor, current.ContentType):
			skip = "unsupported_content_type"
		case binding.descriptor.MaxInputBytes != nil && new(big.Int).SetUint64(uint64(len(before))).Cmp(binding.descriptor.MaxInputBytes) > 0:
			skip = "max_input_bytes_exceeded"
		}
		if skip != "" {
			receipt, e := p.receipt(binding.componentBinding, Passthrough, skip, before, before, receiptReferences{current.RawRef, current.RawDigest}, shared, 0)
			if e != nil {
				return ObservationOutcome{}, e
			}
			outcome.receipts = append(outcome.receipts, receipt)
			continue
		}
		started := time.Now()
		result, callErr := binding.invoke(ctx, outcome.observation, options)
		if errors.Is(callErr, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				callErr = ctx.Err()
			}
			return ObservationOutcome{}, callErr
		}
		var reduction ReductionConfig
		if callErr == nil {
			reduction, callErr = result.Snapshot()
			if callErr != nil {
				callErr = &ComponentFault{Class: "TypeError", Cause: callErr}
			} else {
				_, e := NewReduction(reduction)
				if e != nil {
					callErr = &ComponentFault{Class: "ValueError", Cause: e}
				}
			}
		}
		if callErr == nil && !utf8.ValidString(reduction.Content) {
			callErr = &ComponentFault{Class: "UnicodeEncodeError", Cause: errors.New("invalid reducer UTF-8")}
		}
		elapsed := float64(time.Since(started)) / float64(time.Millisecond)
		if callErr != nil {
			receipt, e := p.receipt(binding.componentBinding, Error, componentReason(callErr), before, before, receiptReferences{current.RawRef, current.RawDigest}, shared, elapsed)
			if e != nil {
				return ObservationOutcome{}, e
			}
			outcome.receipts = append(outcome.receipts, receipt)
			continue
		}
		after := reduction.Content
		warnings := mergeObservationWarnings(shared, reduction.Warnings)
		status, reason := Applied, "reduced"
		switch {
		case after == before:
			status, reason = Passthrough, "component_passthrough"
		case binding.mode == Shadow:
			status, reason = Shadowed, "shadow_candidate"
		default:
			if p.inflation {
				beforeTokens, afterTokens := p.estimates(before, after)
				if len(after) > len(before) || afterTokens.Cmp(beforeTokens) > 0 {
					status, reason = Degraded, "inflation_guard"
				}
			}
		}
		projected := current.clone()
		if status == Applied && binding.descriptor.Lossiness == Recoverable && projected.RawRef == nil {
			storageWarning := ""
			if !persistMaskedRaw || p.rawStore == nil {
				storageWarning = "raw_store_unavailable"
			} else {
				pointer, e := putObservationRaw(p.rawStore, before)
				if e != nil {
					storageWarning = "raw_store_error"
				} else {
					snapshot, e := pointer.Snapshot()
					if e != nil {
						storageWarning = "raw_store_error"
					} else {
						projected.RawRef = &snapshot.Ref
						projected.RawDigest = &snapshot.Digest
					}
				}
			}
			if storageWarning != "" {
				outcome.warnings = append(outcome.warnings, storageWarning)
				warnings = mergeObservationWarnings([]string{storageWarning}, warnings)
				status, reason = Degraded, "recovery_unavailable"
				projected.RawRef = nil
				projected.RawDigest = nil
			}
		}
		receipt, e := p.receipt(binding.componentBinding, status, reason, before, after, receiptReferences{projected.RawRef, projected.RawDigest}, warnings, elapsed)
		if e != nil {
			receipt, e = p.receipt(binding.componentBinding, Error, "component_error:ValueError", before, before, receiptReferences{current.RawRef, current.RawDigest}, shared, elapsed)
		}
		if e != nil {
			return ObservationOutcome{}, e
		}
		outcome.receipts = append(outcome.receipts, receipt)
		if status == Applied && receipt.config.Status == Applied {
			projected.Content = after
			projected.ReducedBy = append(projected.ReducedBy, binding.descriptor.ID)
			next, e := NewObservation(projected)
			if e != nil {
				return ObservationOutcome{}, e
			}
			outcome.observation = next
			current = next.config.clone()
		}
	}
	return outcome, nil
}
