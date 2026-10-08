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
)

type responseContextWire struct {
	Task         string                 `json:"task"`
	Settings     ResponseSettingsConfig `json:"settings"`
	Capabilities []string               `json:"provider_capabilities"`
	Budget       *big.Int               `json:"budget_tokens"`
	Concise      bool                   `json:"concise_requested"`
	AppliedBy    []string               `json:"applied_by"`
}

func responseWire(t *testing.T, c ResponseContextConfig) responseContextWire {
	t.Helper()
	s, e := c.Settings.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	return responseContextWire{c.Task, s, c.ProviderCapabilities, c.BudgetTokens, c.ConciseRequested, c.AppliedBy}
}
func responseFromWire(t *testing.T, c responseContextWire) ResponseContext {
	t.Helper()
	s, e := NewResponseSettings(c.Settings)
	if e != nil {
		t.Fatal(e)
	}
	result, e := NewResponseContext(ResponseContextConfig{c.Task, s, c.Capabilities, c.Budget, c.Concise, c.AppliedBy})
	if e != nil {
		t.Fatal(e)
	}
	return result
}

type responseCall struct {
	ID      string              `json:"id"`
	Context responseContextWire `json:"context"`
}
type responseProbe struct {
	descriptor       Descriptor
	action           string
	cap              *big.Int
	calls            *[]responseCall
	mu               *sync.Mutex
	started, release chan struct{}
}

func (p *responseProbe) Descriptor() Descriptor { return p.descriptor }
func (p *responseProbe) Plan(ctx context.Context, c ResponseContext) (ResponsePlan, error) {
	snapshot, _ := c.Snapshot()
	s, _ := snapshot.Settings.Snapshot()
	d, _ := p.descriptor.Snapshot()
	p.mu.Lock()
	*p.calls = append(*p.calls, responseCall{d.ID, responseContextWire{snapshot.Task, s, snapshot.ProviderCapabilities, copyInteger(snapshot.BudgetTokens), snapshot.ConciseRequested, append([]string{}, snapshot.AppliedBy...)}})
	p.mu.Unlock()
	if p.started != nil {
		close(p.started)
		select {
		case <-p.release:
		case <-ctx.Done():
			return ResponsePlan{}, ctx.Err()
		}
	}
	switch p.action {
	case "failure":
		return ResponsePlan{}, errors.New("private policy diagnostic")
	case "unsafe":
		return ResponsePlan{}, &ComponentFault{Class: "Bad private name"}
	case "timeout":
		<-ctx.Done()
		return ResponsePlan{}, ctx.Err()
	case "cancel":
		return ResponsePlan{}, context.Canceled
	case "wrong":
		return ResponsePlan{}, nil
	case "change":
		value := "brief"
		s.Verbosity = &value
	case "cap":
		value := "brief"
		s.Verbosity = &value
		s.MaxOutputTokens = copyInteger(p.cap)
	case "metadata":
		s.Metadata = append(s.Metadata, ResponseMetadata{"second", "applied"})
	}
	settings, e := NewResponseSettings(s)
	if e != nil {
		return ResponsePlan{}, e
	}
	if p.action == "forged" {
		settings.config.Instructions = []string{strings.Repeat("x", 2049)}
	}
	warnings := []string{}
	if p.action == "change" {
		warnings = []string{"settings_changed"}
	}
	result, e := NewResponsePlan(ResponsePlanConfig{settings, warnings})
	if p.action == "warning" {
		result.config.Warnings = []string{"private invalid warning"}
	}
	return result, e
}

type responseSource struct {
	Settings []struct {
		Name   string
		Input  ResponseSettingsConfig
		Output *ResponseSettingsConfig
		Repr   *string
		Error  *sourceFault
	}
	Rows []struct {
		Name       string
		Components []struct {
			ID, Action string
			Mode       *Mode
			Fields     json.RawMessage
		}
		Options struct {
			DefaultMode Mode  `json:"default_mode"`
			Double      *bool `json:"prevent_double_reduction"`
			Counter     string
			Closed      bool
		}
		Input  responseContextWire
		Output *struct {
			Context  responseContextWire
			Receipts []ReceiptProjection
		}
		Error        *sourceFault
		Calls        []responseCall
		CounterCalls []string `json:"counter_calls"`
	}
}

