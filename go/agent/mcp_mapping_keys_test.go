package agent

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

type mcpMappingAction struct {
	ToolUseID string `json:"tool_use_id"`
	Status    ActionStatus
	InputHash string `json:"input_hash"`
	Result    *string
}
type mcpMappingFixture struct {
	Cases []struct {
		Name    string
		Mode    PermissionMode
		Input   string `json:"input_json"`
		Batch   bool
		Output  *string
		Error   *struct{ Kind, Message string }
		Trace   []string
		Calls   []jsonvalue.Value
		Results []struct {
			Output         string
			Denied, Failed bool
		}
		Requests            int
		ModelInputUnchanged bool `json:"model_input_unchanged"`
		Actions             []mcpMappingAction
	}
}

func TestMCPMappingKeysMatchActualSourceBatchAndGate(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-mcp-mapping-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture mcpMappingFixture
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 116 {
		t.Fatal(err, len(fixture.Cases))
	}
	checkMCPMappingCases(t, fixture, false)
}
func TestMCPMappingKeysMatchActualSourceJournal(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-mcp-mapping-key-journal.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture mcpMappingFixture
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 116 {
		t.Fatal(err, len(fixture.Cases))
	}
	checkMCPMappingCases(t, fixture, true)
}
func checkMCPMappingCases(t *testing.T, fixture mcpMappingFixture, journaled bool) {
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			trace := []string{}
			calls := []jsonvalue.Value{}
			results := []ToolOutcome{}
			peer := simpleMCPPeer("peer", "")
			peer.callHook = func(_ context.Context, value jsonvalue.Value) {
				trace = append(trace, "handler")
				calls = append(calls, value)
			}
			// A concrete peer returns each prepared object in Source compact form.
			client := mcpModelRootPeer{peer: peer}
			registry := &MCPRegistry{}
			published, err := registry.Register(context.Background(), client, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			provider := &mcpRootProvider{input: row.Input, batch: row.Batch}
			config := runtimeConfig(t.TempDir(), provider)
			config.MCPCatalog = published.Catalog
			config.Mode = row.Mode
			var journal *InMemoryActionJournal
			if journaled {
				journal, err = NewInMemoryActionJournal(512)
				if err != nil {
					t.Fatal(err)
				}
				config.ActionJournal = journal
			}
			config.Hooks = GateHooks{Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
				trace = append(trace, "before")
				return KeepToolCall(), nil
			})}, After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
				trace = append(trace, "after")
				return output, nil
			})}, Observers: []ResultObserver{observerFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, result ToolOutcome) error {
				trace = append(trace, "result")
				results = append(results, result)
				return nil
			})}}
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			output, err := session.Run(context.Background(), "call MCP")
			if row.Error != nil {
				var failure interface {
					error
					SourceClass() string
				}
				if !errors.As(err, &failure) || failure.SourceClass() != row.Error.Kind || failure.Error() != row.Error.Message {
					t.Fatal("Source preparation failure differs", err, row.Error)
				}
			} else if err != nil || row.Output == nil || output != *row.Output {
				t.Fatal(output, row.Output, err)
			}
			if !reflect.DeepEqual(trace, row.Trace) || !reflect.DeepEqual(calls, row.Calls) || len(results) != len(row.Results) || provider.requests != row.Requests {
				t.Fatal("Source effects/hook stages differ", trace, row.Trace, calls, row.Calls, results, provider.requests, row.Requests)
			}
			for i, result := range results {
				if result.Output != row.Results[i].Output || result.Denied != row.Results[i].Denied || result.Failed != row.Results[i].Failed {
					t.Fatal(result, row.Results[i])
				}
			}
			if journal != nil {
				observed := make([]mcpMappingAction, 0, len(journal.records))
				for _, action := range journal.records {
					observed = append(observed, mcpMappingAction{action.ToolUseID, action.Status, string(action.InputHash), action.Result})
				}
				sort.Slice(observed, func(i, j int) bool { return observed[i].ToolUseID < observed[j].ToolUseID })
				if !reflect.DeepEqual(observed, row.Actions) {
					t.Fatal("Source journal records differ", observed, row.Actions)
				}
			}
			messages := session.Messages()
			blocks, _ := messages[1].Content.Blocks()
			use, _ := blocks[len(blocks)-1].ToolUse()
			observation, err := use.Input.Observation()
			if err != nil {
				t.Fatal(err)
			}
			original, err := observation.LegacyJSON()
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonvalue.Decode(row.Input)
			if err != nil {
				t.Fatal(err)
			}
			expected, _ := value.MarshalLegacyUTF8()
			if string(original) != string(expected) || !row.ModelInputUnchanged {
				t.Fatal("prepared object overwrote model history", string(original), string(expected))
			}
		})
	}
}

func TestMCPMappingArchiveDoesNotMaskNumericKeysAsText(t *testing.T) {
	input, err := protocol.DecodeModelToolInput("mcp__peer__echo", []byte(`[[Infinity,"Infinity-secret"]]`))
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.ForModelBatch()
	if err != nil {
		t.Fatal(err)
	}
	record := SessionEventRecord{Sequence: 1, TranscriptEpoch: 1, SessionID: "session", Event: SessionEvent{kind: EventToolUse, toolUse: ToolUseEvent{Name: input.Name(), Input: input, ID: "model"}}}
	data, err := maskedEventArchive(record, func(text string) string { return strings.ReplaceAll(text, "Infinity", "masked") })
	if err != nil {
		t.Fatal(err)
	}
	stored, err := DecodeStoredEvent(data)
	if err != nil {
		t.Fatal(err)
	}
	use, _ := stored.Event.ToolUse()
	observed, err := use.Input.Observation()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := observed.LegacyJSON()
	if err != nil || string(encoded) != `{"Infinity":"masked-secret"}` {
		t.Fatal("numeric key lost original kind while masking", string(encoded), err)
	}
}
