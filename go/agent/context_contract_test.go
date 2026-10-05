package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type contextContract struct {
	Catalogs []struct {
		Input       []protocol.ToolSchema
		Schemas     []protocol.ToolSchema
		Sent        []protocol.ToolName
		Omitted     []protocol.ToolName
		TrimmedTo   *int `json:"trimmed_to"`
		Fingerprint string
		System      string
	}
	Wire []struct {
		Messages    []protocol.Message
		System      string
		Tools       []protocol.ToolSchema
		Estimate    int
		FakeTokens  int    `json:"fake_tokens"`
		ASCIIJSON   string `json:"ascii_json"`
		UnicodeJSON string `json:"unicode_json"`
	}
	Cheap []struct {
		Messages    []protocol.Message
		MaxMessages int `json:"max_messages"`
		Snipped     []protocol.Message
		Removed     int
		Micro       []protocol.Message
		Cleared     int
	}
	Meter []struct {
		Messages      []protocol.Message
		Usage         protocol.TokenUsage
		Envelope      string
		Probe         []protocol.Message
		ProbeEnvelope string `json:"probe_envelope"`
		Used          int
		Snapshot      TokenMeterSnapshot
	}
	Spill struct {
		Messages     []protocol.Message
		Result       []protocol.Message
		Persisted    int
		MaxBytes     int `json:"max_bytes"`
		PreviewChars int `json:"preview_chars"`
		Files        []struct{ Path, Content string }
	}
	Summary struct {
		Messages []protocol.Message
		Result   []protocol.Message
		Archive  string
		Requests []struct {
			Messages  []protocol.Message
			MaxTokens int `json:"max_tokens"`
			Purpose   protocol.RequestPurpose
		}
		Events []struct {
			Event            string
			Kind             CompactionKind
			Transcript       string
			ReplacedMessages int    `json:"replaced_messages"`
			ReplacedTokens   int    `json:"replaced_tokens_estimate"`
			InputTokens      int    `json:"summary_input_tokens"`
			OutputTokens     int    `json:"summary_output_tokens"`
			Model            string `json:"summary_model"`
		}
	}
}

func readContextContract(t *testing.T) contextContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-context.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture contextContract
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestRequestCatalogAndSystemMatchPython(t *testing.T) {
	for i, fixture := range readContextContract(t).Catalogs {
		var definitions []ToolDefinition
		for _, schema := range fixture.Input {
			definition, err := NewToolDefinitionWithSchema(schema, ToolTraits{Risk: RiskRead, Readonly: true}, &gateHandler{})
			if err != nil {
				t.Fatal(err)
			}
			definitions = append(definitions, definition)
		}
		catalog, err := NewToolCatalog(definitions...)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := catalog.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		schemas := snapshot.Schemas()
		if !reflect.DeepEqual(schemas, fixture.Schemas) || snapshot.Fingerprint() != fixture.Fingerprint {
			t.Fatalf("case %d fitted schemas/fingerprint drift", i)
		}
		if strings.Join(toolStrings(snapshot.SentNames()), ",") != strings.Join(toolStrings(fixture.Sent), ",") || strings.Join(toolStrings(snapshot.OmittedNames()), ",") != strings.Join(toolStrings(fixture.Omitted), ",") {
			t.Fatalf("case %d inventory drift", i)
		}
		trimmed := 0
		if fixture.TrimmedTo != nil {
			trimmed = *fixture.TrimmedTo
		}
		if snapshot.TrimmedTo() != trimmed || snapshot.InventoryCount() != len(fixture.Input) {
			t.Fatalf("case %d fitting metadata drift", i)
		}
		system, err := (DefaultSystemBuilder{}).BuildSystem(SystemContext{Workspace: "/contract", Catalog: snapshot, Skills: "paint: 绘图"})
		if err != nil || system != fixture.System {
			t.Fatalf("case %d system drift: %v", i, err)
		}
		if len(schemas) > 0 {
			schemas[0].Description = "mutated"
			if schemas[0].InputSchema.Properties != nil {
				delete(*schemas[0].InputSchema.Properties, "command")
			}
			if !reflect.DeepEqual(snapshot.Schemas(), fixture.Schemas) {
				t.Fatal("snapshot leaked a schema")
			}
		}
		if len(fixture.Omitted) == 0 && fixture.TrimmedTo == nil {
			for _, schema := range fixture.Input {
				production, ok := protocol.DefaultToolSchema(schema.Name)
				if !ok || !reflect.DeepEqual(production, schema) {
					t.Fatalf("production schema %s drift", schema.Name)
				}
			}
		}
	}
}
func toolStrings(names []protocol.ToolName) []string {
	result := make([]string, len(names))
	for i, name := range names {
		result[i] = string(name)
	}
	return result
}

func TestRequestJSONAndBudgetMatchPython(t *testing.T) {
	for i, fixture := range readContextContract(t).Wire {
		ascii, err := protocol.PythonJSON(fixture.Messages, true, false)
		if err != nil || ascii != fixture.ASCIIJSON {
			t.Fatalf("case %d ASCII JSON drift: %s", i, ascii)
		}
		unicode, err := protocol.PythonJSON(fixture.Messages, false, false)
		if err != nil || unicode != fixture.UnicodeJSON {
			t.Fatalf("case %d Unicode JSON drift: %s", i, unicode)
		}
		if EstimateTokens(fixture.Messages) != fixture.Estimate {
			t.Fatalf("case %d estimate drift", i)
		}
		request := protocol.ModelRequest{Model: DefaultModel, MaxTokens: 8000, Messages: fixture.Messages, System: &fixture.System, Tools: fixture.Tools, Purpose: protocol.PurposeAgentTurn}
		count, err := FakePromptTokens(request)
		if err != nil || count != fixture.FakeTokens {
			t.Fatalf("case %d fake count %d != %d", i, count, fixture.FakeTokens)
		}
		reply, err := (&FakeProvider{}).Complete(context.Background(), request)
		if err != nil || reply.Usage.InputTokens != fixture.FakeTokens || reply.Model != DefaultModel {
			t.Fatalf("case %d usage not connected: %v", i, err)
		}
	}
}

