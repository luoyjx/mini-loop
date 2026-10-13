package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type batchSlotInput struct{ Slot int }

func (batchSlotInput) Validate() error { return nil }

type batchSlotCodec struct{}

func (batchSlotCodec) DecodeExtensionInput(_ context.Context, input protocol.ToolInput) (batchSlotInput, error) {
	raw, err := input.MarshalJSON()
	if err != nil {
		return batchSlotInput{}, err
	}
	value, err := jsonvalue.Decode(string(raw))
	if err != nil {
		return batchSlotInput{}, err
	}
	slot, ok := value.Lookup("slot")
	if !ok {
		return batchSlotInput{}, errors.New("missing slot")
	}
	number, ok := slot.Integer()
	if !ok {
		return batchSlotInput{}, errors.New("slot must be integer")
	}
	index, err := strconv.Atoi(number)
	return batchSlotInput{index}, err
}

type batchSlotHandler struct {
	mu      sync.Mutex
	bad     int
	effects []int
}

func (handler *batchSlotHandler) ExecuteExtensionTool(_ context.Context, _ ToolAuthority, input batchSlotInput) (string, error) {
	handler.mu.Lock()
	handler.effects = append(handler.effects, input.Slot)
	handler.mu.Unlock()
	if input.Slot == handler.bad {
		return string([]byte{0xed, 0xa0, 0x80}), nil
	}
	return fmt.Sprintf("output-%d", input.Slot), nil
}

type batchSlotProvider struct {
	modes    []string
	requests int
}

func (provider *batchSlotProvider) Complete(_ context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests++
	if provider.requests > 1 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	blocks := make([]protocol.Block, len(provider.modes))
	for i, mode := range provider.modes {
		input, err := protocol.DecodeModelToolInput(protocol.ToolName(mode), []byte(fmt.Sprintf(`{"slot":%d}`, i)))
		if err != nil {
			return protocol.ModelReply{}, err
		}
		blocks[i] = protocol.NewToolUse(fmt.Sprintf("id-%d", i), input)
	}
	return fakeReply(blocks, protocol.StopToolUse), nil
}