func readResponseSource(t *testing.T) responseSource {
	t.Helper()
	data, e := os.ReadFile("../testdata/python-efficiency-response.json")
	if e != nil {
		t.Fatal(e)
	}
	var result responseSource
	if e := json.Unmarshal(data, &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Settings) != 19 || len(result.Rows) != 48 {
		t.Fatal("Source response inventory changed")
	}
	return result
}
func TestResponseSettingsMatchSource(t *testing.T) {
	for _, row := range readResponseSource(t).Settings {
		t.Run(row.Name, func(t *testing.T) {
			settings, e := NewResponseSettings(row.Input)
			if row.Error != nil {
				if e == nil || e.Error() != row.Error.Message {
					t.Fatal("constructor fault differs", e, row.Error)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			projection, e := settings.Snapshot()
			if e != nil || !reflect.DeepEqual(projection, *row.Output) {
				t.Fatal("settings differ", e)
			}
			representation, e := settings.representation()
			if e != nil || representation != *row.Repr {
				t.Fatal("Source repr differs", representation, *row.Repr, e)
			}
			data, e := json.Marshal(settings)
			if e != nil {
				t.Fatal(e)
			}
			var decoded ResponseSettingsConfig
			if e := json.Unmarshal(data, &decoded); e != nil || !reflect.DeepEqual(decoded, *row.Output) {
				t.Fatal("settings projection differs", e)
			}
		})
	}
}
func TestResponsePipelineMatchesSource(t *testing.T) {
	for _, row := range readResponseSource(t).Rows {
		t.Run(row.Name, func(t *testing.T) {
			calls := []responseCall{}
			counterCalls := []string{}
			var mu sync.Mutex
			config := DefaultResponsePipelineConfig()
			if row.Options.DefaultMode != "" {
				config.DefaultMode = row.Options.DefaultMode
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
				}
				return EstimateObservationTokens(text), nil
			}
			for _, spec := range row.Components {
				var planner ResponsePlanner
				if spec.Action == "passthrough" {
					planner = PassthroughResponsePlanner{}
				} else if spec.Action == "concise" {
					c := DefaultConciseResponseConfig()
					if e := json.Unmarshal(spec.Fields, &c); e != nil {
						t.Fatal(e)
					}
					p, e := NewConciseResponsePlanner(c)
					if e != nil {
						t.Fatal(e)
					}
					planner = p
				} else {
					var fields struct {
						Cap           *big.Int `json:"cap"`
						TimeoutMS     *big.Int `json:"timeout_ms"`
						MaxInputBytes *big.Int `json:"max_input_bytes"`
					}
					if e := json.Unmarshal(spec.Fields, &fields); e != nil {
						t.Fatal(e)
					}
					d := DefaultDescriptor(spec.ID, "1", ResponsePolicy)
					if fields.TimeoutMS != nil {
						d.TimeoutMS = fields.TimeoutMS
					}
					d.MaxInputBytes = fields.MaxInputBytes
					descriptor, e := NewDescriptor(d)
					if e != nil {
						t.Fatal(e)
					}
					planner = &responseProbe{descriptor: descriptor, action: spec.Action, cap: fields.Cap, calls: &calls, mu: &mu}
				}
				config.Bindings = append(config.Bindings, ResponseBindingConfig{planner, spec.Mode})
			}
			p, e := NewResponsePipeline(config)
			if e != nil {
				t.Fatal(e)
			}
			if row.Options.Closed {
				if _, e := p.Close(context.Background(), LifecycleCloseOptions{}); e != nil {
					t.Fatal(e)
				}
			}
			original := responseFromWire(t, row.Input)
			result, e := p.PlanResponse(context.Background(), original)
			if row.Error != nil {
				if e == nil {
					t.Fatal("expected Source fault")
				}
				if row.Error.Kind == "CancelledError" {
					if !errors.Is(e, context.Canceled) {
						t.Fatal("cancel differs", e)
					}
				} else if e.Error() != row.Error.Message {
					t.Fatal("fault differs", e, row.Error)
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				out, e := result.Snapshot()
				if e != nil {
					t.Fatal(e)
				}
				if got := responseWire(t, out.Context); !reflect.DeepEqual(got, row.Output.Context) {
					t.Fatal("response context differs", got, row.Output.Context)
				}
				for i := range out.Receipts {
					out.Receipts[i].ElapsedMS = 0
				}
				if !reflect.DeepEqual(out.Receipts, row.Output.Receipts) {
					t.Fatal("response receipts differ", out.Receipts, row.Output.Receipts)
				}
			}
			if !reflect.DeepEqual(calls, row.Calls) || !reflect.DeepEqual(counterCalls, row.CounterCalls) {
				t.Fatal("response call/counter order differs", calls, row.Calls, counterCalls, row.CounterCalls)
			}
			untouched, _ := original.Snapshot()
			if !reflect.DeepEqual(responseWire(t, untouched), row.Input) {
				t.Fatal("input context mutated")
			}
		})
	}
}
func TestResponseSettingsContextAndPolicySnapshots(t *testing.T) {
	cap := big.NewInt(1000)
	verbosity := "brief"
	instructions := []string{"preserve warnings"}
	metadata := []ResponseMetadata{{"one", "value"}}
	s, e := NewResponseSettings(ResponseSettingsConfig{instructions, cap, &verbosity, metadata})
	if e != nil {
		t.Fatal(e)
	}
	instructions[0] = "changed"
	cap.SetInt64(1)
	verbosity = "changed"
	metadata[0].Value = "changed"
	snapshot, _ := s.Snapshot()
	snapshot.Instructions[0] = "changed"
	snapshot.MaxOutputTokens.SetInt64(2)
	*snapshot.Verbosity = "changed"
	snapshot.Metadata[0].Value = "changed"
	fresh, _ := s.Snapshot()
	if fresh.Instructions[0] != "preserve warnings" || fresh.MaxOutputTokens.Int64() != 1000 || *fresh.Verbosity != "brief" || fresh.Metadata[0].Value != "value" {
		t.Fatal("settings aliases caller values")
	}
	budget := big.NewInt(500)
	capabilities := []string{"max_tokens"}
	markers := []string{"first", "first"}
	c, e := NewResponseContext(ResponseContextConfig{Settings: s, BudgetTokens: budget, ProviderCapabilities: capabilities, AppliedBy: markers})
	if e != nil {
		t.Fatal(e)
	}
	budget.SetInt64(1)
	capabilities[0] = "changed"
	markers[0] = "changed"
	out, _ := c.Snapshot()
	out.BudgetTokens.SetInt64(2)
	out.ProviderCapabilities[0] = "changed"
	out.AppliedBy[0] = "changed"
	original, _ := c.Snapshot()
	if original.BudgetTokens.Int64() != 500 || original.ProviderCapabilities[0] != "max_tokens" || !reflect.DeepEqual(original.AppliedBy, []string{"first"}) {
		t.Fatal("context aliases caller values")
	}
	cfg := DefaultConciseResponseConfig()
	cfg.RequireOptIn = false
	cfg.MaxOutputTokens = big.NewInt(300)
	policy, e := NewConciseResponsePlanner(cfg)
	if e != nil {
		t.Fatal(e)
	}
	cfg.MaxOutputTokens.SetInt64(1)
	plan, e := policy.Plan(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	planned, _ := plan.Snapshot()
	projected, _ := planned.Settings.Snapshot()
	if projected.MaxOutputTokens.Int64() != 300 {
		t.Fatal("policy aliases caller cap")
	}
	for _, text := range []string{"", " \t\n", "\u001c"} {
		cfg := DefaultConciseResponseConfig()
		cfg.Instruction = text
		if _, e := NewConciseResponsePlanner(cfg); e == nil {
			t.Fatal("empty instruction accepted")
		}
	}
	cfg = DefaultConciseResponseConfig()
	cfg.MaxOutputTokens = big.NewInt(0)
	if _, e := NewConciseResponsePlanner(cfg); e == nil {
		t.Fatal("zero concise cap accepted")
	}
	if _, e := NewResponseContext(ResponseContextConfig{Settings: s, BudgetTokens: big.NewInt(0)}); e == nil {
		t.Fatal("zero budget accepted")
	}
	if _, e := json.Marshal(StableResponseSettings{}); e == nil {
		t.Fatal("zero settings marshaled")
	}
	var pair ResponseMetadata
	if e := json.Unmarshal([]byte(`["a","b","c"]`), &pair); e == nil {
		t.Fatal("malformed metadata pair accepted")
	}
}
func TestResponseCloneCancellationAndCloseDrain(t *testing.T) {
	calls := []responseCall{}
	var mu sync.Mutex
	d, _ := NewDescriptor(DefaultDescriptor("probe", "1", ResponsePolicy))
	probe := &responseProbe{descriptor: d, action: "same", calls: &calls, mu: &mu, started: make(chan struct{}), release: make(chan struct{})}
	cfg := DefaultResponsePipelineConfig()
	mode := Enforce
	cfg.Bindings = []ResponseBindingConfig{{Planner: probe, Mode: &mode}}
	p, e := NewResponsePipeline(cfg)
	if e != nil {
		t.Fatal(e)
	}
	mode = Off
	cfg.Bindings[0].Planner = nil
	c, _ := NewResponseContext(DefaultResponseContext("answer"))
	done := make(chan error, 1)
	go func() { _, e := p.PlanResponse(context.Background(), c); done <- e }()
	waitLifecycleSignal(t, probe.started)
	if normalizedLifecycle(t, p.Health()).Status != LifecycleSkipped {
		t.Fatal("health overlapped response call")
	}
	canceled, cancel := context.WithCancel(context.Background())
	queued := &pipelineQueueContext{Context: canceled, waiting: make(chan struct{})}
	second := make(chan error, 1)
	go func() { _, e := p.Clone().PlanResponse(queued, c); second <- e }()
	waitLifecycleSignal(t, queued.waiting)
	cancel()
	if e := <-second; !errors.Is(e, context.Canceled) {
		t.Fatal("queued response cancellation swallowed", e)
	}
	deadline := 0.001
	r, e := p.Clone().Close(context.Background(), LifecycleCloseOptions{&deadline})
	if e != nil || normalizedLifecycle(t, r).Components[0].Stage != ResponsePolicy {
		t.Fatal("response close failed to drain", e)
	}
	denied, e := p.PlanResponse(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	out, _ := denied.Snapshot()
	if out.Receipts[0].Reason != "runtime_closed" || len(out.Receipts[0].Warnings) != 0 {
		t.Fatal("closed response receipt differs")
	}
	close(probe.release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	r, e = p.Close(context.Background(), LifecycleCloseOptions{})
	if e != nil || normalizedLifecycle(t, r).Status != LifecycleSkipped {
		t.Fatal("missing response hooks differ", e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatal("response clone admission failed", calls)
	}
}
