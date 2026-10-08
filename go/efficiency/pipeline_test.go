package efficiency

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

type pipelineProbeFields struct {
	Lossiness     Lossiness
	Recoverable   bool
	MaxInputBytes *big.Int `json:"max_input_bytes"`
	TimeoutMS     *big.Int `json:"timeout_ms"`
}
type pipelineProbeRecipe struct {
	ID, Action string
	Mode       *Mode
	Fields     pipelineProbeFields
}
type pipelineCall struct {
	Component, Content string
	Query              *string
	BudgetTokens       *big.Int `json:"budget_tokens"`
}
type pipelineProbe struct {
	descriptor Descriptor
	action     string
	calls      *[]pipelineCall
}

func (p *pipelineProbe) Descriptor() Descriptor { return p.descriptor }
func (p *pipelineProbe) Reduce(ctx context.Context, o MaskedObservation, options ReductionOptions) (ObservationReduction, error) {
	c, err := o.Snapshot()
	if err != nil {
		return ObservationReduction{}, err
	}
	*p.calls = append(*p.calls, pipelineCall{p.descriptor.config.ID, c.Content, copyText(options.Query), copyInteger(options.BudgetTokens)})
	switch p.action {
	case "RuntimeError", "ValueError":
		return ObservationReduction{}, &ComponentFault{Class: p.action, Cause: errors.New("private payload must not escape")}
	case "unsafe-error":
		return ObservationReduction{}, &ComponentFault{Class: "SECRET=value", Cause: errors.New("private")}
	case "cancelled":
		return ObservationReduction{}, context.Canceled
	case "timeout":
		<-ctx.Done()
		return ObservationReduction{}, ctx.Err()
	case "wrong-result":
		return ObservationReduction{}, nil
	case "invalid-warning":
		result, e := NewReduction(ReductionConfig{Content: "short", Warnings: []string{"unsafe warning"}})
		return result, &ComponentFault{Class: "ValueError", Cause: e}
	}
	text := "short"
	switch p.action {
	case "same":
		text = c.Content
	case "inflate":
		text = c.Content + "extra"
	case "empty":
		text = ""
	}
	return NewReduction(ReductionConfig{Content: text, Warnings: []string{"candidate:1", "candidate:1"}})
}

type pipelineSourceOptions struct {
	DefaultMode            *Mode `json:"default_mode"`
	Counter, Store         string
	InflationGuard         *bool `json:"inflation_guard"`
	PreventDoubleReduction *bool `json:"prevent_double_reduction"`
	Persist                bool
}

