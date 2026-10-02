package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type compressionProvider struct {
	requests     []protocol.ModelRequest
	turns        int
	summaryError error
	summaryText  string
}

func (provider *compressionProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	provider.requests = append(provider.requests, request.Clone())
	if request.Purpose == protocol.PurposeCompaction {
		if provider.summaryError != nil {
			return protocol.ModelReply{}, provider.summaryError
		}
		reply := fakeReply([]protocol.Block{protocol.NewTextBlock(provider.summaryText)}, protocol.StopEndTurn)
		reply.Usage.InputTokens = 9999
		return reply, nil
	}
	provider.turns++
	if provider.turns == 1 {
		reply := fakeReply([]protocol.Block{
			protocol.NewToolUse("compress", protocol.CompressToolInput()),
			protocol.NewToolUse("read", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "proof"})),
		}, protocol.StopToolUse)
		reply.Usage.InputTokens = 100
		return reply, nil
	}
	reply := fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn)
	reply.Usage.InputTokens = 200
	return reply, nil
}
func TestCompressWaitsForWholeBatchAndSummaryDoesNotAnchorMeter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "proof"), []byte("proof content"), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &compressionProvider{summaryText: "handoff"}
	config := runtimeConfig(root, provider)
	config.Model = "requested-model"
	config.MaxTokens = 777
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.Run(context.Background(), "compress then continue")
	if err != nil || output != "done" {
		t.Fatalf("run: %q %v", output, err)
	}
	if len(provider.requests) != 3 || provider.requests[1].Purpose != protocol.PurposeCompaction {
		t.Fatalf("request order: %+v", provider.requests)
	}
	for _, request := range provider.requests {
		if request.Model != "requested-model" {
			t.Fatal("configured model lost")
		}
	}
	if provider.requests[0].MaxTokens != 777 || provider.requests[2].MaxTokens != 777 || provider.requests[1].MaxTokens != 2000 || provider.requests[1].System != nil || provider.requests[1].Tools != nil {
		t.Fatal("summary request polluted with turn settings")
	}
	meter := session.TokenMeter()
	if meter.Observations != 2 || meter.AnchorTokens == nil || *meter.AnchorTokens != 200 {
		t.Fatalf("summary changed live meter: %+v", meter)
	}
	var receipt SummaryReceipt
	for _, record := range session.Events() {
		if event, ok := record.Event.Compaction(); ok {
			if value, ok := event.Summary(); ok {
				receipt = value
			}
		}
	}
	if receipt.ReplacedMessages != 3 || receipt.InputTokens != 9999 || receipt.Transcript == "" {
		t.Fatalf("summary receipt: %+v", receipt)
	}
	data, err := os.ReadFile(receipt.Transcript)
	if err != nil {
		t.Fatal(err)
	}
	var archived []protocol.Message
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var message protocol.Message
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatal(err)
		}
		archived = append(archived, message)
	}
	if err := protocol.ValidateTranscript(archived); err != nil {
		t.Fatal(err)
	}
	results, _ := archived[2].Content.Blocks()
	if len(results) != 2 {
		t.Fatal("summary ran before batch paired")
	}
	read, _ := results[1].ToolResult()
	if !strings.Contains(read.Content, "proof content") {
		t.Fatal("summary missed later tool output")
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
}

func TestReadonlyCompressIsDeniedAndCreatesNoArchive(t *testing.T) {
	root := t.TempDir()
	provider := &compressionProvider{summaryText: "handoff"}
	config := runtimeConfig(root, provider)
	config.Mode = ModeReadonly
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "compress"); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 2 {
		t.Fatal("denied compression called summary model")
	}
	if _, err := os.Stat(filepath.Join(root, ".transcripts")); !os.IsNotExist(err) {
		t.Fatalf("denied compression wrote archive: %v", err)
	}
	blocks, _ := session.Messages()[2].Content.Blocks()
	result, _ := blocks[0].ToolResult()
	if !result.IsError || !strings.Contains(result.Content, "denied") {
		t.Fatalf("compress denial lost: %+v", result)
	}
}
func TestForcedCompressionFailurePreservesPairedHistory(t *testing.T) {
	for _, failure := range []error{errors.New("provider unavailable"), context.Canceled} {
		provider := &compressionProvider{summaryError: failure}
		session, err := NewRuntimeSession(runtimeConfig(t.TempDir(), provider))
		if err != nil {
			t.Fatal(err)
		}
		_, err = session.Run(context.Background(), "compress")
		if !errors.Is(err, failure) {
			t.Fatalf("summary error lost: %v", err)
		}
		messages := session.Messages()
		if len(messages) != 3 || protocol.ValidateTranscript(messages) != nil {
			t.Fatal("summary failure lost tool batch")
		}
	}
}

