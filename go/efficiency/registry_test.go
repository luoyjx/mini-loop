package efficiency

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

type registryProbe struct {
	descriptor       Descriptor
	events           *[][2]string
	mu               *sync.Mutex
	started, release chan struct{}
}

func (p *registryProbe) Descriptor() Descriptor { return p.descriptor }
func (p *registryProbe) record(phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	*p.events = append(*p.events, [2]string{p.descriptor.config.ID, phase})
}
func (p *registryProbe) Initialize(context.Context, LifecycleServices) error {
	p.record("initialize")
	return nil
}
func (p *registryProbe) Health() (LifecycleHealth, error) {
	p.record("health")
	return HealthyComponent(), nil
}
func (p *registryProbe) Close(context.Context, LifecycleCloseOptions) error {
	p.record("close")
	return nil
}
func (p *registryProbe) Reduce(ctx context.Context, input MaskedObservation, _ ReductionOptions) (ObservationReduction, error) {
	p.record("reduce")
	if p.started != nil {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
			return ObservationReduction{}, ctx.Err()
		}
	}
	c, err := input.Snapshot()
	if err != nil {
		return ObservationReduction{}, err
	}
	return NewReduction(ReductionConfig{Content: c.Content})
}
func (p *registryProbe) Optimize(_ context.Context, input OptimizationContext, _ RequestOptimizationOptions) (RequestOptimizationResult, error) {
	p.record("optimize")
	c, err := input.Snapshot()
	if err != nil {
		return RequestOptimizationResult{}, err
	}
	return NewRequestOptimization(RequestOptimizationConfig{Request: c.Request})
}
func (p *registryProbe) Plan(_ context.Context, input ResponseContext) (ResponsePlan, error) {
	p.record("plan")
	c, err := input.Snapshot()
	if err != nil {
		return ResponsePlan{}, err
	}
	return NewResponsePlan(ResponsePlanConfig{Settings: c.Settings})
}

func TestRegistryAndSharedRuntimeMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-registry.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name    string
			Entries []struct {
				ID, Target, Object string
				Mode               *Mode
				DescriptorStage    Stage `json:"descriptor_stage"`
			}
			Direct                    bool
			Default                   Mode
			Accepted, Errors          []string
			Snapshot                  [][]string
			Initialize, Health, Close LifecycleReportProjection
			CachedInitialize          bool `json:"cached_initialize"`
			CachedClose               bool `json:"cached_close"`
			Events                    [][2]string
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 10 {
		t.Fatal("Source registry inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			events := [][2]string{}
			var mu sync.Mutex
			objects := map[string]*registryProbe{}
			var registry Registry
			direct := StageComponentsConfig{}
			accepted, errors := []string{}, []string{}
			for _, entry := range row.Entries {
				probe := objects[entry.Object]
				if probe == nil {
					d, e := NewDescriptor(DefaultDescriptor(entry.ID, "1", entry.DescriptorStage))
					if e != nil {
						t.Fatal(e)
					}
					probe = &registryProbe{descriptor: d, events: &events, mu: &mu}
					objects[entry.Object] = probe
				}
				var e error
				if row.Direct {
					switch Stage(entry.Target) {
					case Observation:
						direct.Observation = append(direct.Observation, ReducerBindingConfig{probe, entry.Mode})
					case RequestContext:
						direct.Request = append(direct.Request, RequestBindingConfig{probe, entry.Mode})
					case ResponsePolicy:
						direct.Response = append(direct.Response, ResponseBindingConfig{probe, entry.Mode})
					}
				} else {
					switch Stage(entry.Target) {
					case Observation:
						var out ObservationReducer
						out, e = registry.RegisterObservation(probe, entry.Mode)
						if e == nil && out != probe {
							t.Fatal("registration changed instance")
						}
					case RequestContext:
						var out RequestOptimizer
						out, e = registry.RegisterRequest(probe, entry.Mode)
						if e == nil && out != probe {
							t.Fatal("registration changed instance")
						}
					case ResponsePolicy:
						var out ResponsePlanner
						out, e = registry.RegisterResponse(probe, entry.Mode)
						if e == nil && out != probe {
							t.Fatal("registration changed instance")
						}
					}
				}
				if e != nil {
					errors = append(errors, e.Error())
				} else {
					accepted = append(accepted, entry.ID)
				}
			}
			if !reflect.DeepEqual(accepted, row.Accepted) || !reflect.DeepEqual(errors, row.Errors) {
				t.Fatal("registration differs", accepted, errors, row.Accepted, row.Errors)
			}
			stages := registry.Snapshot()
			if row.Direct {
				stages, err = NewStageComponents(direct)
				if err != nil {
					t.Fatal(err)
				}
			}
			actual := [][]string{{}, {}, {}}
			snapshot := stages.Snapshot()
			for _, b := range snapshot.Observation {
				actual[0] = append(actual[0], b.Reducer.Descriptor().config.ID)
			}
			for _, b := range snapshot.Request {
				actual[1] = append(actual[1], b.Optimizer.Descriptor().config.ID)
			}
			for _, b := range snapshot.Response {
				actual[2] = append(actual[2], b.Planner.Descriptor().config.ID)
			}
			if !reflect.DeepEqual(actual, row.Snapshot) {
				t.Fatal("snapshot differs", actual, row.Snapshot)
			}
			later, _ := NewDescriptor(DefaultDescriptor("later", "1", Observation))
			if _, err = registry.RegisterObservation(&registryProbe{descriptor: later, events: &events, mu: &mu}, nil); err != nil {
				t.Fatal(err)
			}
			c := DefaultRuntimeConfig()
			c.Components = stages
			c.DefaultMode = row.Default
			runtime, e := NewRuntime(c)
			if e != nil {
				t.Fatal(e)
			}
			clone := runtime.WithRawStore(nil)
			initial, e := runtime.Initialize(context.Background(), LifecycleServices{})
			if e != nil {
				t.Fatal(e)
			}
			repeated, e := clone.Initialize(context.Background(), LifecycleServices{})
			if e != nil {
				t.Fatal(e)
			}
			if !row.CachedInitialize || !reflect.DeepEqual(normalizedLifecycle(t, initial), row.Initialize) || !reflect.DeepEqual(normalizedLifecycle(t, repeated), row.Initialize) {
				t.Fatal("initialize differs")
			}
			if !reflect.DeepEqual(normalizedLifecycle(t, clone.Health()), row.Health) {
				t.Fatal("health differs")
			}
			obs, _ := NewObservation(DefaultObservation("text"))
			if _, e = runtime.ReduceObservation(context.Background(), obs, ReductionOptions{}, false); e != nil {
				t.Fatal(e)
			}
			request, _ := NewOptimizationContext(OptimizationContextConfig{Request: requestDocument(t, `{"messages":[]}`)})
			if _, e = clone.OptimizeRequest(context.Background(), request, RequestOptimizationOptions{}); e != nil {
				t.Fatal(e)
			}
			response, _ := NewResponseContext(DefaultResponseContext("task"))
			if _, e = runtime.PlanResponse(context.Background(), response); e != nil {
				t.Fatal(e)
			}
			closed, e := clone.Close(context.Background(), LifecycleCloseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			again, e := runtime.Close(context.Background(), LifecycleCloseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			if !row.CachedClose || !reflect.DeepEqual(normalizedLifecycle(t, closed), row.Close) || !reflect.DeepEqual(normalizedLifecycle(t, again), row.Close) {
				t.Fatal("close differs", normalizedLifecycle(t, closed), row.Close)
			}
			if !reflect.DeepEqual(events, row.Events) {
				t.Fatal("events differ", events, row.Events)
			}
		})
	}
}

