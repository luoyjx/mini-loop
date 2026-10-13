package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
)

type callerObservationAgentFixture struct {
	Runs []callerObservationAgentRun `json:"agent_runs"`
}

type callerObservationAgentRun struct {
	Name              string
	Response          string                         `json:"response_json"`
	Trace             []string                       `json:"trace"`
	Calls             []callerObservationHandlerCall `json:"handler_calls"`
	Result            string                         `json:"result_json"`
	History           string                         `json:"history_json"`
	Events            string                         `json:"events_json"`
	Requests          string                         `json:"requests_json"`
	Final             string                         `json:"final"`
	ErrorClass        *string                        `json:"error_class"`
	ErrorMessage      *string                        `json:"error_message"`
	TransportRequests int                            `json:"transport_requests"`
}

type callerObservationHandlerCall struct {
	ActionID string             `json:"action_id"`
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Mode     string             `json:"permission_mode"`
	Context  RunContextSnapshot `json:"run_context"`
	Value    string             `json:"value"`
}

type callerObservationResult struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Output string `json:"output"`
	Denied bool   `json:"denied"`
	Failed bool   `json:"failed"`
}

type callerObservationProbeInput struct{ Value string }

func (input callerObservationProbeInput) Validate() error {
	if input.Value != "safe-value" {
		return errors.New("safe probe requires the fixed fixture value")
	}
	return nil
}

type callerObservationProbeCodec struct{}

func (callerObservationProbeCodec) DecodeExtensionInput(_ context.Context, input protocol.ToolInput) (callerObservationProbeInput, error) {
	data, err := input.MarshalJSON()
	if err != nil {
		return callerObservationProbeInput{}, err
	}
	var decoded struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return callerObservationProbeInput{}, err
	}
	return callerObservationProbeInput{Value: decoded.Value}, nil
}

type callerObservationProbeHandler struct {
	trace       *[]string
	calls       []callerObservationHandlerCall
	authorities []ToolAuthority
}

func (handler *callerObservationProbeHandler) ExecuteExtensionTool(_ context.Context, authority ToolAuthority, input callerObservationProbeInput) (string, error) {
	*handler.trace = append(*handler.trace, "handler")
	handler.authorities = append(handler.authorities, authority)
	handler.calls = append(handler.calls, callerObservationHandlerCall{
		ActionID: string(authority.ActionID), ID: authority.ToolUseID, Name: "safe_probe",
		Mode: string(authority.Mode), Context: authority.RunContext.Snapshot(), Value: input.Value,
	})
	return "safe output", nil
}

type callerObservationTransport func(*http.Request) (*http.Response, error)

