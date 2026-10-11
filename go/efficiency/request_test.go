package efficiency

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type requestCall struct {
	ID          string   `json:"id"`
	RequestJSON string   `json:"request_json"`
	Frozen      *big.Int `json:"frozen"`
	OptimizedBy []string `json:"optimized_by"`
	Budget      *big.Int `json:"budget"`
}
type sourceRequestOutcome struct {
	RequestJSON string              `json:"request_json"`
	Frozen      *big.Int            `json:"frozen"`
	OptimizedBy []string            `json:"optimized_by"`
	Receipts    []ReceiptProjection `json:"receipts"`
}
type requestProbe struct {
	descriptor       Descriptor
	action           string
	calls            *[]requestCall
	mu               *sync.Mutex
	started, release chan struct{}
}

func (p *requestProbe) Descriptor() Descriptor { return p.descriptor }
func replaceRequestField(v jsonvalue.Value, key string, next jsonvalue.Value, remove bool) jsonvalue.Value {
	fields := []jsonvalue.Field{}
	found := false
	for _, name := range v.Keys() {
		child, _ := v.Lookup(name)
		if name == key {
			found = true
			if remove {
				continue
			}
			child = next
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: child})
	}
	if !found && !remove {
		fields = append(fields, jsonvalue.Field{Name: key, Value: next})
	}
	return jsonvalue.ObjectValue(fields)
}
func (p *requestProbe) Optimize(ctx context.Context, c OptimizationContext, options RequestOptimizationOptions) (RequestOptimizationResult, error) {
	snapshot, _ := c.Snapshot()
	d, _ := p.descriptor.Snapshot()
	value, _ := snapshot.Request.Value()
	text, _ := value.LegacyString()
	p.mu.Lock()
	*p.calls = append(*p.calls, requestCall{d.ID, text, copyInteger(snapshot.FrozenPrefixMessages), snapshot.OptimizedBy, copyInteger(options.BudgetTokens)})
	p.mu.Unlock()
	if p.started != nil {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
			return RequestOptimizationResult{}, ctx.Err()
		}
	}
	switch p.action {
	case "timeout":
		<-ctx.Done()
		return RequestOptimizationResult{}, ctx.Err()
	case "cancel":
		return RequestOptimizationResult{}, context.Canceled
	case "failure", "mutate-fail":
		snapshot.FrozenPrefixMessages.SetInt64(0)
		snapshot.OptimizedBy = append(snapshot.OptimizedBy, "private")
		return RequestOptimizationResult{}, errors.New("private optimizer diagnostic")
	case "unsafe":
		return RequestOptimizationResult{}, &ComponentFault{Class: "Bad private name"}
	case "wrong":
		return RequestOptimizationResult{}, nil
	case "model":
		value = replaceRequestField(value, "model", jsonvalue.TextValue("x"), false)
	case "remove":
		value = replaceRequestField(value, "messages", jsonvalue.NullValue(), true)
	case "clear":
		value = replaceRequestField(value, "messages", jsonvalue.ArrayValue(nil), false)
	case "tail", "prefix", "role", "inflate":
		messages, _ := value.Lookup("messages")
		items, _ := messages.Array()
		index := len(items) - 1
		key, replacement := "content", "x"
		if p.action == "prefix" {
			index = 0
		}
		if p.action == "role" {
			key, replacement = "role", "changed"
		}
		if p.action == "inflate" {
			replacement = strings.Repeat("long ", 400)
		}
		items[index] = replaceRequestField(items[index], key, jsonvalue.TextValue(replacement), false)
		value = replaceRequestField(value, "messages", jsonvalue.ArrayValue(items), false)
	}
	document, _ := NewRequestDocument(value)
	warnings := []string{}
	if p.action == "tail" {
		warnings = []string{"tail_changed"}
	}
	result, err := NewRequestOptimization(RequestOptimizationConfig{document, warnings})
	if p.action == "warning" {
		result.config.Warnings = []string{"private invalid warning"}
	}
	return result, err
}
func requestDocument(t *testing.T, text string) RequestDocument {
	t.Helper()
	v, err := jsonvalue.Decode(text)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewRequestDocument(v)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func equalRequestJSON(t *testing.T, a, b string) bool {
	t.Helper()
	left, err := jsonvalue.Decode(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := jsonvalue.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	x, _ := left.LegacyString()
	y, _ := right.LegacyString()
	return x == y
}
func TestRequestPipelineMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-request.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name       string
			Components []struct {
				ID, Action string
				Mode       *Mode
				Fields     json.RawMessage
			}
			Options struct {
				DefaultMode    Mode  `json:"default_mode"`
				InflationGuard *bool `json:"inflation_guard"`
				Double         *bool `json:"prevent_double_reduction"`
				Counter        string
				Closed         bool
			}
			InputJSON      string `json:"input_json"`
			Frozen         *big.Int
			OptimizedBy    []string `json:"optimized_by"`
			Output         *sourceRequestOutcome
			Error          *sourceFault
			Calls          []requestCall
			CounterCalls   []string `json:"counter_calls"`
			InputUnchanged bool     `json:"input_unchanged"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 40 {
		t.Fatal("Source request inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			calls := []requestCall{}
			counterCalls := []string{}
			var mu sync.Mutex
			config := DefaultRequestPipelineConfig()
			if row.Options.DefaultMode != "" {
				config.DefaultMode = row.Options.DefaultMode
			}
			if row.Options.InflationGuard != nil {
				config.InflationGuard = *row.Options.InflationGuard
			}
			if row.Options.Double != nil {
				config.PreventDoubleReduction = *row.Options.Double
			}
			config.TokenCounter = func(text string) (*big.Int, error) {
				counterCalls = append(counterCalls, text)
				switch row.Options.Counter {
				case "broken":
					return nil, errors.New("private counter")
				case "negative":
					return big.NewInt(-1), nil
				case "large":
					return new(big.Int).Exp(big.NewInt(10), big.NewInt(100), nil), nil
				case "inverse":
					if strings.Contains(text, `"content":"x"`) {
						return big.NewInt(1000), nil
					}
					return big.NewInt(1), nil
				}
				return EstimateObservationTokens(text), nil
			}
			for _, spec := range row.Components {
				c := DefaultDescriptor(spec.ID, "1", RequestContext)
				if err := json.Unmarshal(spec.Fields, &c); err != nil {
					t.Fatal(err)
				}
				d, err := NewDescriptor(c)
				if err != nil {
					t.Fatal(err)
				}
				var optimizer RequestOptimizer = &requestProbe{descriptor: d, action: spec.Action, calls: &calls, mu: &mu}
				if spec.Action == "builtin" {
					optimizer = PassthroughRequestOptimizer{}
				}
				config.Bindings = append(config.Bindings, RequestBindingConfig{optimizer, spec.Mode})
			}
			p, err := NewRequestPipeline(config)
			if err != nil {
				t.Fatal(err)
			}
			if row.Options.Closed {
				if _, err := p.Close(context.Background(), LifecycleCloseOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			input, err := NewOptimizationContext(OptimizationContextConfig{requestDocument(t, row.InputJSON), row.Frozen, row.OptimizedBy})
			if err != nil {
				t.Fatal(err)
			}
			budget := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
			result, err := p.OptimizeRequest(context.Background(), input, RequestOptimizationOptions{budget})
			if row.Error != nil {
				if err == nil {
					t.Fatal("expected Source fault", row.Error)
				}
				if row.Error.Kind == "CancelledError" {
					if !errors.Is(err, context.Canceled) {
						t.Fatal("cancellation differs", err)
					}
				} else if err.Error() != row.Error.Message {
					t.Fatal("fault differs", err, row.Error)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := result.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				v, _ := snapshot.Context.Request.Value()
				got, _ := v.LegacyString()
				if !equalRequestJSON(t, got, row.Output.RequestJSON) || snapshot.Context.FrozenPrefixMessages.Cmp(row.Output.Frozen) != 0 || !reflect.DeepEqual(snapshot.Context.OptimizedBy, row.Output.OptimizedBy) {
					t.Fatal("request context differs", got, row.Output)
				}
				for i := range snapshot.Receipts {
					snapshot.Receipts[i].ElapsedMS = 0
				}
				if !reflect.DeepEqual(snapshot.Receipts, row.Output.Receipts) {
					t.Fatal("request receipts differ", snapshot.Receipts, row.Output.Receipts)
				}
			}
			for i := range calls {
				if i >= len(row.Calls) {
					t.Fatal("too many calls")
				}
				if !equalRequestJSON(t, calls[i].RequestJSON, row.Calls[i].RequestJSON) {
					t.Fatal("component input differs")
				}
				calls[i].RequestJSON = row.Calls[i].RequestJSON
			}
			if !reflect.DeepEqual(calls, row.Calls) || !reflect.DeepEqual(counterCalls, row.CounterCalls) {
				t.Fatal("call/counter order differs", calls, row.Calls, counterCalls, row.CounterCalls)
			}
			untouched, _ := input.Snapshot()
			v, _ := untouched.Request.Value()
			got, _ := v.LegacyString()
			if !row.InputUnchanged || !equalRequestJSON(t, got, row.InputJSON) {
				t.Fatal("input was mutated")
			}
		})
	}
}
func TestRequestContextDetachesMutableFields(t *testing.T) {
	doc := requestDocument(t, `{"messages":[{"role":"user","content":"long"}]}`)
	count := big.NewInt(1)
	markers := []string{"one", "one"}
	c, err := NewOptimizationContext(OptimizationContextConfig{doc, count, markers})
	if err != nil {
		t.Fatal(err)
	}
	count.SetInt64(0)
	markers[0] = "changed"
	snapshot, _ := c.Snapshot()
	snapshot.FrozenPrefixMessages.SetInt64(999)
	snapshot.OptimizedBy[0] = "changed again"
	fresh, _ := c.Snapshot()
	if fresh.FrozenPrefixMessages.Int64() != 1 || !reflect.DeepEqual(fresh.OptimizedBy, []string{"one"}) {
		t.Fatal("context aliases mutable fields")
	}
	if _, err := NewRequestDocument(jsonvalue.ArrayValue(nil)); err == nil {
		t.Fatal("non-object accepted")
	}
	if _, err := NewOptimizationContext(OptimizationContextConfig{doc, big.NewInt(-1), nil}); err == nil {
		t.Fatal("negative prefix accepted")
	}
	if _, err := NewRequestOptimization(RequestOptimizationConfig{doc, []string{"private invalid warning"}}); err == nil {
		t.Fatal("invalid warning accepted")
	}
	if _, err := json.Marshal(RequestDocument{}); err == nil {
		t.Fatal("zero document marshaled")
	}
}
func TestRequestCloseDrainsAndFencesClones(t *testing.T) {
	calls := []requestCall{}
	var mu sync.Mutex
	d, _ := NewDescriptor(DefaultDescriptor("probe", "1", RequestContext))
	probe := &requestProbe{descriptor: d, action: "same", calls: &calls, mu: &mu, started: make(chan struct{}), release: make(chan struct{})}
	config := DefaultRequestPipelineConfig()
	config.DefaultMode = Enforce
	config.Bindings = []RequestBindingConfig{{Optimizer: probe}}
	p, err := NewRequestPipeline(config)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := NewOptimizationContext(OptimizationContextConfig{Request: requestDocument(t, `{"messages":[{"role":"user","content":"long"}]}`)})
	done := make(chan error, 1)
	go func() {
		_, err := p.OptimizeRequest(context.Background(), input, RequestOptimizationOptions{})
		done <- err
	}()
	waitLifecycleSignal(t, probe.started)
	if normalizedLifecycle(t, p.Health()).Status != LifecycleSkipped {
		t.Fatal("health overlapped optimizer")
	}
	deadline := 0.001
	r, err := p.Clone().Close(context.Background(), LifecycleCloseOptions{&deadline})
	if err != nil || normalizedLifecycle(t, r).Components[0].Stage != RequestContext {
		t.Fatal("request close did not drain", err)
	}
	result, err := p.OptimizeRequest(context.Background(), input, RequestOptimizationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := result.Snapshot()
	if snapshot.Receipts[0].Reason != "runtime_closed" || len(snapshot.Receipts[0].Warnings) != 0 {
		t.Fatal("request closed receipt differs")
	}
	close(probe.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	r, err = p.Clone().Close(context.Background(), LifecycleCloseOptions{})
	if err != nil || normalizedLifecycle(t, r).Status != LifecycleSkipped {
		t.Fatal("missing request hooks differ", err)
	}
}

func TestRequestCloneQueuedCancellationAndPinnedBindings(t *testing.T) {
	calls := []requestCall{}
	var mu sync.Mutex
	d, _ := NewDescriptor(DefaultDescriptor("probe", "1", RequestContext))
	probe := &requestProbe{descriptor: d, action: "same", calls: &calls, mu: &mu, started: make(chan struct{}), release: make(chan struct{})}
	config := DefaultRequestPipelineConfig()
	mode := Enforce
	config.Bindings = []RequestBindingConfig{{Optimizer: probe, Mode: &mode}}
	p, err := NewRequestPipeline(config)
	if err != nil {
		t.Fatal(err)
	}
	mode = Off
	config.Bindings[0].Optimizer = nil
	input, _ := NewOptimizationContext(OptimizationContextConfig{Request: requestDocument(t, `{"messages":[{"role":"user","content":"long"}]}`)})
	first := make(chan error, 1)
	go func() {
		_, err := p.OptimizeRequest(context.Background(), input, RequestOptimizationOptions{})
		first <- err
	}()
	waitLifecycleSignal(t, probe.started)
	// Reuse the observation test's context whose Done access proves the second
	// call reached component serialization rather than merely launching a goroutine.
	canceled, cancel := context.WithCancel(context.Background())
	queued := &pipelineQueueContext{Context: canceled, waiting: make(chan struct{})}
	second := make(chan error, 1)
	go func() {
		_, err := p.Clone().OptimizeRequest(queued, input, RequestOptimizationOptions{})
		second <- err
	}()
	waitLifecycleSignal(t, queued.waiting)
	cancel()
	if err := <-second; !errors.Is(err, context.Canceled) {
		t.Fatal("queued cancellation swallowed", err)
	}
	close(probe.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatal("clone serialization or frozen mode failed", calls)
	}
}