func TestObservationPipelineMatchesSource(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Name       string
			Components []pipelineProbeRecipe
			Options    pipelineSourceOptions
			Input      ObservationConfig
			Output     *ObservationOutcomeSnapshot
			Error      *sourceFault
			Calls      []pipelineCall
			Recovered  *string
			Enforced   bool
		}
	}
	data, err := os.ReadFile("../testdata/python-efficiency-pipeline.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 38 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			config := DefaultObservationPipelineConfig()
			calls := []pipelineCall{}
			if row.Options.DefaultMode != nil {
				config.DefaultMode = *row.Options.DefaultMode
			}
			if row.Options.InflationGuard != nil {
				config.InflationGuard = *row.Options.InflationGuard
			}
			if row.Options.PreventDoubleReduction != nil {
				config.PreventDoubleReduction = *row.Options.PreventDoubleReduction
			}
			for _, entry := range row.Components {
				d := DefaultDescriptor(entry.ID, "1", Observation)
				d.ContentTypes = []string{"text/*"}
				if entry.Fields.Lossiness != "" {
					d.Lossiness = entry.Fields.Lossiness
				}
				d.Recoverable = entry.Fields.Recoverable
				d.MaxInputBytes = entry.Fields.MaxInputBytes
				if entry.Fields.TimeoutMS != nil {
					d.TimeoutMS = entry.Fields.TimeoutMS
				}
				descriptor, e := NewDescriptor(d)
				if e != nil {
					t.Fatal(e)
				}
				config.Bindings = append(config.Bindings, ReducerBindingConfig{&pipelineProbe{descriptor, entry.Action, &calls}, entry.Mode})
			}
			var store *RawStore
			if row.Options.Store != "" {
				c := DefaultRawStoreConfig(t.TempDir())
				c.Clock = func() (float64, error) { return 0, nil }
				if row.Options.Store == "small" {
					c.MaxArtifactBytes = big.NewInt(1)
				}
				var e error
				store, e = NewRawStore(c)
				if e != nil {
					t.Fatal(e)
				}
				store.entropy = &fixtureEntropy{constant: true}
				defer store.Close()
				if row.Options.Store == "closed" {
					store.Close()
				}
				config.RawStore = store
			}
			config.TokenCounter = func(text string) (*big.Int, error) {
				switch row.Options.Counter {
				case "broken":
					return nil, errors.New("counter unavailable")
				case "negative":
					return big.NewInt(-1), nil
				case "large":
					return new(big.Int).Exp(big.NewInt(10), big.NewInt(100), nil), nil
				case "inverse":
					if text == "short" {
						return big.NewInt(1000), nil
					}
					return big.NewInt(1), nil
				}
				return EstimateObservationTokens(text), nil
			}
			pipeline, e := NewObservationPipeline(config)
			if e != nil {
				t.Fatal(e)
			}
			if pipeline.ObservationEnforced() != row.Enforced {
				t.Fatal("enforced profile differs")
			}
			obs, e := NewObservation(row.Input)
			if e != nil {
				t.Fatal(e)
			}
			query := "masked query"
			outcome, e := pipeline.ReduceObservation(context.Background(), obs, ReductionOptions{Query: &query, BudgetTokens: big.NewInt(100)}, row.Options.Persist)
			if !reflect.DeepEqual(calls, row.Calls) {
				t.Fatal("calls differ", calls, row.Calls)
			}
			if row.Error != nil {
				if row.Error.Kind != "CancelledError" || !errors.Is(e, context.Canceled) {
					t.Fatal("Source error differs", e, row.Error)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			snapshot, e := outcome.Snapshot()
			if e != nil {
				t.Fatal(e)
			}
			for i := range snapshot.Receipts {
				snapshot.Receipts[i].ElapsedMS = 0
			}
			if !reflect.DeepEqual(snapshot, *row.Output) {
				a, _ := json.Marshal(snapshot)
				b, _ := json.Marshal(row.Output)
				t.Fatalf("outcome differs\ngot %s\nwant %s", a, b)
			}
			if row.Recovered != nil {
				recovered, e := store.GetMasked(*snapshot.Observation.RawRef)
				if e != nil || recovered != *row.Recovered {
					t.Fatal("recovery differs", recovered, e)
				}
			}
		})
	}
}

// Done is observed only when invocation admission evaluates its wait select.
type pipelineQueueContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *pipelineQueueContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

type serialPipelineReducer struct {
	descriptor  Descriptor
	entered     chan struct{}
	release     chan struct{}
	active, max int
}

