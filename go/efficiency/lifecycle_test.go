package efficiency

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

type lifecycleProbe struct {
	descriptor                                                                           Descriptor
	action                                                                               string
	mu                                                                                   *sync.Mutex
	events                                                                               *[][2]string
	initStarted, initRelease, reduceStarted, reduceRelease, healthStarted, healthRelease chan struct{}
}

func (p *lifecycleProbe) Descriptor() Descriptor { return p.descriptor }
func (p *lifecycleProbe) event(phase string) {
	d, _ := p.descriptor.Snapshot()
	p.mu.Lock()
	*p.events = append(*p.events, [2]string{d.ID, phase})
	p.mu.Unlock()
}
func (p *lifecycleProbe) Reduce(ctx context.Context, o MaskedObservation, options ReductionOptions) (ObservationReduction, error) {
	p.event("reduce")
	if p.reduceStarted != nil {
		close(p.reduceStarted)
		select {
		case <-p.reduceRelease:
		case <-ctx.Done():
			return ObservationReduction{}, ctx.Err()
		}
	}
	return NewReduction(ReductionConfig{Content: "short"})
}
func (p *lifecycleProbe) Initialize(ctx context.Context, services LifecycleServices) error {
	p.event("initialize")
	if p.initStarted != nil {
		close(p.initStarted)
		select {
		case <-p.initRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.fault(ctx, "initialize")
}
func (p *lifecycleProbe) fault(ctx context.Context, phase string) error {
	switch p.action {
	case "failure":
		return errors.New("private lifecycle diagnostic")
	case "panic":
		panic("private lifecycle panic")
	case "unsafe":
		if phase == "initialize" {
			return &ComponentFault{Class: "Bad private name", Cause: errors.New("private")}
		}
	case "timeout":
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}
func (p *lifecycleProbe) Health() (LifecycleHealth, error) {
	p.event("health")
	if p.healthStarted != nil {
		close(p.healthStarted)
		<-p.healthRelease
	}
	switch p.action {
	case "failure":
		return LifecycleHealth{}, errors.New("private health diagnostic")
	case "panic":
		panic("private health panic")
	case "false":
		return NewLifecycleHealth(LifecycleHealthConfig{Status: LifecycleDegraded, Detail: "component_reported_unhealthy"})
	case "degraded":
		return NewLifecycleHealth(LifecycleHealthConfig{Status: LifecycleDegraded, Detail: "capacity_low"})
	case "skipped":
		return NewLifecycleHealth(LifecycleHealthConfig{Status: LifecycleSkipped})
	case "wrong", "async":
		return LifecycleHealth{}, &ComponentFault{Class: "TypeError"}
	case "forged":
		return LifecycleHealth{}, &ComponentFault{Class: "ValueError"}
	}
	return HealthyComponent(), nil
}
func (p *lifecycleProbe) Close(ctx context.Context, options LifecycleCloseOptions) error {
	p.event("close")
	if options.DeadlineSeconds != nil && *options.DeadlineSeconds <= 0 {
		return errors.New("deadline supplied to hook must be positive")
	}
	return p.fault(ctx, "close")
}

// This wrapper deliberately exposes no optional lifecycle methods.
type bareLifecycleReducer struct{ probe *lifecycleProbe }

func (b bareLifecycleReducer) Descriptor() Descriptor { return b.probe.Descriptor() }
func (b bareLifecycleReducer) Reduce(ctx context.Context, o MaskedObservation, opt ReductionOptions) (ObservationReduction, error) {
	return b.probe.Reduce(ctx, o, opt)
}
func newLifecycleProbe(t *testing.T, id, action string, events *[][2]string, mu *sync.Mutex) *lifecycleProbe {
	t.Helper()
	c := DefaultDescriptor(id, "1", Observation)
	c.Version = "1"
	if action == "timeout" {
		c.TimeoutMS.SetInt64(5)
	}
	d, err := NewDescriptor(c)
	if err != nil {
		t.Fatal(err)
	}
	return &lifecycleProbe{descriptor: d, action: action, events: events, mu: mu}
}
func normalizedLifecycle(t *testing.T, r LifecycleReport) LifecycleReportProjection {
	t.Helper()
	out, err := r.Projection()
	if err != nil {
		t.Fatal(err)
	}
	for i := range out.Components {
		out.Components[i].ElapsedMS = 0
	}
	return out
}
func TestLifecycleMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name                      string
			Actions                   []string
			Mode                      Mode
			Default                   Mode
			Initialize, Health, Close LifecycleReportProjection
			ClosedHealth              LifecycleReportProjection `json:"closed_health"`
			ClosedInitialize          LifecycleReportProjection `json:"closed_initialize"`
			CachedInitialize          bool                      `json:"cached_initialize"`
			CachedClose               bool                      `json:"cached_close"`
			ClosedReceipts            []ReceiptProjection       `json:"closed_receipts"`
			ClosedWarnings            []string                  `json:"closed_warnings"`
			Events                    [][2]string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 9 {
		t.Fatal("Source lifecycle inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			events := [][2]string{}
			var mu sync.Mutex
			config := DefaultObservationPipelineConfig()
			config.DefaultMode = row.Default
			for i, action := range row.Actions {
				probe := newLifecycleProbe(t, "probe-"+string(rune('0'+i)), action, &events, &mu)
				var reducer ObservationReducer = probe
				if action == "missing" {
					reducer = bareLifecycleReducer{probe}
				}
				mode := row.Mode
				config.Bindings = append(config.Bindings, ReducerBindingConfig{reducer, &mode})
			}
			p, err := NewObservationPipeline(config)
			if err != nil {
				t.Fatal(err)
			}
			initialized, err := p.Initialize(context.Background(), LifecycleServices{})
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := p.WithRawStore(nil).Initialize(context.Background(), LifecycleServices{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(normalizedLifecycle(t, initialized), row.Initialize) || !reflect.DeepEqual(normalizedLifecycle(t, repeated), row.Initialize) {
				t.Fatal("initialize differs")
			}
			if !row.CachedInitialize || p.lifecycle.initialized == nil {
				t.Fatal("initialize cache absent")
			}
			if out := normalizedLifecycle(t, p.Health()); !reflect.DeepEqual(out, row.Health) {
				t.Fatal("health differs", out, row.Health)
			}
			closed, err := p.WithRawStore(nil).Close(context.Background(), LifecycleCloseOptions{})
			if err != nil {
				t.Fatal(err)
			}
			again, err := p.Close(context.Background(), LifecycleCloseOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(normalizedLifecycle(t, closed), row.Close) || !reflect.DeepEqual(normalizedLifecycle(t, again), row.Close) || !row.CachedClose {
				t.Fatal("close differs")
			}
			obs, _ := NewObservation(DefaultObservation("long observation"))
			out, err := p.ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
			if err != nil {
				t.Fatal(err)
			}
			projected, err := out.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(projected.Receipts, row.ClosedReceipts) || !reflect.DeepEqual(projected.Warnings, row.ClosedWarnings) {
				t.Fatal("closed receipts differ", projected, row.ClosedReceipts)
			}
			if !reflect.DeepEqual(normalizedLifecycle(t, p.Health()), row.ClosedHealth) {
				t.Fatal("closed health differs")
			}
			after, err := p.Initialize(context.Background(), LifecycleServices{})
			if err != nil || !reflect.DeepEqual(normalizedLifecycle(t, after), row.ClosedInitialize) {
				t.Fatal("closed initialize differs", err)
			}
			if !reflect.DeepEqual(events, row.Events) {
				t.Fatal("hook order differs", events, row.Events)
			}
		})
	}
}
func lifecyclePipeline(t *testing.T, probe *lifecycleProbe) *ObservationPipeline {
	t.Helper()
	c := DefaultObservationPipelineConfig()
	c.DefaultMode = Enforce
	c.Bindings = []ReducerBindingConfig{{Reducer: probe}}
	p, err := NewObservationPipeline(c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func waitLifecycleSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("lifecycle did not reach expected gate")
	}
}

type lifecycleResult struct {
	report LifecycleReport
	err    error
}

func TestInitializeAdmissionAndCancellation(t *testing.T) {
	events := [][2]string{}
	var mu sync.Mutex
	probe := newLifecycleProbe(t, "probe", "ok", &events, &mu)
	probe.initStarted = make(chan struct{})
	probe.initRelease = make(chan struct{})
	p := lifecyclePipeline(t, probe)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan lifecycleResult, 1)
	go func() { r, e := p.Initialize(ctx, LifecycleServices{}); done <- lifecycleResult{r, e} }()
	waitLifecycleSignal(t, probe.initStarted)
	obs, _ := NewObservation(DefaultObservation("long observation"))
	result, err := p.WithRawStore(nil).ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := result.Snapshot()
	if out.Receipts[0].Reason != "runtime_busy" || normalizedLifecycle(t, p.Health()).Status != LifecycleSkipped {
		t.Fatal("initialization admission failed")
	}
	cancel()
	if completed := <-done; !errors.Is(completed.err, context.Canceled) {
		t.Fatal("initialize cancellation swallowed", completed.err)
	}
	// Cancellation clears initializing and does not cache partial work.
	p.lifecycle.mu.Lock()
	initializing, cached := p.lifecycle.initializing, p.lifecycle.initialized
	p.lifecycle.mu.Unlock()
	if initializing || cached != nil {
		t.Fatal("canceled initialization was cached")
	}
	probe.initStarted = nil
	probe.initRelease = nil
	report, err := p.Initialize(context.Background(), LifecycleServices{})
	if err != nil || normalizedLifecycle(t, report).Status != LifecycleOK {
		t.Fatal(err)
	}
}
func TestCloseDrainsAndRetriesAcrossClones(t *testing.T) {
	events := [][2]string{}
	var mu sync.Mutex
	probe := newLifecycleProbe(t, "probe", "ok", &events, &mu)
	probe.reduceStarted = make(chan struct{})
	probe.reduceRelease = make(chan struct{})
	p := lifecyclePipeline(t, probe)
	obs, _ := NewObservation(DefaultObservation("long observation"))
	call := make(chan error, 1)
	go func() { _, e := p.ReduceObservation(context.Background(), obs, ReductionOptions{}, false); call <- e }()
	waitLifecycleSignal(t, probe.reduceStarted)
	deadline := 0.001
	timed, err := p.WithRawStore(nil).Close(context.Background(), LifecycleCloseOptions{&deadline})
	if err != nil || normalizedLifecycle(t, timed).Components[0].Detail != "deadline_exceeded" {
		t.Fatal("drain timeout differs", err)
	}
	if p.lifecycle.closedReport != nil {
		t.Fatal("drain timeout was cached")
	}
	skipped, err := p.ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	projection, _ := skipped.Snapshot()
	if projection.Receipts[0].Reason != "runtime_closed" {
		t.Fatal("close did not stop admission")
	}
	mu.Lock()
	if len(events) != 1 {
		t.Fatal("close hook ran before drain", events)
	}
	mu.Unlock()
	close(probe.reduceRelease)
	if err := <-call; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := p.WithRawStore(nil).Close(context.Background(), LifecycleCloseOptions{})
			if e != nil || normalizedLifecycle(t, r).Status != LifecycleOK {
				t.Error("retry failed", e)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(events, [][2]string{{"probe", "reduce"}, {"probe", "close"}}) {
		t.Fatal("close hook repeated", events)
	}
}
func TestHealthExcludesObservationAndCloseWaits(t *testing.T) {
	events := [][2]string{}
	var mu sync.Mutex
	probe := newLifecycleProbe(t, "probe", "ok", &events, &mu)
	probe.healthStarted = make(chan struct{})
	probe.healthRelease = make(chan struct{})
	p := lifecyclePipeline(t, probe)
	healthDone := make(chan LifecycleReport, 1)
	go func() { healthDone <- p.Health() }()
	waitLifecycleSignal(t, probe.healthStarted)
	obs, _ := NewObservation(DefaultObservation("long observation"))
	result, err := p.WithRawStore(nil).ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := result.Snapshot()
	if out.Receipts[0].Reason != "runtime_busy" {
		t.Fatal("health admission failed")
	}
	if normalizedLifecycle(t, p.Health()).Status != LifecycleSkipped {
		t.Fatal("health overlapped")
	}
	deadline := 0.001
	timed, err := p.Close(context.Background(), LifecycleCloseOptions{&deadline})
	if err != nil || normalizedLifecycle(t, timed).Status != LifecycleError {
		t.Fatal("close did not drain health", err)
	}
	close(probe.healthRelease)
	if normalizedLifecycle(t, <-healthDone).Status != LifecycleOK {
		t.Fatal("health failed")
	}
	if _, err := p.Close(context.Background(), LifecycleCloseOptions{}); err != nil {
		t.Fatal(err)
	}
}
func TestLifecycleValidationAndDetachedReports(t *testing.T) {
	for _, c := range []LifecycleHealthConfig{{Status: "invalid"}, {Status: LifecycleOK, Detail: "private detail"}} {
		if _, err := NewLifecycleHealth(c); err == nil {
			t.Fatal("invalid health accepted")
		}
	}
	recordConfig := LifecycleRecordConfig{ComponentID: "probe", ComponentVersion: "1", Stage: Observation, Phase: HealthPhase, Status: LifecycleOK}
	record, err := NewLifecycleRecord(recordConfig)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []LifecycleRecordConfig{recordConfig, recordConfig, recordConfig, recordConfig, recordConfig, recordConfig, recordConfig, recordConfig}
	invalid[0].ComponentID = "!"
	invalid[1].ComponentVersion = "!"
	invalid[2].Stage = "bad"
	invalid[3].Phase = "bad"
	invalid[4].Status = "bad"
	invalid[5].Detail = "private detail"
	invalid[6].ElapsedMS = -1
	invalid[7].ElapsedMS = math.NaN()
	for _, c := range invalid {
		if _, err := NewLifecycleRecord(c); err == nil {
			t.Fatal("invalid record accepted", c)
		}
	}
	records := []LifecycleRecord{record}
	report, err := NewLifecycleReport(HealthPhase, LifecycleOK, records)
	if err != nil {
		t.Fatal(err)
	}
	records[0] = LifecycleRecord{}
	projection, _ := report.Projection()
	projection.Components[0].Detail = "mutated"
	fresh, _ := report.Projection()
	if fresh.Components[0].Detail != "" || !report.Healthy() {
		t.Fatal("report aliases projection")
	}
	if _, err := json.Marshal(LifecycleReport{}); err == nil {
		t.Fatal("zero report marshaled")
	}
	if _, err := NewLifecycleReport(HealthPhase, LifecycleOK, []LifecycleRecord{{}}); err == nil {
		t.Fatal("zero record accepted")
	}
	events := [][2]string{}
	var mu sync.Mutex
	p := lifecyclePipeline(t, newLifecycleProbe(t, "probe", "panic", &events, &mu))
	r, e := p.Initialize(context.Background(), LifecycleServices{})
	if e != nil || normalizedLifecycle(t, r).Components[0].Detail != "component_error:RuntimeError" {
		t.Fatal("panic escaped", e)
	}
	if normalizedLifecycle(t, p.Health()).Components[0].Detail != "component_error:RuntimeError" {
		t.Fatal("health panic escaped")
	}
	r, e = p.Close(context.Background(), LifecycleCloseOptions{})
	if e != nil || normalizedLifecycle(t, r).Components[0].Detail != "component_error:RuntimeError" {
		t.Fatal("close panic escaped", e)
	}
	for _, seconds := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := p.Close(context.Background(), LifecycleCloseOptions{&seconds}); err == nil {
			t.Fatal("invalid deadline accepted")
		}
	}
}

func TestCloseBeforeInitializeAndCanceledDrain(t *testing.T) {
	events := [][2]string{}
	var mu sync.Mutex
	probe := newLifecycleProbe(t, "probe", "ok", &events, &mu)
	probe.reduceStarted = make(chan struct{})
	probe.reduceRelease = make(chan struct{})
	p := lifecyclePipeline(t, probe)
	obs, _ := NewObservation(DefaultObservation("long observation"))
	callCtx, cancelCall := context.WithCancel(context.Background())
	call := make(chan error, 1)
	go func() { _, err := p.ReduceObservation(callCtx, obs, ReductionOptions{}, false); call <- err }()
	waitLifecycleSignal(t, probe.reduceStarted)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Close(canceled, LifecycleCloseOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal("close cancellation swallowed", err)
	}
	cancelCall()
	if err := <-call; !errors.Is(err, context.Canceled) {
		t.Fatal("observation cancellation swallowed", err)
	}
	if r, err := p.Close(context.Background(), LifecycleCloseOptions{}); err != nil || normalizedLifecycle(t, r).Status != LifecycleOK {
		t.Fatal("close retry failed", err)
	}
	if r, err := p.Initialize(context.Background(), LifecycleServices{}); err != nil || normalizedLifecycle(t, r).Status != LifecycleSkipped {
		t.Fatal("closed runtime initialized", err)
	}
	if !reflect.DeepEqual(events, [][2]string{{"probe", "reduce"}, {"probe", "close"}}) {
		t.Fatal("unexpected hooks", events)
	}
}

type closeBudgetProbe struct {
	*lifecycleProbe
	remaining chan float64
}

func (p *closeBudgetProbe) Close(ctx context.Context, options LifecycleCloseOptions) error {
	p.event("close")
	if options.DeadlineSeconds != nil {
		p.remaining <- *options.DeadlineSeconds
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestCloseDeadlineIncludesHooksAndCachesFaults(t *testing.T) {
	events := [][2]string{}
	var mu sync.Mutex
	first := newLifecycleProbe(t, "first", "ok", &events, &mu)
	last := &closeBudgetProbe{newLifecycleProbe(t, "last", "ok", &events, &mu), make(chan float64, 1)}
	c := DefaultObservationPipelineConfig()
	c.DefaultMode = Enforce
	c.Bindings = []ReducerBindingConfig{{Reducer: first}, {Reducer: last}}
	p, err := NewObservationPipeline(c)
	if err != nil {
		t.Fatal(err)
	}
	deadline := 0.01
	r, err := p.Close(context.Background(), LifecycleCloseOptions{&deadline})
	if err != nil {
		t.Fatal(err)
	}
	report := normalizedLifecycle(t, r)
	if report.Components[0].ComponentID != "last" || report.Components[0].Detail != "component_error:TimeoutError" || report.Components[1].Detail != "deadline_exceeded" {
		t.Fatal("close budget/order differs", report)
	}
	if remaining := <-last.remaining; remaining <= 0 || remaining > deadline {
		t.Fatal("hook budget differs", remaining)
	}
	again, err := p.Close(context.Background(), LifecycleCloseOptions{})
	if err != nil || !reflect.DeepEqual(normalizedLifecycle(t, again), report) {
		t.Fatal("close error report not cached", err)
	}
	if !reflect.DeepEqual(events, [][2]string{{"last", "close"}}) {
		t.Fatal("hook ran after budget/repeated close", events)
	}
}