func (transport callerObservationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type callerObservationEventSink struct{ trace *[]string }

func (sink callerObservationEventSink) OnEvent(_ context.Context, record SessionEventRecord) error {
	switch record.Event.Kind() {
	case EventToolUse, EventToolResult, EventError:
		*sink.trace = append(*sink.trace, string(record.Event.Kind()))
	}
	return nil
}

type callerObservationSystem struct{}

func (callerObservationSystem) BuildSystem(SystemContext) (string, error) {
	return "Caller observation fixture.", nil
}

func callerObservationAgentCanonical(t *testing.T, raw string) string {
	t.Helper()
	value, err := jsonvalue.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := value.Sorted().MarshalLegacyUTF8()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSourceCallerObservationsThroughActualAgent(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-caller-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture callerObservationAgentFixture
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Runs) != 3 {
		t.Fatal("Source actual Agent inventory changed", err, len(fixture.Runs))
	}
	for _, row := range fixture.Runs {
		t.Run(row.Name, func(t *testing.T) {
			trace := []string{}
			requests := []jsonvalue.Value{}
			transport := callerObservationTransport(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				value, err := jsonvalue.Decode(string(body))
				if err != nil {
					return nil, err
				}
				requests = append(requests, value)
				trace = append(trace, "transport")
				response := row.Response
				if len(requests) > 1 {
					response = `{"id":"caller-done","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":1}}`
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
			})
			client, err := provider.New(provider.Config{APIKey: "fixture-key", BaseURL: "https://caller-fixture.invalid", HTTPClient: &http.Client{Transport: transport}})
			if err != nil {
				t.Fatal(err)
			}
			schema, err := protocol.ExtensionToolSchema("safe_probe", "Safe observation fixture", []byte(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`))
			if err != nil {
				t.Fatal(err)
			}
			handler := &callerObservationProbeHandler{trace: &trace}
			definition, err := NewTypedExtensionTool(schema, ToolTraits{Risk: RiskRead, Readonly: true}, callerObservationProbeCodec{}, handler)
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := NewToolCatalog(definition)
			if err != nil {
				t.Fatal(err)
			}
			bindings := []ToolAuthority{}
			results := []callerObservationResult{}
			gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(_ context.Context, authority ToolAuthority, call ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					bindings = append(bindings, authority)
					if call.ID != "actual-use" || call.Name() != "safe_probe" {
						return BeforeDecision{}, errors.New("caller observation replaced dispatched identity")
					}
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(_ context.Context, authority ToolAuthority, _ ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					bindings = append(bindings, authority)
					return "", false, nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, authority ToolAuthority, _ ToolCall, output string) (string, error) {
					trace = append(trace, "after")
					bindings = append(bindings, authority)
					return output, nil
				})},
				Observers: []ResultObserver{observerFunc(func(_ context.Context, authority ToolAuthority, call ToolCall, outcome ToolOutcome) error {
					trace = append(trace, "result")
					bindings = append(bindings, authority)
					results = append(results, callerObservationResult{ID: call.ID, Name: string(call.Name()), Output: outcome.Output, Denied: outcome.Denied, Failed: outcome.Failed})
					return nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			session, err := NewSessionWithGate("caller-fixture", "trusted-owner", client, gate, ModeAuto, "", 2)
			if err != nil {
				t.Fatal(err)
			}
			session.label, session.model, session.maxTokens = "main", "fixture-model", 8
			session.systemBuilder = callerObservationSystem{}
			session.events.sink = callerObservationEventSink{trace: &trace}
			actor := ActorID("fixture-peer")
			run, err := PeerAgentRunContext("fixture-parent", PeerRunConfig{ActorID: &actor})
			if err != nil {
				t.Fatal(err)
			}
			// Match Source's sole identity normalization after the public trusted
			// constructor. No caller observation enters this live authority.
			run.messageID = "fixture-message"
			answer, err := session.RunWithContext(context.Background(), "run the safe fixture", run)
			if err != nil || row.ErrorClass != nil || row.ErrorMessage != nil || answer != row.Final {
				t.Fatal("actual Agent completion changed", answer, err, row.Final)
			}
			if !reflect.DeepEqual(trace, row.Trace) || len(requests) != row.TransportRequests {
				t.Fatal("handler/encoding boundary ordering changed", trace, row.Trace, len(requests), row.TransportRequests)
			}
			gotCalls, err := json.Marshal(handler.calls)
			if err != nil {
				t.Fatal(err)
			}
			wantCalls, err := json.Marshal(row.Calls)
			if err != nil || callerObservationAgentCanonical(t, string(gotCalls)) != callerObservationAgentCanonical(t, string(wantCalls)) {
				t.Fatal("safe handler identity or authority differs", string(gotCalls), string(wantCalls), err)
			}
			if len(handler.calls) != 1 {
				t.Fatal("caller observation changed safe handler execution count", len(handler.calls))
			}
			for _, authority := range append(bindings, handler.authorities...) {
				if authority.SessionID != "caller-fixture" || authority.OwnerID != "trusted-owner" || authority.Workspace != "" || authority.Mode != ModeAuto || authority.ToolUseID != "actual-use" || string(authority.ActionID) != row.Calls[0].ActionID || authority.Scope.Label != "main" || authority.Scope.Depth != 0 {
					t.Fatal("caller observation replaced trusted binding", authority)
				}
				if !reflect.DeepEqual(authority.RunContext.Snapshot(), run.Snapshot()) || authority.RunContext.Authority() != AuthorityPeerAgent || authority.RunContext.Allows(CapabilityWorkflowLaunch) {
					t.Fatal("caller observation acquired live authority")
				}
			}
			gotResults, err := json.Marshal(results)
			if err != nil || callerObservationAgentCanonical(t, string(gotResults)) != callerObservationAgentCanonical(t, row.Result) {
				t.Fatal("Source result observer differs", string(gotResults), row.Result, err)
			}
			history := session.Messages()
			if err := protocol.ValidateTranscript(history); err != nil {
				t.Fatal("caller observation broke actual tool-use pairing", err)
			}
			gotHistory, err := protocol.ObservedMessagesPythonJSON(history, false, true)
			if err != nil || callerObservationAgentCanonical(t, gotHistory) != callerObservationAgentCanonical(t, row.History) {
				t.Fatal("actual Source history differs", gotHistory, row.History, err)
			}
			gotRequests, err := jsonvalue.ArrayValue(requests).MarshalJSON()
			if err != nil || callerObservationAgentCanonical(t, string(gotRequests)) != callerObservationAgentCanonical(t, row.Requests) {
				t.Fatal("actual SDK request transcript differs", string(gotRequests), row.Requests, err)
			}
			assertCallerObservationAgentEvents(t, session.Events(), row, run)
			// Exercise the real archival masker, including nonfinite caller
			// extension values, and prove recording never mutates live history.
			mask := func(s string) string { return strings.ReplaceAll(s, "secret", "masked") }
			for _, message := range history {
				original, err := message.MarshalArchiveJSON()
				if err != nil {
					t.Fatal(err)
				}
				masked, err := maskedMessageArchive(message, mask)
				if err != nil {
					t.Fatal(err)
				}
				value, err := jsonvalue.Decode(string(original))
				if err != nil {
					t.Fatal(err)
				}
				want, err := value.MapStrings(mask).MarshalLegacyUTF8()
				if err != nil || callerObservationAgentCanonical(t, string(masked)) != callerObservationAgentCanonical(t, string(want)) {
					t.Fatal("archive mask lost caller root/nonfinite values", string(masked), err)
				}
			}
			afterMask, err := protocol.ObservedMessagesPythonJSON(session.Messages(), false, true)
			if err != nil || callerObservationAgentCanonical(t, afterMask) != callerObservationAgentCanonical(t, row.History) {
				t.Fatal("recording mutated live caller observations", err)
			}
		})
	}
}

func assertCallerObservationAgentEvents(t *testing.T, records []SessionEventRecord, row callerObservationAgentRun, run RunContext) {
	t.Helper()
	var sourceEvents []struct {
		Type      string `json:"type"`
		Agent     string `json:"agent"`
		Depth     int    `json:"depth"`
		MessageID string `json:"message_id"`
		Name      string `json:"name"`
		ID        string `json:"id"`
		ActionID  string `json:"action_id"`
		Output    string `json:"output"`
		Error     bool   `json:"error"`
	}
	// The complete capture also contains recovery.error text. Only decode the
	// paired tool lifecycle here; its error field is the actual boolean result.
	value, err := jsonvalue.Decode(row.Events)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := value.Array()
	if !ok {
		t.Fatal("Source event capture is not an array")
	}
	selected := []jsonvalue.Value{}
	for _, item := range items {
		kind, _ := item.Lookup("type")
		name, _ := kind.Text()
		if name == "tool_use" || name == "tool_result" {
			selected = append(selected, item)
		}
	}
	data, err := jsonvalue.ArrayValue(selected).MarshalJSON()
	if err != nil || json.Unmarshal(data, &sourceEvents) != nil || len(sourceEvents) != 2 {
		t.Fatal("Source paired lifecycle shape changed", err)
	}
	uses := eventRecordsOfKind(records, EventToolUse)
	results := eventRecordsOfKind(records, EventToolResult)
	if len(uses) != 1 || len(results) != 1 {
		t.Fatal("observed metadata changed tool lifecycle counts")
	}
	use, _ := uses[0].Event.ToolUse()
	result, _ := results[0].Event.ToolResult()
	if use.ID != sourceEvents[0].ID || string(use.Name) != sourceEvents[0].Name || string(use.ActionID) != sourceEvents[0].ActionID || result.ID != sourceEvents[1].ID || string(result.Name) != sourceEvents[1].Name || string(result.ActionID) != sourceEvents[1].ActionID || result.Output != sourceEvents[1].Output || result.Failed != sourceEvents[1].Error || result.Denied || use.SpanID != result.SpanID {
		t.Fatal("caller metadata changed paired event identity or outcome", use, result)
	}
	for i, event := range []SessionEventRecord{uses[0], results[0]} {
		want := sourceEvents[i]
		if event.SessionID != "caller-fixture" || event.Scope.Label != want.Agent || event.Scope.Depth != want.Depth || string(event.Scope.RunContext.MessageID()) != want.MessageID || !reflect.DeepEqual(event.Scope.RunContext.Snapshot(), run.Snapshot()) {
			t.Fatal("caller metadata changed trusted event scope")
		}
	}
}