func TestMultiStageRuntimeSharedAdmissionAndSnapshot(t *testing.T) {
	events := [][2]string{}
	var mu sync.Mutex
	d, _ := NewDescriptor(DefaultDescriptor("shared", "1", Observation))
	probe := &registryProbe{descriptor: d, events: &events, mu: &mu, started: make(chan struct{}), release: make(chan struct{})}
	mode := Enforce
	source := StageComponentsConfig{Observation: []ReducerBindingConfig{{probe, &mode}}, Request: []RequestBindingConfig{{probe, &mode}}, Response: []ResponseBindingConfig{{probe, &mode}}}
	stages, err := NewStageComponents(source)
	if err != nil {
		t.Fatal(err)
	}
	mode = Off
	source.Observation[0].Reducer = nil
	snapshot := stages.Snapshot()
	*snapshot.Request[0].Mode = Off
	snapshot.Response[0].Planner = nil
	c := DefaultRuntimeConfig()
	c.Components = stages
	runtime, err := NewRuntime(c)
	if err != nil {
		t.Fatal(err)
	}
	clone := runtime.WithRawStore(nil)
	if !runtime.ObservationEnforced() || runtime.observation.bindings[0].serial != runtime.request.bindings[0].serial || runtime.request.bindings[0].serial != runtime.response.bindings[0].serial || len(runtime.bindings) != 1 {
		t.Fatal("shared instance not serialized/deduplicated")
	}
	obs, _ := NewObservation(DefaultObservation("text"))
	done := make(chan error, 1)
	go func() {
		_, e := runtime.ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
		done <- e
	}()
	<-probe.started
	if normalizedLifecycle(t, clone.Health()).Status != LifecycleSkipped {
		t.Fatal("health raced active call")
	}
	request, _ := NewOptimizationContext(OptimizationContextConfig{Request: requestDocument(t, `{"messages":[]}`)})
	base, cancel := context.WithCancel(context.Background())
	queued := &pipelineQueueContext{Context: base, waiting: make(chan struct{})}
	requestDone := make(chan error, 1)
	go func() { _, e := clone.OptimizeRequest(queued, request, RequestOptimizationOptions{}); requestDone <- e }()
	select {
	case <-queued.waiting:
	case <-time.After(time.Second):
		t.Fatal("request did not wait on shared lock")
	}
	cancel()
	if err = <-requestDone; err != context.Canceled {
		t.Fatal("queued cancellation lost", err)
	}
	deadline := 0.001
	closed, err := clone.Close(context.Background(), LifecycleCloseOptions{DeadlineSeconds: &deadline})
	if err != nil || normalizedLifecycle(t, closed).Status != LifecycleError {
		t.Fatal("close did not wait for all stages", err)
	}
	response, _ := NewResponseContext(DefaultResponseContext("task"))
	out, err := runtime.PlanResponse(context.Background(), response)
	if err != nil {
		t.Fatal(err)
	}
	projection, _ := out.Snapshot()
	if len(projection.Receipts) != 1 || projection.Receipts[0].Reason != "runtime_closed" {
		t.Fatal("response was not fenced", projection)
	}
	close(probe.release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.Close(context.Background(), LifecycleCloseOptions{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(events, [][2]string{{"shared", "reduce"}, {"shared", "close"}}) {
		t.Fatal("cancelled/fenced stage ran a hook", events)
	}
}