func TestCheapCompactionMatchesPython(t *testing.T) {
	for i, fixture := range readContextContract(t).Cheap {
		original := append([]protocol.Message(nil), fixture.Messages...)
		snipped, removed := SnipCompact(fixture.Messages, fixture.MaxMessages)
		micro, cleared := MicroCompact(fixture.Messages)
		if removed != fixture.Removed || !reflect.DeepEqual(snipped, fixture.Snipped) {
			t.Fatalf("case %d snip drift", i)
		}
		if cleared != fixture.Cleared || !reflect.DeepEqual(micro, fixture.Micro) {
			t.Fatalf("case %d micro drift", i)
		}
		if !reflect.DeepEqual(original, fixture.Messages) {
			t.Fatal("compaction mutated original")
		}
		if err := protocol.ValidateTranscript(snipped); err != nil {
			t.Fatal(err)
		}
		if err := protocol.ValidateTranscript(micro); err != nil {
			t.Fatal(err)
		}
	}
}
func TestMeterMatchesPython(t *testing.T) {
	var meter TokenMeter
	for i, fixture := range readContextContract(t).Meter {
		meter.Observe(fixture.Usage, fixture.Messages, fixture.Envelope)
		if used := meter.UsedFor(fixture.Probe, fixture.ProbeEnvelope); used != fixture.Used {
			t.Fatalf("case %d used %d != %d", i, used, fixture.Used)
		}
		if !reflect.DeepEqual(meter.Snapshot(), fixture.Snapshot) {
			t.Fatalf("case %d snapshot drift: %+v != %+v", i, meter.Snapshot(), fixture.Snapshot)
		}
	}
}

type summaryProvider struct {
	requests []protocol.ModelRequest
	text     string
	err      error
}

func (provider *summaryProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, request.Clone())
	if provider.err != nil {
		return protocol.ModelReply{}, provider.err
	}
	reply := fakeReply([]protocol.Block{protocol.NewTextBlock(provider.text)}, protocol.StopEndTurn)
	reply.Model = "served-summary"
	reply.Usage.InputTokens = 123
	reply.Usage.OutputTokens = 7
	return reply, nil
}
func normalizeMessages(t *testing.T, messages []protocol.Message, root string) []protocol.Message {
	t.Helper()
	data, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	var normalized []protocol.Message
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(data), root, "<workspace>")), &normalized); err != nil {
		t.Fatal(err)
	}
	return normalized
}
func TestSpillAndSummaryMatchPython(t *testing.T) {
	fixture := readContextContract(t)
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	compactor := NewDefaultCompactor()
	compactor.now = func() int64 { return 1234 }
	compactor.ResultBudget, compactor.PreviewChars = fixture.Spill.MaxBytes, fixture.Spill.PreviewChars
	provider := &summaryProvider{text: "handoff: 已完成"}
	value := CompactionContext{Messages: fixture.Spill.Messages, Files: files, Provider: provider, Model: DefaultModel}
	spilled, err := compactor.budget(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalizeMessages(t, spilled.Messages, files.Root()), fixture.Spill.Result) {
		t.Fatal("spill content drift")
	}
	if len(spilled.Events) != 1 || spilled.Events[0].kind != CompactBudget || spilled.Events[0].count != fixture.Spill.Persisted {
		t.Fatal("spill receipt drift")
	}
	for _, artifact := range fixture.Spill.Files {
		data, err := os.ReadFile(filepath.Join(files.Root(), artifact.Path))
		if err != nil || string(data) != artifact.Content {
			t.Fatalf("spill artifact drift: %v", err)
		}
	}
	value.Messages = fixture.Summary.Messages
	compressed, err := compactor.Compact(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalizeMessages(t, compressed.Messages, files.Root()), fixture.Summary.Result) {
		t.Fatal("summary content drift")
	}
	wantRequest := fixture.Summary.Requests[0]
	if len(provider.requests) != 1 || provider.requests[0].System != nil || provider.requests[0].Tools != nil || provider.requests[0].MaxTokens != wantRequest.MaxTokens || provider.requests[0].Purpose != wantRequest.Purpose || !reflect.DeepEqual(provider.requests[0].Messages, wantRequest.Messages) {
		t.Fatal("summary request drift")
	}
	archive, err := os.ReadFile(filepath.Join(files.Root(), ".transcripts/transcript_1234.jsonl"))
	if err != nil || string(archive) != fixture.Summary.Archive {
		t.Fatalf("archive drift: %v", err)
	}
	event, ok := compressed.Events[0].Summary()
	want := fixture.Summary.Events[0]
	if !ok || event.ReplacedMessages != want.ReplacedMessages || event.ReplacedTokensEstimate != want.ReplacedTokens || event.InputTokens != want.InputTokens || event.OutputTokens != want.OutputTokens || event.Model != want.Model || strings.ReplaceAll(event.Transcript, files.Root(), "<workspace>") != want.Transcript {
		t.Fatalf("summary receipt drift: %+v", event)
	}
}