func TestMixedBatchStepAdmissionMatchesActualSource(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-batch-step-boundary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name               string
			Modes              []string
			Bad, Capacity      int
			Error              *string
			Output             string
			Effects            []int
			ResultEventIDs     []string `json:"result_event_ids"`
			ToolEventIDs       []string `json:"tool_event_ids"`
			ModelStarts        int      `json:"model_starts"`
			Requests, Messages int
			ResultIDs          []string `json:"result_ids"`
			ResultTextJSON     []string `json:"result_text_json"`
			Steps              []ToolStep
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 32 {
		t.Fatal("Source profile count changed", len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			handler := &batchSlotHandler{bad: row.Bad, effects: []int{}}
			definitions := []ToolDefinition{}
			for _, mode := range []string{"exclusive", "parallel"} {
				definition, err := NewTypedExtensionTool(protocol.ToolSchema{Name: protocol.ToolName(mode), Description: "Batch boundary", InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}, ToolTraits{Risk: RiskRead, Readonly: true, ParallelSafe: mode == "parallel"}, batchSlotCodec{}, handler)
				if err != nil {
					t.Fatal(err)
				}
				definitions = append(definitions, definition)
			}
			catalog, err := NewToolCatalog(definitions...)
			if err != nil {
				t.Fatal(err)
			}
			gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
			if err != nil {
				t.Fatal(err)
			}
			provider := &batchSlotProvider{modes: row.Modes}
			session, err := NewSessionWithGate("s", "owner", provider, gate, ModeAuto, "", 2)
			if err != nil {
				t.Fatal(err)
			}
			session.toolLimiter, err = NewConcurrencyLimiter(ConcurrencyLimit(row.Capacity))
			if err != nil {
				t.Fatal(err)
			}
			output, runErr := session.Run(context.Background(), "batch")
			if row.Error == nil {
				if runErr != nil || output != row.Output {
					t.Fatal("successful batch differs", output, runErr, row.Output)
				}
			} else if *row.Error != "UnicodeEncodeError" || !errors.Is(runErr, protocol.ErrResultUTF8) {
				t.Fatal("batch hash failure differs", runErr, row.Error)
			}
			if provider.requests != row.Requests || len(session.Messages()) != row.Messages {
				t.Fatal("request/transcript admission differs", provider.requests, len(session.Messages()), row.Requests, row.Messages)
			}
			handler.mu.Lock()
			effects := append([]int{}, handler.effects...)
			handler.mu.Unlock()
			wantEffects := append([]int{}, row.Effects...)
			sort.Ints(effects)
			sort.Ints(wantEffects)
			allExclusive := true
			for _, mode := range row.Modes {
				allExclusive = allExclusive && mode == "exclusive"
			}
			if allExclusive && !reflect.DeepEqual(handler.effects, row.Effects) {
				t.Fatal("exclusive effects reordered", handler.effects, row.Effects)
			}
			if !reflect.DeepEqual(effects, wantEffects) {
				t.Fatal("executed effects differ", effects, wantEffects)
			}
			toolIDs, resultEvents, resultIDs, resultTexts := []string{}, []string{}, []string{}, []string{}
			starts := 0
			for _, record := range session.Events() {
				event := record.Event
				if _, ok := event.ModelStart(); ok {
					starts++
				}
				if use, ok := event.ToolUse(); ok {
					toolIDs = append(toolIDs, use.ID)
				}
				if result, ok := event.ToolResult(); ok {
					resultEvents = append(resultEvents, result.ID)
				}
			}
			wantEvents := append([]string{}, row.ResultEventIDs...)
			sort.Strings(resultEvents)
			sort.Strings(wantEvents)
			if !reflect.DeepEqual(toolIDs, row.ToolEventIDs) || !reflect.DeepEqual(resultEvents, wantEvents) || starts != row.ModelStarts {
				t.Fatal("model/tool/result event phase differs", toolIDs, row.ToolEventIDs, resultEvents, wantEvents, starts, row.ModelStarts)
			}
			for _, message := range session.Messages() {
				blocks, _ := message.Content.Blocks()
				for _, block := range blocks {
					if result, ok := block.ToolResult(); ok {
						resultIDs = append(resultIDs, result.ToolUseID)
						encoded, err := json.Marshal(result.Content)
						if err != nil {
							t.Fatal(err)
						}
						resultTexts = append(resultTexts, string(encoded))
					}
				}
			}
			if !reflect.DeepEqual(resultIDs, row.ResultIDs) {
				t.Fatal("result batch differs", resultIDs, row.ResultIDs)
			}
			if !reflect.DeepEqual(resultTexts, row.ResultTextJSON) {
				t.Fatal("result text differs", resultTexts, row.ResultTextJSON)
			}
			steps := append([]ToolStep{}, session.recentSteps...)
			if !reflect.DeepEqual(steps, row.Steps) {
				t.Fatal("batch-local ledger admission differs", steps, row.Steps)
			}
		})
	}
}

type cancelBatchResultObserver struct{ cancel context.CancelFunc }

func (observer cancelBatchResultObserver) OnResult(context.Context, ToolAuthority, ToolCall, ToolOutcome) error {
	observer.cancel()
	return nil
}

func TestCancelledBatchHashFailureRetainsCancellationRepair(t *testing.T) {
	for _, mode := range []string{"exclusive", "parallel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			handler := &batchSlotHandler{bad: 0}
			definition, err := NewTypedExtensionTool(protocol.ToolSchema{Name: protocol.ToolName(mode), InputSchema: protocol.InputSchema{Type: protocol.SchemaObject}}, ToolTraits{Risk: RiskRead, Readonly: true, ParallelSafe: mode == "parallel"}, batchSlotCodec{}, handler)
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := NewToolCatalog(definition)
			if err != nil {
				t.Fatal(err)
			}
			gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{Observers: []ResultObserver{cancelBatchResultObserver{cancel}}})
			if err != nil {
				t.Fatal(err)
			}
			provider := &batchSlotProvider{modes: []string{mode}}
			session, err := NewSessionWithGate("cancel-hash", "owner", provider, gate, ModeAuto, "", 2)
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Run(ctx, "batch")
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("parent cancellation lost to hash error: %v", err)
			}
			if provider.requests != 1 || len(session.recentSteps) != 0 {
				t.Fatal("cancelled batch advanced", provider.requests, session.recentSteps)
			}
			messages := session.Messages()
			if len(messages) != 3 {
				t.Fatalf("cancelled call was not paired: %d messages", len(messages))
			}
			blocks, ok := messages[2].Content.Blocks()
			if !ok || len(blocks) != 1 {
				t.Fatal("missing cancellation repair result", blocks)
			}
			result, ok := blocks[0].ToolResult()
			if !ok || result.ToolUseID != "id-0" || result.Content != UnknownResult {
				t.Fatal("unexpected cancellation repair result", result)
			}
		})
	}
}