func (r *serialPipelineReducer) Descriptor() Descriptor { return r.descriptor }
func (r *serialPipelineReducer) Reduce(ctx context.Context, o MaskedObservation, _ ReductionOptions) (ObservationReduction, error) {
	r.active++
	if r.active > r.max {
		r.max = r.active
	}
	defer func() { r.active-- }()
	select {
	case r.entered <- struct{}{}:
	case <-ctx.Done():
		return ObservationReduction{}, ctx.Err()
	}
	select {
	case <-r.release:
	case <-ctx.Done():
		return ObservationReduction{}, ctx.Err()
	}
	c, err := o.Snapshot()
	if err != nil {
		return ObservationReduction{}, err
	}
	return NewReduction(ReductionConfig{Content: c.Content})
}
func TestObservationPipelineCloneSerializationAndQueuedCancellation(t *testing.T) {
	descriptor, err := NewDescriptor(DefaultDescriptor("serial", "1", Observation))
	if err != nil {
		t.Fatal(err)
	}
	reducer := &serialPipelineReducer{descriptor: descriptor, entered: make(chan struct{}, 2), release: make(chan struct{})}
	mode := Enforce
	c := DefaultObservationPipelineConfig()
	c.Bindings = []ReducerBindingConfig{{Reducer: reducer, Mode: &mode}}
	// A deliberately stateful counter is safe across clones as in Source's single
	// asyncio loop; metric pairs execute together through the shared coordinator.
	counterCalls := 0
	c.TokenCounter = func(s string) (*big.Int, error) { counterCalls++; return EstimateObservationTokens(s), nil }
	pipeline, err := NewObservationPipeline(c)
	if err != nil {
		t.Fatal(err)
	}
	clone := pipeline.WithRawStore(nil)
	mode = Off
	c.Bindings[0].Reducer = PassthroughObservationReducer{}
	if !pipeline.ObservationEnforced() || !clone.ObservationEnforced() {
		t.Fatal("caller binding mode mutated runtime")
	}
	obs, err := NewObservation(DefaultObservation("masked"))
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, e := pipeline.ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
		firstDone <- e
	}()
	select {
	case <-reducer.entered:
	case <-time.After(time.Second):
		t.Fatal("first call did not enter")
	}

	ctx, cancel := context.WithCancel(context.Background())
	queuedContext := &pipelineQueueContext{Context: ctx, waiting: make(chan struct{})}
	queuedDone := make(chan error, 1)
	go func() {
		_, e := clone.ReduceObservation(queuedContext, obs, ReductionOptions{}, false)
		queuedDone <- e
	}()
	select {
	case <-queuedContext.waiting:
	case <-time.After(time.Second):
		t.Fatal("clone did not wait for component admission")
	}
	cancel()
	select {
	case e := <-queuedDone:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("queued cancellation did not escape")
	}
	secondDone := make(chan error, 1)
	go func() {
		_, e := clone.ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
		secondDone <- e
	}()
	close(reducer.release)
	for _, done := range []chan error{firstDone, secondDone} {
		select {
		case e := <-done:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(time.Second):
			t.Fatal("call did not finish")
		}
	}
	if reducer.max != 1 || counterCalls != 4 {
		t.Fatal("shared serialization/metrics differ", reducer.max, counterCalls)
	}
	outcome, err := pipeline.ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := outcome.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Receipts[0].RawBytes.SetInt64(999)
	snapshot.Receipts[0].Warnings = append(snapshot.Receipts[0].Warnings, "changed")
	fresh, err := outcome.Snapshot()
	if err != nil || fresh.Receipts[0].RawBytes.Int64() != 6 || len(fresh.Receipts[0].Warnings) != 0 {
		t.Fatal("outcome projection aliases", fresh, err)
	}
}
func TestObservationPipelineConstructorAndPrivateFaultBoundaries(t *testing.T) {
	c := DefaultObservationPipelineConfig()
	c.DefaultMode = "invalid"
	if _, err := NewObservationPipeline(c); err == nil {
		t.Fatal("invalid mode admitted")
	}
	c = DefaultObservationPipelineConfig()
	c.TokenCounter = nil
	if _, err := NewObservationPipeline(c); err == nil {
		t.Fatal("nil counter admitted")
	}
	c = DefaultObservationPipelineConfig()
	c.Bindings = []ReducerBindingConfig{{Reducer: PassthroughObservationReducer{}}, {Reducer: PassthroughObservationReducer{}}}
	if _, err := NewObservationPipeline(c); err == nil || err.Error() != "duplicate token-efficiency component id: passthrough-observation" {
		t.Fatal(err)
	}
	c = DefaultObservationPipelineConfig()
	c.DefaultMode = Enforce
	c.Bindings = []ReducerBindingConfig{{Reducer: PassthroughObservationReducer{}}}
	c.TokenCounter = func(string) (*big.Int, error) { panic("private counter failure") }
	p, err := NewObservationPipeline(c)
	if err != nil {
		t.Fatal(err)
	}
	obs, err := NewObservation(DefaultObservation("masked"))
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := p.ReduceObservation(context.Background(), obs, ReductionOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := outcome.Snapshot()
	if err != nil || s.Receipts[0].TokensBeforeEstimate.Int64() != 2 {
		t.Fatal("counter panic escaped", s, err)
	}
}

func TestObservationPipelineSnapshotsCallArgumentsBeforeCounters(t *testing.T) {
	query := "original"
	budget := big.NewInt(100)
	calls := []pipelineCall{}
	c := DefaultObservationPipelineConfig()
	c.DefaultMode = Enforce
	for _, id := range []string{"first", "second"} {
		d, err := NewDescriptor(DefaultDescriptor(id, "1", Observation))
		if err != nil {
			t.Fatal(err)
		}
		c.Bindings = append(c.Bindings, ReducerBindingConfig{Reducer: &pipelineProbe{descriptor: d, action: "same", calls: &calls}})
	}
	c.TokenCounter = func(s string) (*big.Int, error) {
		query = "changed"
		budget.SetInt64(0)
		return EstimateObservationTokens(s), nil
	}
	p, err := NewObservationPipeline(c)
	if err != nil {
		t.Fatal(err)
	}
	o, err := NewObservation(DefaultObservation("masked"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ReduceObservation(context.Background(), o, ReductionOptions{Query: &query, BudgetTokens: budget}, false); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatal(calls)
	}
	for _, call := range calls {
		if *call.Query != "original" || call.BudgetTokens.Int64() != 100 {
			t.Fatal("caller pointers changed captured arguments", call)
		}
	}
}