func TestAutomaticCompressionFailureAndCancellationKeepCheapChanges(t *testing.T) {
	fixture := readContextContract(t).Cheap[2]
	for _, failure := range []error{nil, context.Canceled} {
		files, err := workspace.NewFiles(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		provider := &summaryProvider{text: "\x1c\u3000", err: failure}
		compactor := NewDefaultCompactor()
		compactor.TokenThreshold = 1
		compactor.ResultBudget = 1_000_000
		result, err := compactor.MaybeCompact(context.Background(), CompactionContext{Messages: fixture.Messages, Files: files, Provider: provider, Model: DefaultModel})
		if !reflect.DeepEqual(result.Messages, fixture.Micro) {
			t.Fatal("failed summary discarded cheap compaction")
		}
		if failure != nil {
			if !errors.Is(err, failure) {
				t.Fatalf("cancellation swallowed: %v", err)
			}
		} else {
			if err != nil || len(result.Events) != 2 || result.Events[1].kind != CompactFailed || !strings.Contains(result.Events[1].failure, "summary came back empty") {
				t.Fatalf("empty summary failure missing: %+v %v", result.Events, err)
			}
		}
	}
}

func TestCheapShrinkAvoidsUnnecessarySummary(t *testing.T) {
	fixture := readContextContract(t).Cheap[0]
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var meter TokenMeter
	used := EstimateTokens(fixture.Messages)
	meter.Observe(protocol.TokenUsage{InputTokens: used}, fixture.Messages, "stable")
	provider := &summaryProvider{text: "unexpected summary"}
	compactor := NewDefaultCompactor()
	compactor.TokenThreshold = used - 1
	compactor.MaxMessages = fixture.MaxMessages
	compactor.ResultBudget = 1_000_000
	result, err := compactor.MaybeCompact(context.Background(), CompactionContext{Messages: fixture.Messages, Files: files, Provider: provider, Model: DefaultModel, Meter: meter, Envelope: "stable"})
	if err != nil || len(provider.requests) != 0 || len(result.Messages) >= len(fixture.Messages) {
		t.Fatalf("cheap shrink still summarized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(files.Root(), ".transcripts")); !os.IsNotExist(err) {
		t.Fatal("cheap compaction archived")
	}
}

func TestCompactionWorkspaceBoundaryRejectsEscapedArchive(t *testing.T) {
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(files.Root(), ".transcripts")); err != nil {
		t.Fatal(err)
	}
	provider := &summaryProvider{text: "summary"}
	_, err = NewDefaultCompactor().Compact(context.Background(), CompactionContext{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("prompt")}}, Files: files, Provider: provider, Model: DefaultModel})
	var escaped *workspace.PathEscapeError
	if !errors.As(err, &escaped) || len(provider.requests) != 0 {
		t.Fatalf("archive escaped workspace: %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("wrote outside bound root")
	}
}

type recordingProvider struct{ requests []protocol.ModelRequest }

type invalidCompactor struct{ failure error }

func (compactor invalidCompactor) MaybeCompact(context.Context, CompactionContext) (CompactionResult, error) {
	if compactor.failure != nil {
		return CompactionResult{}, compactor.failure
	}
	return CompactionResult{Messages: []protocol.Message{{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("orphan", "echo"))}}}, nil
}
func (compactor invalidCompactor) Compact(ctx context.Context, value CompactionContext) (CompactionResult, error) {
	return compactor.MaybeCompact(ctx, value)
}
func TestCustomCompactorFailureAndInvalidRewritePreserveOriginal(t *testing.T) {
	for _, failure := range []error{nil, context.Canceled} {
		provider := &recordingProvider{}
		config := runtimeConfig(t.TempDir(), provider)
		config.Compactor = invalidCompactor{failure}
		session, err := NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
		_, err = session.Run(context.Background(), "original")
		if failure != nil {
			if !errors.Is(err, failure) {
				t.Fatalf("original error lost: %v", err)
			}
		} else if err == nil {
			t.Fatal("orphan rewrite accepted")
		}
		messages := session.Messages()
		text, _ := messages[0].Content.Plain()
		if len(messages) != 1 || text != "original" || len(provider.requests) != 0 {
			t.Fatal("invalid rewrite changed session")
		}
	}
}

func (provider *recordingProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, request.Clone())
	request.Tools[0].Description = "mutated by provider"
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestSkillsAndChangedRuntimeFactsEnterActualRequest(t *testing.T) {
	root := t.TempDir()
	skillRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("---\nname: paint\ndescription: 绘图\n---\nbody"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := skills.NewCatalog(context.Background(), skillRoot)
	if err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{}
	config := runtimeConfig(root, provider)
	config.Skills = source
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	items := []protocol.TodoItem{{Content: "read", Status: protocol.TodoPending, ActiveForm: "reading"}}
	if _, err := session.todos.Update(items); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := session.Run(context.Background(), "continue"); err != nil {
			t.Fatal(err)
		}
	}
	if len(provider.requests) != 2 || provider.requests[0].System == nil || !strings.Contains(*provider.requests[0].System, "paint: 绘图") || *provider.requests[0].System != *provider.requests[1].System {
		t.Fatal("skills missing or stable prompt changed")
	}
	facts := 0
	for _, message := range provider.requests[1].Messages {
		if text, ok := message.Content.Plain(); ok && strings.HasPrefix(text, "<runtime-state>") {
			facts++
			if !strings.Contains(text, "Current TodoWrite state:") {
				t.Fatal("todo state missing")
			}
		}
	}
	if facts != 1 {
		t.Fatal("unchanged facts repeatedly injected")
	}
	if provider.requests[1].Tools[0].Description == "mutated by provider" {
		t.Fatal("request schema leaked into catalog")
	}
}
