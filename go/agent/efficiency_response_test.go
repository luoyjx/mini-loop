package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type agentResponseCall struct {
	Phase     string
	System    *string
	MaxTokens int `json:"max_tokens"`
	Messages  []protocol.Message
}
type agentResponseInput struct {
	Task         string                            `json:"task"`
	Settings     efficiency.ResponseSettingsConfig `json:"settings"`
	Capabilities []string                          `json:"provider_capabilities"`
	Budget       *big.Int                          `json:"budget_tokens"`
	Concise      bool                              `json:"concise_requested"`
	AppliedBy    []string                          `json:"applied_by"`
}
type agentResponseProbe struct {
	planner    efficiency.ResponsePlanner
	descriptor efficiency.Descriptor
	action     string
	calls      *[]agentResponseCall
	inputs     *[]agentResponseInput
}

func (p *agentResponseProbe) Descriptor() efficiency.Descriptor { return p.descriptor }
func (p *agentResponseProbe) Plan(ctx context.Context, c efficiency.ResponseContext) (efficiency.ResponsePlan, error) {
	v, err := c.Snapshot()
	if err != nil {
		return efficiency.ResponsePlan{}, err
	}
	s, err := v.Settings.Snapshot()
	if err != nil {
		return efficiency.ResponsePlan{}, err
	}
	*p.inputs = append(*p.inputs, agentResponseInput{v.Task, s, v.ProviderCapabilities, v.BudgetTokens, v.ConciseRequested, v.AppliedBy})
	*p.calls = append(*p.calls, agentResponseCall{Phase: "policy"})
	switch p.action {
	case "failure":
		return efficiency.ResponsePlan{}, &efficiency.ComponentFault{Class: "RuntimeError", Cause: errors.New("private response fault")}
	case "cancel":
		return efficiency.ResponsePlan{}, context.Canceled
	case "instructions":
		s, err := efficiency.NewResponseSettings(efficiency.ResponseSettingsConfig{Instructions: []string{"first", "second"}, MaxOutputTokens: big.NewInt(900)})
		if err != nil {
			return efficiency.ResponsePlan{}, err
		}
		return efficiency.NewResponsePlan(efficiency.ResponsePlanConfig{Settings: s, Warnings: []string{"bounded_warning"}})
	default:
		return p.planner.Plan(ctx, c)
	}
}

type agentResponseCache struct{ calls *[]agentResponseCall }

func (p agentResponseCache) Annotate(r protocol.ModelRequest) (protocol.ModelRequest, error) {
	*p.calls = append(*p.calls, agentResponseCall{Phase: "cache", System: clonePointer(r.System)})
	return r.Clone(), nil
}

type agentResponseProvider struct{ calls *[]agentResponseCall }

func (p agentResponseProvider) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	*p.calls = append(*p.calls, agentResponseCall{Phase: "provider", System: clonePointer(r.System), MaxTokens: r.MaxTokens, Messages: r.Clone().Messages})
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

type agentResponseSink struct{ calls *[]agentResponseCall }

func (s agentResponseSink) OnEvent(_ context.Context, record SessionEventRecord) error {
	if record.Event.Kind() == EventOptimizationReceipt {
		*s.calls = append(*s.calls, agentResponseCall{Phase: "receipt"})
	}
	return nil
}

func TestAgentResponseAdoptionMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-agent-response.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Action string
			Purpose      protocol.RequestPurpose
			System       *string
			Mode         efficiency.Mode
			OptIn        bool `json:"opt_in"`
			RequireOptIn bool `json:"require_opt_in"`
			Maximum      *int
			Cap          *big.Int
			Messages     []protocol.Message
			Inputs       []agentResponseInput
			Calls        []agentResponseCall
			Receipts     []OptimizationMetrics
			Error        *string
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 14 {
		t.Fatal("Source Agent response inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			calls, inputs := []agentResponseCall{}, []agentResponseInput{}
			policyConfig := efficiency.DefaultConciseResponseConfig()
			policyConfig.RequireOptIn, policyConfig.MaxOutputTokens = row.RequireOptIn, row.Cap
			policy, e := efficiency.NewConciseResponsePlanner(policyConfig)
			if e != nil {
				t.Fatal(e)
			}
			d := policy.Descriptor()
			if row.Action != "concise" {
				d, e = efficiency.NewDescriptor(efficiency.DefaultDescriptor("agent-response-probe", "1", efficiency.ResponsePolicy))
				if e != nil {
					t.Fatal(e)
				}
			}
			probe := &agentResponseProbe{planner: policy, descriptor: d, action: row.Action, calls: &calls, inputs: &inputs}
			var registry efficiency.Registry
			if _, e = registry.RegisterResponse(probe, &row.Mode); e != nil {
				t.Fatal(e)
			}
			runtime, e := registry.Runtime(efficiency.DefaultRuntimeConfig())
			if e != nil {
				t.Fatal(e)
			}
			c := runtimeConfig(t.TempDir(), agentResponseProvider{&calls})
			c.TokenEfficiency, c.ConciseResponse, c.CachePolicy, c.EventSink = runtime, row.OptIn, agentResponseCache{&calls}, agentResponseSink{&calls}
			session, e := NewRuntimeSession(c)
			if e != nil {
				t.Fatal(e)
			}
			maximum := DefaultMaxTokens
			if row.Maximum != nil {
				maximum = *row.Maximum
			}
			request := protocol.ModelRequest{Model: DefaultModel, MaxTokens: maximum, Messages: append([]protocol.Message(nil), row.Messages...), System: clonePointer(row.System), Purpose: row.Purpose}
			before, e := json.Marshal(struct {
				System   *string
				Messages []protocol.Message
			}{request.System, request.Messages})
			if e != nil {
				t.Fatal(e)
			}
			_, e = session.completeSideModel(context.Background(), request)
			if row.Error != nil {
				if *row.Error != "CancelledError" || !errors.Is(e, context.Canceled) {
					t.Fatal("cancellation differs", e)
				}
			} else if e != nil {
				t.Fatal(e)
			}
			after, _ := json.Marshal(struct {
				System   *string
				Messages []protocol.Message
			}{request.System, request.Messages})
			if string(before) != string(after) {
				t.Fatal("caller request mutated")
			}
			if !reflect.DeepEqual(inputs, row.Inputs) {
				t.Fatal("policy context differs", inputs, row.Inputs)
			}
			if !reflect.DeepEqual(calls, row.Calls) {
				t.Fatal("policy/cache/provider order differs", calls, row.Calls)
			}
			receipts := []OptimizationMetrics{}
			for _, record := range session.Events() {
				if metrics, ok := record.Event.OptimizationReceipt(); ok {
					metrics.ElapsedMS = 0
					receipts = append(receipts, metrics)
					encoded, e := json.Marshal(record)
					if e != nil {
						t.Fatal(e)
					}
					for _, forbidden := range []string{"input_digest", "output_digest", "raw_digest", "\"warnings\"", "bounded_warning", "private response fault"} {
						if strings.Contains(string(encoded), forbidden) {
							t.Fatal("private receipt fields reached event", string(encoded))
						}
					}
					stored, e := DecodeStoredEvent(encoded)
					if e != nil {
						t.Fatal(e)
					}
					replayed, ok := stored.Event.OptimizationReceipt()
					if !ok {
						t.Fatal("archival event discriminator lost")
					}
					replayed.ElapsedMS = 0
					if !reflect.DeepEqual(replayed, metrics) {
						t.Fatal("archival receipt differs")
					}
				}
			}
			if !reflect.DeepEqual(receipts, row.Receipts) {
				t.Fatal("receipt metrics differ", receipts, row.Receipts)
			}
		})
	}
}

