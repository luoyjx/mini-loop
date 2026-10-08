package efficiency

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// StageComponents keeps stage-specific SPIs and detached binding modes. Components
// are trusted instances, not copied services. Use pointers for shared object identity.
type StageComponents struct {
	observation []ReducerBindingConfig
	request     []RequestBindingConfig
	response    []ResponseBindingConfig
}

type StageComponentsConfig struct {
	Observation []ReducerBindingConfig
	Request     []RequestBindingConfig
	Response    []ResponseBindingConfig
}

func copyMode(mode *Mode) *Mode {
	if mode == nil {
		return nil
	}
	copy := *mode
	return &copy
}

func (s StageComponents) Snapshot() StageComponentsConfig {
	out := StageComponentsConfig{Observation: append([]ReducerBindingConfig{}, s.observation...), Request: append([]RequestBindingConfig{}, s.request...), Response: append([]ResponseBindingConfig{}, s.response...)}
	for i := range out.Observation {
		out.Observation[i].Mode = copyMode(out.Observation[i].Mode)
	}
	for i := range out.Request {
		out.Request[i].Mode = copyMode(out.Request[i].Mode)
	}
	for i := range out.Response {
		out.Response[i].Mode = copyMode(out.Response[i].Mode)
	}
	return out
}

func componentDescriptor(component DescriptorComponent, mode *Mode) (DescriptorConfig, error) {
	if component == nil {
		return DescriptorConfig{}, errors.New("component must expose a ComponentDescriptor")
	}
	value := reflect.ValueOf(component)
	if value.Kind() == reflect.Ptr && value.IsNil() {
		return DescriptorConfig{}, errors.New("component must expose a ComponentDescriptor")
	}
	d, err := component.Descriptor().Snapshot()
	if err != nil {
		return DescriptorConfig{}, errors.New("component must expose a ComponentDescriptor")
	}
	if mode != nil {
		if err = mode.Validate(); err != nil {
			return DescriptorConfig{}, err
		}
	}
	return d, nil
}

// NewStageComponents is the direct Source StageComponents seam: typed SPIs are
// required, but descriptor stages and repeated IDs are not registry admission rules.
func NewStageComponents(c StageComponentsConfig) (StageComponents, error) {
	for _, b := range c.Observation {
		if _, err := componentDescriptor(b.Reducer, b.Mode); err != nil {
			return StageComponents{}, err
		}
	}
	for _, b := range c.Request {
		if _, err := componentDescriptor(b.Optimizer, b.Mode); err != nil {
			return StageComponents{}, err
		}
	}
	for _, b := range c.Response {
		if _, err := componentDescriptor(b.Planner, b.Mode); err != nil {
			return StageComponents{}, err
		}
	}
	s := StageComponents{c.Observation, c.Request, c.Response}.Snapshot()
	return StageComponents{s.Observation, s.Request, s.Response}, nil
}

// Registry performs explicit registration only. The zero value is ready to use.
type Registry struct {
	mu         sync.Mutex
	components StageComponents
	ids        map[string]bool
}

func (r *Registry) admit(component DescriptorComponent, mode *Mode, stage Stage) error {
	d, err := componentDescriptor(component, mode)
	if err != nil {
		return err
	}
	if d.Stage != stage {
		return fmt.Errorf("component %s belongs to %s, not %s", d.ID, d.Stage, stage)
	}
	if r.ids[d.ID] {
		return fmt.Errorf("duplicate token-efficiency component id: %s", d.ID)
	}
	if r.ids == nil {
		r.ids = make(map[string]bool)
	}
	r.ids[d.ID] = true
	return nil
}

func (r *Registry) RegisterObservation(component ObservationReducer, mode *Mode) (ObservationReducer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.admit(component, mode, Observation); err != nil {
		return nil, err
	}
	r.components.observation = append(r.components.observation, ReducerBindingConfig{Reducer: component, Mode: copyMode(mode)})
	return component, nil
}
func (r *Registry) RegisterRequest(component RequestOptimizer, mode *Mode) (RequestOptimizer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.admit(component, mode, RequestContext); err != nil {
		return nil, err
	}
	r.components.request = append(r.components.request, RequestBindingConfig{Optimizer: component, Mode: copyMode(mode)})
	return component, nil
}
func (r *Registry) RegisterResponse(component ResponsePlanner, mode *Mode) (ResponsePlanner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.admit(component, mode, ResponsePolicy); err != nil {
		return nil, err
	}
	r.components.response = append(r.components.response, ResponseBindingConfig{Planner: component, Mode: copyMode(mode)})
	return component, nil
}
func (r *Registry) Snapshot() StageComponents {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.components.Snapshot()
	return StageComponents{c.Observation, c.Request, c.Response}
}
func (r *Registry) Runtime(c RuntimeConfig) (*Runtime, error) {
	c.Components = r.Snapshot()
	return NewRuntime(c)
}

