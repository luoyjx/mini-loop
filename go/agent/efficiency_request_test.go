package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func replaceRequestField(v jsonvalue.Value, name string, value jsonvalue.Value) jsonvalue.Value {
	fields := []jsonvalue.Field{}
	found := false
	for _, key := range v.Keys() {
		item, _ := v.Lookup(key)
		if key == name {
			item = value
			found = true
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: item})
	}
	if !found {
		fields = append(fields, jsonvalue.Field{Name: name, Value: value})
	}
	return jsonvalue.ObjectValue(fields)
}

type requestAgentInput struct {
	Request     jsonvalue.Value
	Frozen      *big.Int `json:"frozen_prefix_messages"`
	OptimizedBy []string `json:"optimized_by"`
	Budget      *big.Int `json:"budget_tokens"`
}
type requestAgentLedger struct {
	Index     int
	Digest    string
	Projected protocol.Message
}
type requestAgentProbe struct {
	descriptor efficiency.Descriptor
	action     string
	inputs     *[]requestAgentInput
	calls      *[]string
}

func (p *requestAgentProbe) Descriptor() efficiency.Descriptor { return p.descriptor }
func (p *requestAgentProbe) Optimize(_ context.Context, input efficiency.OptimizationContext, options efficiency.RequestOptimizationOptions) (efficiency.RequestOptimizationResult, error) {
	c, err := input.Snapshot()
	if err != nil {
		return efficiency.RequestOptimizationResult{}, err
	}
	v, err := c.Request.Value()
	if err != nil {
		return efficiency.RequestOptimizationResult{}, err
	}
	*p.inputs = append(*p.inputs, requestAgentInput{v, c.FrozenPrefixMessages, c.OptimizedBy, options.BudgetTokens})
	*p.calls = append(*p.calls, "optimize")
	if p.action == "cancel" {
		return efficiency.RequestOptimizationResult{}, context.Canceled
	}
	m, _ := v.Lookup("messages")
	messages, _ := m.Array()
	tail := messages[len(messages)-1]
	switch p.action {
	case "drop":
		messages = messages[:len(messages)-1]
	case "role":
		messages[len(messages)-1] = replaceRequestField(replaceRequestField(tail, "role", jsonvalue.TextValue("assistant")), "content", jsonvalue.TextValue("short"))
	case "prefix":
		messages[0] = replaceRequestField(messages[0], "content", jsonvalue.TextValue("short"))
	default:
		content, _ := tail.Lookup("content")
		if _, plain := content.Text(); plain {
			content = jsonvalue.TextValue("short")
		} else {
			blocks, _ := content.Array()
			for i, block := range blocks {
				kind, _ := block.Lookup("type")
				name, _ := kind.Text()
				switch name {
				case "tool_result":
					if p.action == "tool-id" {
						block = replaceRequestField(block, "tool_use_id", jsonvalue.TextValue("bad"))
					} else {
						if p.action == "error-flag" {
							block = replaceRequestField(block, "is_error", jsonvalue.BoolValue(true))
						}
						block = replaceRequestField(block, "content", jsonvalue.TextValue("short"))
					}
				case "text":
					block = replaceRequestField(block, "text", jsonvalue.TextValue("short"))
				case "thinking":
					block = replaceRequestField(block, "thinking", jsonvalue.TextValue("short"))
				}
				blocks[i] = block
			}
			content = jsonvalue.ArrayValue(blocks)
		}
		messages[len(messages)-1] = replaceRequestField(tail, "content", content)
	}
	v = replaceRequestField(v, "messages", jsonvalue.ArrayValue(messages))
	document, err := efficiency.NewRequestDocument(v)
	if err != nil {
		return efficiency.RequestOptimizationResult{}, err
	}
	return efficiency.NewRequestOptimization(efficiency.RequestOptimizationConfig{Request: document})
}

type requestAgentCache struct{ calls *[]string }

func (c requestAgentCache) Annotate(r protocol.ModelRequest) (protocol.ModelRequest, error) {
	*c.calls = append(*c.calls, "cache")
	return r.Clone(), nil
}

type requestAgentProvider struct {
	calls     *[]string
	requests  *[][]protocol.Message
	protected bool
}

func (p requestAgentProvider) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	if p.protected && len(r.Cache.Messages) > 0 {
		return protocol.ModelReply{}, errors.New("protected message cache metadata changed")
	}
	*p.calls = append(*p.calls, "provider")
	*p.requests = append(*p.requests, r.Clone().Messages)
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

type requestAgentSink struct {
	calls    *[]string
	receipts *[]OptimizationMetrics
	rejected *[]string
}

