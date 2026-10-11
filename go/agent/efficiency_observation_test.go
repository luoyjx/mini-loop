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
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
)

type agentObservationInput struct {
	Observation efficiency.ObservationConfig `json:"observation"`
	Query       *string                      `json:"query"`
	Budget      *big.Int                     `json:"budget_tokens"`
}

type observationLiveExecutor string

func (e observationLiveExecutor) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	return string(e), nil
}

func TestObservationLiveRunRecoversMaskedAuthority(t *testing.T) {
	capture := &agentObservationCapture{}
	raw := "source-observation-secret-123456 " + strings.Repeat("diagnostic 中文 😀 ", 100)
	root := t.TempDir()
	store, err := efficiency.NewRawStore(efficiency.DefaultRawStoreConfig(root))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	descriptor := efficiency.DefaultDescriptor("agent-observation-probe", "1", efficiency.Observation)
	descriptor.Lossiness, descriptor.Recoverable = efficiency.Recoverable, true
	d, err := efficiency.NewDescriptor(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	var registry efficiency.Registry
	mode := efficiency.Enforce
	if _, err = registry.RegisterObservation(&agentObservationProbe{d, "reduce", "short", capture}, &mode); err != nil {
		t.Fatal(err)
	}
	runtimeConfigValue := efficiency.DefaultRuntimeConfig()
	runtimeConfigValue.RawStore = store
	runtime, err := registry.Runtime(runtimeConfigValue)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewInMemoryActionJournal(100)
	if err != nil {
		t.Fatal(err)
	}
	var session *Session
	var providerResults []string
	rounds := 0
	provider := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		rounds++
		if rounds == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("command", protocol.BashToolInput(protocol.BashInput{Command: "echo diagnostic"}))}, protocol.StopToolUse), nil
		}
		last, _ := request.Messages[len(request.Messages)-1].Content.Blocks()
		for _, block := range last {
			if result, ok := block.ToolResult(); ok {
				providerResults = append(providerResults, result.Content)
			}
		}
		if rounds == 2 {
			var ref string
			for _, record := range session.Events() {
				if m, ok := record.Event.OptimizationReceipt(); ok && m.RawRef != nil {
					ref = *m.RawRef
				}
			}
			input, err := protocol.DecodeToolInput(protocol.ToolReadTokenArtifact, []byte(`{"raw_ref":"`+ref+`"}`))
			if err != nil {
				return protocol.ModelReply{}, err
			}
			return fakeReply([]protocol.Block{protocol.NewToolUse("recover", input)}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	config := runtimeConfig(root, provider)
	config.Bash, config.TokenEfficiency, config.ActionJournal = observationLiveExecutor(raw), runtime, journal
	config.TokenArtifactTools, config.TokenArtifactStore = true, store
	config.ObservationRaw = &ObservationRawConfig{Persist: true, MinBytes: big.NewInt(1)}
	config.Secrets = literalApprovalRedactor{"source-observation-secret-123456"}
	config.MaxRounds = 4
	session, err = NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "diagnose"); err != nil {
		t.Fatal(err)
	}
	if len(capture.Inputs) != 1 || len(providerResults) != 2 || !strings.HasSuffix(providerResults[0], "\nshort") || !strings.Contains(providerResults[0], "read_token_artifact") {
		t.Fatal("live projection/recovery flow missing", capture.Inputs, providerResults)
	}
	authority := literalApprovalRedactor{"source-observation-secret-123456"}.MaskText(raw)
	if !strings.Contains(providerResults[1], authority) || strings.Contains(strings.Join(providerResults, ""), "source-observation-secret-123456") {
		t.Fatal("recovered result was reduced or unmasked", providerResults)
	}
	for _, record := range session.Events() {
		if use, ok := record.Event.ToolUse(); ok && use.ID == "command" {
			saved, exists, err := journal.Get(context.Background(), use.ActionID)
			if err != nil || !exists || saved.Result == nil || *saved.Result != authority {
				t.Fatal("journal persisted recovery projection", saved, err)
			}
		}
	}
}