type RuntimeConfig struct {
	Components             StageComponents
	DefaultMode            Mode
	RawStore               *RawStore
	InflationGuard         bool
	PreventDoubleReduction bool
	TokenCounter           TokenCounter
}

func DefaultRuntimeConfig() RuntimeConfig {
	c := DefaultObservationPipelineConfig()
	return RuntimeConfig{DefaultMode: c.DefaultMode, InflationGuard: c.InflationGuard, PreventDoubleReduction: c.PreventDoubleReduction, TokenCounter: c.TokenCounter}
}

// Runtime has one admission, lifecycle and counter root across all three stages.
// Fresh runtimes have fresh roots; WithRawStore clones share the original root.
type Runtime struct {
	*componentRuntime
	observation *ObservationPipeline
	request     *RequestPipeline
	response    *ResponsePipeline
}

func NewRuntime(c RuntimeConfig) (*Runtime, error) {
	if err := c.DefaultMode.Validate(); err != nil {
		return nil, err
	}
	if c.TokenCounter == nil {
		return nil, errors.New("token_counter must be callable")
	}
	root := newComponentRuntime(c.TokenCounter)
	p := &Runtime{componentRuntime: root,
		observation: &ObservationPipeline{componentRuntime: root, rawStore: c.RawStore, inflation: c.InflationGuard, double: c.PreventDoubleReduction},
		request:     &RequestPipeline{componentRuntime: root, inflation: c.InflationGuard, double: c.PreventDoubleReduction},
		response:    &ResponsePipeline{componentRuntime: root, double: c.PreventDoubleReduction}}
	// Only pointer instances have object identity. Value components are independent
	// copies. Named SPI keys stay private; no untyped payload bag enters the runtime.
	locks := make(map[DescriptorComponent]chan struct{})
	seen := make(map[DescriptorComponent]bool)
	bind := func(component DescriptorComponent, override *Mode) (*componentBinding, error) {
		d, err := componentDescriptor(component, override)
		if err != nil {
			return nil, err
		}
		mode := c.DefaultMode
		if override != nil {
			mode = *override
		}
		b := newComponentBinding(component, d, mode)
		pointer := reflect.ValueOf(component).Kind() == reflect.Ptr
		if pointer {
			if serial, ok := locks[component]; ok {
				b.serial = serial
			} else {
				locks[component] = b.serial
			}
		}
		if mode != Off && (!pointer || !seen[component]) {
			root.bindings = append(root.bindings, b)
			if pointer {
				seen[component] = true
			}
		}
		return b, nil
	}
	for _, entry := range c.Components.observation {
		b, err := bind(entry.Reducer, entry.Mode)
		if err != nil {
			return nil, err
		}
		p.observation.bindings = append(p.observation.bindings, &observationBinding{b, entry.Reducer})
	}
	for _, entry := range c.Components.request {
		b, err := bind(entry.Optimizer, entry.Mode)
		if err != nil {
			return nil, err
		}
		p.request.bindings = append(p.request.bindings, &requestBinding{b, entry.Optimizer})
	}
	for _, entry := range c.Components.response {
		b, err := bind(entry.Planner, entry.Mode)
		if err != nil {
			return nil, err
		}
		p.response.bindings = append(p.response.bindings, &responseBinding{b, entry.Planner})
	}
	return p, nil
}

func (r *Runtime) WithRawStore(store *RawStore) *Runtime {
	copy := *r
	copy.observation = r.observation.WithRawStore(store)
	return &copy
}
func (r *Runtime) ObservationEnforced() bool { return r.observation.ObservationEnforced() }
func (r *Runtime) ReduceObservation(ctx context.Context, input MaskedObservation, options ReductionOptions, persistMaskedRaw bool) (ObservationOutcome, error) {
	return r.observation.ReduceObservation(ctx, input, options, persistMaskedRaw)
}
func (r *Runtime) OptimizeRequest(ctx context.Context, input OptimizationContext, options RequestOptimizationOptions) (RequestOutcome, error) {
	return r.request.OptimizeRequest(ctx, input, options)
}
func (r *Runtime) PlanResponse(ctx context.Context, input ResponseContext) (ResponseOutcome, error) {
	return r.response.PlanResponse(ctx, input)
}