func (s requestAgentSink) OnEvent(_ context.Context, r SessionEventRecord) error {
	if m, ok := r.Event.OptimizationReceipt(); ok {
		*s.calls = append(*s.calls, "receipt")
		m.ElapsedMS = 0
		*s.receipts = append(*s.receipts, m)
	}
	if rejection, ok := r.Event.RequestOptimizationRejected(); ok {
		*s.calls = append(*s.calls, "rejected")
		*s.rejected = append(*s.rejected, rejection.Reason)
	}
	return nil
}
func requestLedgerSnapshot(s *Session) []requestAgentLedger {
	rows := []requestAgentLedger{}
	for key, message := range s.requestProjections {
		rows = append(rows, requestAgentLedger{key.Index, hex.EncodeToString(key.Digest[:]), message})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Index < rows[j].Index })
	return rows
}
func TestAgentRequestProjectionMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-agent-request.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Action                    string
			Mode                            efficiency.Mode
			Live, Immutable, Ledger, Tamper bool
			Purpose                         protocol.RequestPurpose
			Messages                        []protocol.Message
			Inputs                          []requestAgentInput
			Requests, Histories             [][]protocol.Message
			Receipts                        []OptimizationMetrics
			Rejected, Calls                 []string
			Ledgers                         [][]requestAgentLedger
			Error                           *string
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 16 {
		t.Fatal("Source request inventory changed")
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			calls, rejected := []string{}, []string{}
			inputs, receipts := []requestAgentInput{}, []OptimizationMetrics{}
			requests, histories := [][]protocol.Message{}, [][]protocol.Message{}
			ledgers := [][]requestAgentLedger{}
			d, err := efficiency.NewDescriptor(efficiency.DefaultDescriptor("agent-request-probe", "1", efficiency.RequestContext))
			if err != nil {
				t.Fatal(err)
			}
			probe := &requestAgentProbe{d, row.Action, &inputs, &calls}
			var registry efficiency.Registry
			if _, err = registry.RegisterRequest(probe, &row.Mode); err != nil {
				t.Fatal(err)
			}
			runtime, err := registry.Runtime(efficiency.DefaultRuntimeConfig())
			if err != nil {
				t.Fatal(err)
			}
			c := runtimeConfig(t.TempDir(), requestAgentProvider{calls: &calls, requests: &requests, protected: row.Immutable})
			c.TokenEfficiency, c.CachePolicy, c.EventSink = runtime, requestAgentCache{&calls}, requestAgentSink{&calls, &receipts, &rejected}
			s, err := NewRuntimeSession(c)
			if err != nil {
				t.Fatal(err)
			}
			if row.Tamper {
				s.recovery = tamperedRequestRecovery{}
			}
			s.messages = append([]protocol.Message(nil), row.Messages...)
			call := func() error {
				messages := append([]protocol.Message(nil), row.Messages...)
				if row.Live {
					messages = append([]protocol.Message(nil), s.messages...)
				}
				r := protocol.ModelRequest{Model: DefaultModel, MaxTokens: DefaultMaxTokens, Messages: messages, Purpose: row.Purpose}
				var e error
				if row.Immutable {
					_, e = s.completeProtectedSideModel(context.Background(), r)
				} else {
					_, e = s.completeModelWithHistory(context.Background(), r, nil, row.Live)
				}
				if e == nil {
					histories = append(histories, s.Messages())
					ledgers = append(ledgers, requestLedgerSnapshot(s))
				}
				return e
			}
			err = call()
			if row.Ledger && err == nil {
				s.messages = append(s.messages, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.PlainContent("done")}, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("next long user task")})
				if err = call(); err == nil {
					s.messages[0].Content = protocol.PlainContent("replacement authoritative task")
					err = call()
				}
				if err == nil {
					s.messages = s.messages[:len(s.messages)-1]
					err = call()
				}
			}
			if row.Error != nil {
				if *row.Error != "CancelledError" || !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation differs", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			actualInputs, _ := json.Marshal(inputs)
			expectedInputs, _ := json.Marshal(row.Inputs)
			actualValue, e := jsonvalue.Decode(string(actualInputs))
			if e != nil {
				t.Fatal(e)
			}
			expectedValue, e := jsonvalue.Decode(string(expectedInputs))
			if e != nil {
				t.Fatal(e)
			}
			if canonicalProtocolValue(actualValue) != canonicalProtocolValue(expectedValue) {
				t.Fatal("optimizer input differs", string(actualInputs), string(expectedInputs))
			}
			if !reflect.DeepEqual(calls, row.Calls) || !reflect.DeepEqual(rejected, row.Rejected) {
				t.Fatal("call/guard order differs", calls, row.Calls, rejected, row.Rejected)
			}
			if !reflect.DeepEqual(requests, row.Requests) || !reflect.DeepEqual(histories, row.Histories) || !reflect.DeepEqual(ledgers, row.Ledgers) {
				t.Fatal("projection/history/ledger differs", requests, row.Requests, histories, row.Histories, ledgers, row.Ledgers)
			}
			if !reflect.DeepEqual(receipts, row.Receipts) {
				t.Fatal("receipt metrics differ", receipts, row.Receipts)
			}
			for _, record := range s.Events() {
				if record.Event.Kind() == EventRequestOptimizationRejected {
					data, err := json.Marshal(record)
					if err != nil {
						t.Fatal(err)
					}
					decoded, err := DecodeStoredEvent(data)
					if err != nil || decoded.Event.Kind() != EventRequestOptimizationRejected {
						t.Fatal("rejection replay failed", err)
					}
				}
			}
		})
	}
}
func TestMessageProtocolShapeMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-message-shape.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			InputJSON string `json:"input_json"`
			Shape     []protocolMessageShape
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 15 {
		t.Fatal("Source shape inventory changed")
	}
	for i, row := range fixture.Rows {
		v, err := jsonvalue.Decode(row.InputJSON)
		if err != nil {
			t.Fatal(err)
		}
		shape, ok := messageProtocolShape(v)
		if ok != (row.Shape != nil) || !reflect.DeepEqual(shape, row.Shape) {
			t.Fatal("shape differs", i, shape, row.Shape)
		}
	}
}