func TestObservationRawBindingsAndStageErrorArchive(t *testing.T) {
	config := runtimeConfig(t.TempDir(), resourceProvider{nil})
	minimum := big.NewInt(1)
	config.ObservationRaw = &ObservationRawConfig{Persist: true, MinBytes: minimum}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	minimum.SetInt64(-1)
	if session.observationRaw.MinBytes.Int64() != 1 {
		t.Fatal("raw settings share mutable threshold")
	}
	if _, err = NewRuntimeSession(config); err == nil {
		t.Fatal("negative threshold accepted")
	}
	config.ObservationRaw = nil
	foreign, err := efficiency.NewRawStore(efficiency.DefaultRawStoreConfig(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	runtimeConfigValue := efficiency.DefaultRuntimeConfig()
	runtimeConfigValue.RawStore = foreign
	config.TokenEfficiency, err = efficiency.NewRuntime(runtimeConfigValue)
	if err != nil {
		t.Fatal(err)
	}
	session, err = NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if session.tokenEfficiency.RawStore() != nil || config.TokenEfficiency.RawStore() != foreign {
		t.Fatal("foreign raw binding not detached per session")
	}
	event := SessionEvent{kind: EventOptimizationStageError, optimizationError: OptimizationStageErrorEvent{efficiency.Observation, "tool-span", "RuntimeError"}}
	data, err := json.Marshal(SessionEventRecord{Event: event, Sequence: 1, TranscriptEpoch: 1, SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded SessionEventRecord
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if value, ok := decoded.Event.OptimizationStageError(); !ok || value != event.optimizationError {
		t.Fatal("stage error archive lost typed payload")
	}
}

type agentObservationCommand struct {
	ExitCode   *int  `json:"exit_code"`
	TimedOut   bool  `json:"timed_out"`
	Overflowed bool  `json:"overflowed"`
	DurationMS int64 `json:"duration_ms"`
}
type agentObservationResult struct {
	Name     string                   `json:"name"`
	Output   string                   `json:"output"`
	Error    bool                     `json:"error"`
	Denied   bool                     `json:"denied,omitempty"`
	Replayed bool                     `json:"replayed,omitempty"`
	Command  *agentObservationCommand `json:"command_result,omitempty"`
}
type agentObservationObserverResult struct {
	Output         string
	Denied, Failed bool
}
type agentObservationJournal struct {
	Status ActionStatus
	Result *string
}
type agentObservationRaw struct{ Ref, Content string }
type agentObservationCapture struct {
	Inputs         []agentObservationInput
	Receipts       []OptimizationMetrics
	Results        []agentObservationResult
	Observers      []agentObservationObserverResult
	Calls, Outputs []string
}
type agentObservationProbe struct {
	descriptor        efficiency.Descriptor
	action, candidate string
	capture           *agentObservationCapture
}

func (p *agentObservationProbe) Descriptor() efficiency.Descriptor { return p.descriptor }
func (p *agentObservationProbe) Reduce(_ context.Context, input efficiency.MaskedObservation, options efficiency.ReductionOptions) (efficiency.ObservationReduction, error) {
	p.capture.Calls = append(p.capture.Calls, "reduce")
	observation, err := input.Snapshot()
	if err != nil {
		return efficiency.ObservationReduction{}, err
	}
	p.capture.Inputs = append(p.capture.Inputs, agentObservationInput{observation, options.Query, options.BudgetTokens})
	if p.action == "cancel" {
		return efficiency.ObservationReduction{}, context.Canceled
	}
	if p.action == "fault" {
		return efficiency.ObservationReduction{}, &efficiency.ComponentFault{Class: "ValueError", Cause: errors.New("source-observation-secret-123456")}
	}
	warnings := []string{}
	if p.action == "secret" {
		warnings = append(warnings, "source-observation-secret-123456")
	}
	return efficiency.NewReduction(efficiency.ReductionConfig{Content: p.candidate, Warnings: warnings})
}

type agentObservationHandler struct {
	content string
	command bool
	capture *agentObservationCapture
}

func (h agentObservationHandler) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	output, _, err := h.executeDetailedTool(ctx, authority, input)
	return output, err
}
func (h agentObservationHandler) executeDetailedTool(context.Context, ToolAuthority, protocol.ToolInput) (string, *shell.Result, error) {
	h.capture.Calls = append(h.capture.Calls, "handler")
	if h.command {
		code := 7
		result := shell.Result{Stdout: h.content, ExitCode: &code, DurationMS: 12}
		return result.Render(), &result, nil
	}
	return h.content, nil, nil
}

type agentObservationObserver struct{ capture *agentObservationCapture }

func (o agentObservationObserver) OnResult(_ context.Context, _ ToolAuthority, _ ToolCall, result ToolOutcome) error {
	o.capture.Calls = append(o.capture.Calls, "observer")
	o.capture.Observers = append(o.capture.Observers, agentObservationObserverResult{result.Output, result.Denied, result.Failed})
	return nil
}

type agentObservationDenial string

func (d agentObservationDenial) BeforeTool(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
	return DenyToolCall(string(d)), nil
}

type agentObservationSink struct{ capture *agentObservationCapture }

func (s agentObservationSink) OnEvent(_ context.Context, record SessionEventRecord) error {
	if receipt, ok := record.Event.OptimizationReceipt(); ok {
		receipt.ElapsedMS = 0
		span := SpanID("tool-span")
		receipt.ParentSpanID = &span
		s.capture.Receipts = append(s.capture.Receipts, receipt)
		s.capture.Calls = append(s.capture.Calls, "receipt")
	}
	if result, ok := record.Event.ToolResult(); ok {
		name := string(result.Name)
		if protocol.IsMCPToolName(result.Name) {
			name = "probe"
		}
		var command *agentObservationCommand
		if m := result.CommandResult; m != nil {
			command = &agentObservationCommand{m.ExitCode, m.TimedOut, m.Overflowed, m.DurationMS}
		}
		s.capture.Results = append(s.capture.Results, agentObservationResult{name, result.Output, result.Failed, result.Denied, result.Replayed, command})
		s.capture.Calls = append(s.capture.Calls, "result")
	}
	return nil
}
func TestAgentObservationProjectionMatchesSource(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Name, Content, Candidate, Action                                           string
			Mode                                                                       efficiency.Mode
			Recoverable, Readonly, Persist, Install, Replay, Command, Denied, Artifact bool
			Inputs                                                                     []agentObservationInput
			Receipts                                                                   []OptimizationMetrics
			Results                                                                    []agentObservationResult
			Observers                                                                  []agentObservationObserverResult
			Calls, Outputs                                                             []string
			Journal                                                                    []agentObservationJournal
			Raw                                                                        []agentObservationRaw
			Error                                                                      *string
		}
	}
	data, err := os.ReadFile("../testdata/python-efficiency-agent-observation.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 17 {
		t.Fatal("Source observation inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			capture := &agentObservationCapture{Inputs: []agentObservationInput{}, Receipts: []OptimizationMetrics{}, Results: []agentObservationResult{}, Observers: []agentObservationObserverResult{}, Calls: []string{}, Outputs: []string{}}
			descriptor := efficiency.DefaultDescriptor("agent-observation-probe", "1", efficiency.Observation)
			descriptor.ContentTypes = []string{"text/*", "application/json"}
			if row.Recoverable {
				descriptor.Lossiness, descriptor.Recoverable = efficiency.Recoverable, true
			}
			d, err := efficiency.NewDescriptor(descriptor)
			if err != nil {
				t.Fatal(err)
			}
			var registry efficiency.Registry
			if _, err = registry.RegisterObservation(&agentObservationProbe{d, row.Action, row.Candidate, capture}, nil); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			storeConfig := efficiency.DefaultRawStoreConfig(root)
			storeConfig.MaxTotalBytes = big.NewInt(int64(len(literalApprovalRedactor{"source-observation-secret-123456"}.MaskText(row.Content)) + 8))
			storeConfig.MaxArtifactBytes = new(big.Int).Set(storeConfig.MaxTotalBytes)
			store, err := efficiency.NewRawStore(storeConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runtimeConfigValue := efficiency.DefaultRuntimeConfig()
			runtimeConfigValue.DefaultMode = row.Mode
			runtimeConfigValue.RawStore = store
			runtime, err := registry.Runtime(runtimeConfigValue)
			if err != nil {
				t.Fatal(err)
			}
			journal, err := NewInMemoryActionJournal(100)
			if err != nil {
				t.Fatal(err)
			}
			config := runtimeConfig(root, resourceProvider{nil})
			config.TokenEfficiency, config.TokenArtifactTools, config.TokenArtifactStore = runtime, row.Install, store
			config.ObservationRaw = &ObservationRawConfig{Persist: row.Persist, MinBytes: big.NewInt(1)}
			config.TokenThreshold, config.ActionJournal = 100000, journal
			config.Secrets = literalApprovalRedactor{"source-observation-secret-123456"}
			config.Hooks.Observers = []ResultObserver{agentObservationObserver{capture}}
			config.EventSink = agentObservationSink{capture}
			if row.Readonly {
				config.Mode = ModeReadonly
			}
			if row.Denied {
				config.Hooks.Before = []BeforeHook{agentObservationDenial(row.Content)}
			}
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			name := protocol.MCPToolName("source-fixture", "probe")
			schemaValue, _ := jsonvalue.Decode(`{"type":"object","properties":{}}`)
			schema, err := protocol.MCPToolSchema(name, "observation probe", schemaValue)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := NewToolDefinitionWithSchema(schema, ToolTraits{Risk: RiskRead, Readonly: true}, agentObservationHandler{row.Content, row.Command, capture})
			if err != nil {
				t.Fatal(err)
			}
			definitions := []ToolDefinition{definition}
			if row.Install {
				recovery, ok := session.gate.catalog.Lookup(protocol.ToolReadTokenArtifact)
				if !ok {
					t.Fatal("missing recovery tool")
				}
				definitions = append(definitions, recovery)
			}
			session.gate.catalog, err = NewToolCatalog(definitions...)
			if err != nil {
				t.Fatal(err)
			}
			arguments, _ := jsonvalue.Decode(`{"value":"source-observation-secret-123456","nested":{"n":1.0}}`)
			input, err := protocol.MCPToolInput(name, arguments)
			if err != nil {
				t.Fatal(err)
			}
			if row.Artifact {
				input, err = protocol.DecodeToolInput(protocol.ToolReadTokenArtifact, []byte(`{"raw_ref":"raw_missing"}`))
				if err != nil {
					t.Fatal(err)
				}
			}
			run, err := DefaultRunContext()
			if err != nil {
				t.Fatal(err)
			}
			session.lastModelSpan = "model-span"
			repeat := 1
			if row.Replay {
				repeat = 2
			}
			for i := 0; i < repeat; i++ {
				outcome, e := session.dispatchTool(context.Background(), run, protocol.ToolUseBlock{ID: "tool-1", Name: input.Name(), Input: input})
				err = e
				if err != nil {
					break
				}
				capture.Outputs = append(capture.Outputs, outcome.Output)
			}
			if row.Error != nil {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("Source cancellation differs", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			action, err := ToolActionID(session.id, run, ToolCall{ID: "tool-1", Input: input})
			if err != nil {
				t.Fatal(err)
			}
			record, exists, err := journal.Get(context.Background(), action)
			if err != nil {
				t.Fatal(err)
			}
			journals := []agentObservationJournal{}
			if exists {
				journals = append(journals, agentObservationJournal{record.Status, record.Result})
			}
			if !reflect.DeepEqual(journals, row.Journal) {
				t.Fatal("journal authority differs", journals, row.Journal)
			}
			// Normalize only the opaque random ref; preserve its length and full envelope.
			raw := []agentObservationRaw{}
			for i := range capture.Receipts {
				ref := capture.Receipts[i].RawRef
				if ref == nil {
					continue
				}
				authoritative, e := store.GetMasked(*ref)
				if e != nil {
					t.Fatal(e)
				}
				sourceRef := "raw_" + strings.Repeat("A", 43)
				raw = append(raw, agentObservationRaw{sourceRef, authoritative})
				for j := range capture.Outputs {
					capture.Outputs[j] = strings.ReplaceAll(capture.Outputs[j], *ref, sourceRef)
				}
				for j := range capture.Results {
					capture.Results[j].Output = strings.ReplaceAll(capture.Results[j].Output, *ref, sourceRef)
				}
				for j := range capture.Observers {
					capture.Observers[j].Output = strings.ReplaceAll(capture.Observers[j].Output, *ref, sourceRef)
				}
				capture.Receipts[i].RawRef = &sourceRef
			}
			if !reflect.DeepEqual(raw, row.Raw) {
				t.Fatal("masked raw artifacts differ", raw, row.Raw)
			}
			if row.Name == "recovery-envelope-inflation" {
				if _, err := store.PutMasked(row.Content); err != nil {
					t.Fatal("unused inflation artifact was retained", err)
				}
			}
			if !reflect.DeepEqual(capture.Inputs, row.Inputs) {
				t.Fatal("post-mask reducer inputs differ", capture.Inputs, row.Inputs)
			}
			if !reflect.DeepEqual(capture.Receipts, row.Receipts) {
				t.Fatal("final envelope receipt metrics differ", capture.Receipts, row.Receipts)
			}
			if !reflect.DeepEqual(capture.Calls, row.Calls) || !reflect.DeepEqual(capture.Outputs, row.Outputs) || !reflect.DeepEqual(capture.Results, row.Results) || !reflect.DeepEqual(capture.Observers, row.Observers) {
				t.Fatal("projection/observer/call order differs", capture, row)
			}
		})
	}
}