func TestOptimizationEventDetachedMetricsAndStoredValidation(t *testing.T) {
	n := new(big.Int).Exp(big.NewInt(10), big.NewInt(100), nil)
	r := efficiency.ReceiptProjection{ReceiptConfig: efficiency.ReceiptConfig{ComponentID: "probe", ComponentVersion: "1", Stage: efficiency.ResponsePolicy, Mode: efficiency.Enforce, Status: efficiency.Applied, Reason: "policy_applied", RawBytes: n, ProjectedBytes: big.NewInt(1), TokensBeforeEstimate: n, TokensAfterEstimate: big.NewInt(1), Lossiness: efficiency.Lossless, Warnings: []string{"bounded_warning"}}}
	metrics := optimizationMetrics(r)
	n.SetInt64(0)
	event := SessionEvent{kind: EventOptimizationReceipt, optimizationReceipt: metrics}
	got, _ := event.OptimizationReceipt()
	got.RawBytes.SetInt64(0)
	copy := event.clone()
	copy.optimizationReceipt.TokensBeforeEstimate.SetInt64(0)
	if event.optimizationReceipt.RawBytes.Sign() == 0 || event.optimizationReceipt.TokensBeforeEstimate.Sign() == 0 {
		t.Fatal("metric snapshots aliased source/event")
	}
	if err := metrics.validateStored(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*OptimizationMetrics){
		func(m *OptimizationMetrics) { m.RawBytes = nil },
		func(m *OptimizationMetrics) { m.TokensAfterEstimate = big.NewInt(-1) },
		func(m *OptimizationMetrics) { m.WarningCount = -1 },
		func(m *OptimizationMetrics) { m.Mode = "unknown" },
	} {
		invalid := metrics.clone()
		mutate(&invalid)
		if invalid.validateStored() == nil {
			t.Fatal("invalid archive metric accepted")
		}
	}
}

func TestExplicitResponseRuntimeReachesLiveRunAndDefaultCache(t *testing.T) {
	var requests []protocol.ModelRequest
	provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		requests = append(requests, r.Clone())
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	c := runtimeConfig(t.TempDir(), provider)
	policyConfig := efficiency.DefaultConciseResponseConfig()
	policyConfig.RequireOptIn = false
	policy, err := efficiency.NewConciseResponsePlanner(policyConfig)
	if err != nil {
		t.Fatal(err)
	}
	var registry efficiency.Registry
	mode := efficiency.Enforce
	if _, err = registry.RegisterResponse(policy, &mode); err != nil {
		t.Fatal(err)
	}
	c.TokenEfficiency, err = registry.Runtime(efficiency.DefaultRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"Explain", "Again"} {
		if _, err = session.Run(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 2 {
		t.Fatal("live calls absent")
	}
	for _, request := range requests {
		if request.MaxTokens != 1200 || request.System == nil || !strings.Contains(*request.System, "Answer concisely") || request.Cache.System == nil {
			t.Fatal("live policy did not precede default cache", request)
		}
		if strings.Count(*request.System, policyConfig.Instruction) != 1 {
			t.Fatal("policy instruction accumulated across turns")
		}
	}
	history, _ := json.Marshal(session.Messages())
	if strings.Contains(string(history), "Answer concisely") || strings.Contains(string(history), "cache_control") {
		t.Fatal("response/cache projections entered authoritative history")
	}
}

type optimizationMask string

func (m optimizationMask) MaskText(text string) string {
	return strings.ReplaceAll(text, string(m), "MASK")
}

func TestOptimizationEventMaskingAndArchivalProjection(t *testing.T) {
	metrics := OptimizationMetrics{ComponentID: "private-probe", ComponentVersion: "1", Stage: efficiency.ResponsePolicy, Mode: efficiency.Enforce, Status: efficiency.Applied, Reason: "private_reason", RawBytes: big.NewInt(1), ProjectedBytes: big.NewInt(1), TokensBeforeEstimate: big.NewInt(1), TokensAfterEstimate: big.NewInt(1), Lossiness: efficiency.Lossless}
	ref, parent := "private-ref", SpanID("private-parent")
	metrics.RawRef, metrics.ParentSpanID = &ref, &parent
	event := maskedEvent(optimizationMask("private"), SessionEvent{kind: EventOptimizationReceipt, optimizationReceipt: metrics})
	encoded, err := json.Marshal(SessionEventRecord{Event: event, SessionID: "session", Sequence: 1, TranscriptEpoch: 1})
	if err != nil || strings.Contains(string(encoded), "private") {
		t.Fatal("metric strings escaped masking", string(encoded), err)
	}
	stored, err := DecodeStoredEvent(encoded)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := stored.Event.OptimizationReceipt()
	if !ok || got.ComponentID != "MASK-probe" || *got.RawRef != "MASK-ref" || *got.ParentSpanID != "MASK-parent" {
		t.Fatal("masked archival metrics lost", got)
	}
	if metrics.ComponentID != "private-probe" || *metrics.RawRef != "private-ref" {
		t.Fatal("masking mutated caller metrics")
	}
}