func TestRequestLoweringPreservesLargeRowsAndRejectsNontextPayloads(t *testing.T) {
	message := protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(strings.Repeat("x", protocol.MaxWireBytes+1))}
	v, err := protocolMessagesValue([]protocol.Message{message})
	if err != nil {
		t.Fatal(err)
	}
	lowered, ok := lowerRequestProjection([]protocol.Message{message}, v)
	if !ok || !reflect.DeepEqual(lowered, []protocol.Message{message}) {
		t.Fatal("accumulated row acquired an unrelated wire cap")
	}
	rows, _ := v.Array()
	rows[0] = replaceRequestField(rows[0], "content", jsonvalue.IntegerValue(1))
	if _, ok := lowerRequestProjection([]protocol.Message{message}, jsonvalue.ArrayValue(rows)); ok {
		t.Fatal("nontext payload entered typed messages")
	}
}

type tamperedRequestRecovery struct{}

func (tamperedRequestRecovery) Recover(ctx context.Context, input RecoveryInput, services RecoveryServices) (protocol.ModelReply, error) {
	r := input.Request.Clone()
	r.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewTextBlock("injected"))}}
	r.Cache.Messages = []protocol.CacheBreakpoint{{MessageIndex: 0, BlockIndex: 0, Control: protocol.CacheControl{Type: protocol.CacheEphemeral}}}
	return services.Call(ctx, r)
}

func TestRequestProjectionLiveRunKeepsCachedPrefixAndAuthority(t *testing.T) {
	calls := []string{}
	inputs := []requestAgentInput{}
	var requests []protocol.ModelRequest
	provider := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		requests = append(requests, request.Clone())
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	d, err := efficiency.NewDescriptor(efficiency.DefaultDescriptor("live-request", "1", efficiency.RequestContext))
	if err != nil {
		t.Fatal(err)
	}
	probe := &requestAgentProbe{d, "shorten", &inputs, &calls}
	var registry efficiency.Registry
	mode := efficiency.Enforce
	if _, err = registry.RegisterRequest(probe, &mode); err != nil {
		t.Fatal(err)
	}
	c := runtimeConfig(t.TempDir(), provider)
	c.TokenEfficiency, err = registry.Runtime(efficiency.DefaultRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewRuntimeSession(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"first long user task", "next long user task"} {
		if _, err = s.Run(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 2 || len(inputs) != 2 {
		t.Fatal("live requests/optimizer calls absent")
	}
	for _, request := range requests {
		text, _ := request.Messages[0].Content.Plain()
		if text != "short" || request.Cache.System == nil {
			t.Fatal("projection/cache did not reach provider")
		}
	}
	history := s.Messages()
	first, _ := history[0].Content.Plain()
	if !strings.Contains(first, "first long user task") {
		t.Fatal("projection replaced authoritative row")
	}
	if len(s.requestProjections) != 2 {
		t.Fatal("projection ledger did not retain both user deltas", len(s.requestProjections))
	}
}

func TestRequestLoweringRetainsCitationAndDefaultStreamMetadata(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-citations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Completed []struct {
			Name       string
			Normalized json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, spec := range fixture.Completed {
		t.Run(spec.Name, func(t *testing.T) {
			var block protocol.Block
			if err := json.Unmarshal(spec.Normalized, &block); err != nil {
				t.Fatal(err)
			}
			originalText, _ := block.Text()
			// The default SDK streaming output also carries an explicit null output.
			block = protocol.NewStreamedTextBlock(originalText.Text, originalText.Citations())
			original := []protocol.Message{{Role: protocol.RoleAssistant, Content: protocol.BlockContent(block)}}
			before, _ := json.Marshal(original)
			value, err := protocolMessagesValue(original)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := value.Array()
			content, _ := rows[0].Lookup("content")
			blocks, _ := content.Array()
			blocks[0] = replaceRequestField(blocks[0], "text", jsonvalue.TextValue("short"))
			rows[0] = replaceRequestField(rows[0], "content", jsonvalue.ArrayValue(blocks))
			projected, ok := lowerRequestProjection(original, jsonvalue.ArrayValue(rows))
			if !ok {
				t.Fatal("projection refused")
			}
			expected := []protocol.Message{{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewStreamedTextBlock("short", originalText.Citations()))}}
			actual, _ := json.Marshal(projected)
			want, _ := json.Marshal(expected)
			if string(actual) != string(want) {
				t.Fatalf("metadata lost: %s / %s", actual, want)
			}
			after, _ := json.Marshal(original)
			if string(before) != string(after) {
				t.Fatal("projection mutated authoritative history")
			}
		})
	}
}
