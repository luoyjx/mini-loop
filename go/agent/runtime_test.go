package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
)

type todoCase struct {
	Items    []protocol.TodoItem
	Output   string
	Failed   bool
	Snapshot []protocol.TodoItem
	HasOpen  bool `json:"has_open"`
}
type questionCase struct {
	Available bool
	Answer    *string
	Output    string
}
type runtimeContract struct {
	TodoFieldCap int `json:"todo_field_cap"`
	Todos        []todoCase
	Questions    []questionCase
}

func readRuntimeContract(t *testing.T) runtimeContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-runtime-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract runtimeContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Todos) != 10 || len(contract.Questions) != 4 || contract.TodoFieldCap != MaxTodoField {
		t.Fatal("runtime contract inventory drift")
	}
	return contract
}
func TestTodoManagerMatchesPythonContracts(t *testing.T) {
	manager := &TodoManager{}
	for i, fixture := range readRuntimeContract(t).Todos {
		output, err := manager.Update(fixture.Items)
		if err != nil {
			output = "Error: " + err.Error()
		}
		if (err != nil) != fixture.Failed || output != fixture.Output || !reflect.DeepEqual(manager.Snapshot(), fixture.Snapshot) || manager.HasOpenItems() != fixture.HasOpen {
			t.Fatalf("todo case %d drift: %q %v", i, output, err)
		}
		snapshot := manager.Snapshot()
		if len(snapshot) > 0 {
			snapshot[0].Content = "caller mutated snapshot"
		}
		if !reflect.DeepEqual(manager.Snapshot(), fixture.Snapshot) {
			t.Fatal("todo snapshot leaked")
		}
	}
}

type resourceProvider struct{ tools []protocol.Block }

func (provider resourceProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	messages := request.Messages
	if len(messages) == 1 {
		return fakeReply(provider.tools, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

type testQuestions struct {
	answer  QuestionAnswer
	request QuestionRequest
	started chan struct{}
	mu      sync.Mutex
}

func (questions *testQuestions) AskQuestion(ctx context.Context, request QuestionRequest) (QuestionAnswer, error) {
	questions.mu.Lock()
	questions.request = request
	questions.mu.Unlock()
	if questions.started != nil {
		close(questions.started)
		<-ctx.Done()
		return NoQuestionAnswer(), ctx.Err()
	}
	return questions.answer, nil
}
func runtimeConfig(root string, provider Provider) RuntimeConfig {
	return RuntimeConfig{ID: "session", Owner: "owner", Provider: provider, Bash: echoExecutor{}, Workspace: root, Mode: ModeInteractive, MaxRounds: 2}
}
func TestQuestionsMatchPythonContractsThroughReadonlyGate(t *testing.T) {
	for i, fixture := range readRuntimeContract(t).Questions {
		config := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewToolUse("q", protocol.AskUserToolInput(protocol.AskUserInput{Question: "哪个颜色？"}))}})
		config.Mode = ModeReadonly
		questioner := &testQuestions{answer: NoQuestionAnswer()}
		if fixture.Answer != nil {
			questioner.answer = AnswerQuestion(*fixture.Answer)
		}
		if fixture.Available {
			config.Questions = questioner
		}
		session, err := NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Run(context.Background(), "ask"); err != nil {
			t.Fatal(err)
		}
		blocks, _ := session.Messages()[2].Content.Blocks()
		result, _ := blocks[0].ToolResult()
		if result.IsError || result.Content != fixture.Output {
			t.Fatalf("question %d: %+v", i, result)
		}
		if fixture.Available && (questioner.request.Authority.OwnerID != config.Owner || questioner.request.Authority.SessionID != config.ID || questioner.request.Question != "哪个颜色？") {
			t.Fatal("question lost authority or text")
		}
	}
}
func TestRuntimeResourcesUseOneGateAndIsolateSessions(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	sourceRoot := filepath.Join(parent, "skills")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "read"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "read", "SKILL.md"), []byte("---\nname: read\n---\nRead carefully"), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := skills.NewCatalog(context.Background(), sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	item := protocol.TodoItem{Content: "read", Status: protocol.TodoInProgress, ActiveForm: "reading"}
	uses := []protocol.Block{
		protocol.NewToolUse("todo", protocol.TodoWriteToolInput(protocol.TodoWriteInput{Items: []protocol.TodoItem{item}})),
		protocol.NewToolUse("skill", protocol.LoadSkillToolInput(protocol.LoadSkillInput{Name: "read"})),
		protocol.NewToolUse("ask", protocol.AskUserToolInput(protocol.AskUserInput{Question: "ready?"})),
	}
	config := runtimeConfig(root, resourceProvider{uses})
	config.Skills = source
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	readonly := config
	readonly.ID = "readonly"
	readonly.Mode = ModeReadonly
	second, err := NewRuntimeSession(readonly)
	if err != nil {
		t.Fatal(err)
	}
	for _, current := range []*Session{session, second} {
		if _, err := current.Run(context.Background(), "resources"); err != nil {
			t.Fatal(err)
		}
		if err := protocol.ValidateTranscript(current.Messages()); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(session.Todos(), []protocol.TodoItem{item}) || len(second.Todos()) != 0 {
		t.Fatal("todo state leaked or readonly mutated it")
	}
	records := session.Events()
	if len(records) != 1 || records[0].Sequence != 1 || records[0].Event.Kind() != EventTodo {
		t.Fatalf("todo event missing: %+v", records)
	}
	todos, ok := records[0].Event.Todos()
	if !ok || !reflect.DeepEqual(todos, session.Todos()) {
		t.Fatal("todo event differs")
	}
	todos[0].Content = "mutated"
	again, _ := session.Events()[0].Event.Todos()
	if again[0] != item {
		t.Fatal("todo event alias")
	}
	if len(second.Events()) != 0 {
		t.Fatal("denied mutation emitted a todo event")
	}
	blocks, _ := second.Messages()[2].Content.Blocks()
	denied, _ := blocks[0].ToolResult()
	loaded, _ := blocks[1].ToolResult()
	if !denied.IsError || !strings.Contains(denied.Content, "read-only") || loaded.IsError || loaded.Content != "<skill name=\"read\">\nRead carefully\n</skill>" {
		t.Fatalf("readonly results: %+v %+v", denied, loaded)
	}
}
func TestRuntimeCatalogMatchesPythonMetadata(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-default-tool-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var metadata []struct {
		Name         protocol.ToolName
		Risk         ToolRisk
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Capabilities []Capability
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	session, err := NewRuntimeSession(runtimeConfig(t.TempDir(), &FakeProvider{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(session.gate.CatalogNames()) != 10 {
		t.Fatal("runtime catalogue drift")
	}
	for _, expected := range metadata {
		definition, exists := session.gate.catalog.Lookup(expected.Name)
		if !exists || definition.Risk() != expected.Risk || definition.Readonly() != expected.Readonly || definition.ParallelSafe() != expected.ParallelSafe || !slices.Equal(definition.Capabilities(), expected.Capabilities) {
			t.Fatalf("metadata drift for %s", expected.Name)
		}
	}
}
func TestCancelledQuestionClosesTranscriptAndLeavesTodoUntouched(t *testing.T) {
	questioner := &testQuestions{started: make(chan struct{})}
	config := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{
		protocol.NewToolUse("q", protocol.AskUserToolInput(protocol.AskUserInput{Question: "wait"})),
		protocol.NewToolUse("todo", protocol.TodoWriteToolInput(protocol.TodoWriteInput{Items: []protocol.TodoItem{{Content: "a", Status: protocol.TodoPending, ActiveForm: "a"}}})),
	}})
	config.Questions = questioner
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "ask"); finished <- err }()
	select {
	case <-questioner.started:
	case <-time.After(time.Second):
		t.Fatal("question did not start")
	}
	_ = session.Events()
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("question did not cancel")
	}
	if len(session.Todos()) != 0 || len(session.Events()) != 0 {
		t.Fatal("cancelled batch mutated resources")
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
}
func TestEventBacklogPreservesSequenceAndDetachedVariants(t *testing.T) {
	events := &sessionEvents{}
	for i := 0; i < EventBacklog+5; i++ {
		events.append(SessionEvent{kind: EventTodo, todos: []protocol.TodoItem{{Content: "task", Status: protocol.TodoPending, ActiveForm: "working"}}})
	}
	records := events.snapshot()
	if len(records) != EventBacklog || records[0].Sequence != 6 || records[len(records)-1].Sequence != EventBacklog+5 {
		t.Fatal("event bound or cursor drift")
	}
	if _, ok := records[0].Event.Stop(); ok {
		t.Fatal("todo was treated as a stop")
	}
	if _, ok := (SessionEvent{}).Stop(); ok {
		t.Fatal("invalid event became a stop")
	}
}

func TestRuntimeBindingRefusesAnotherSessionBeforeMutation(t *testing.T) {
	config := runtimeConfig(t.TempDir(), &FakeProvider{})
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ID: "todo", Input: protocol.TodoWriteToolInput(protocol.TodoWriteInput{Items: []protocol.TodoItem{{Content: "a", Status: protocol.TodoPending, ActiveForm: "a"}}})}
	authority := ToolAuthority{SessionID: "other", OwnerID: config.Owner, Workspace: session.workspace, Mode: config.Mode}
	outcome, err := session.gate.Dispatch(context.Background(), authority, call)
	if err != nil || !outcome.IsError() || len(session.Todos()) != 0 || len(session.Events()) != 0 {
		t.Fatalf("foreign session mutation: %+v %v", outcome, err)
	}
	authority.SessionID, authority.OwnerID = config.ID, "other-owner"
	outcome, err = session.gate.Dispatch(context.Background(), authority, call)
	if err != nil || !outcome.IsError() || len(session.Todos()) != 0 {
		t.Fatal("foreign owner reached runtime state")
	}
}

func TestInvalidQuestionAnswerIsToolErrorAndConstructorValidatesBeforeFilesystem(t *testing.T) {
	config := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewToolUse("q", protocol.AskUserToolInput(protocol.AskUserInput{Question: "question"}))}})
	config.Questions = &testQuestions{}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "ask"); err != nil {
		t.Fatal(err)
	}
	blocks, _ := session.Messages()[2].Content.Blocks()
	result, _ := blocks[0].ToolResult()
	if !result.IsError || result.Content != "Error: question surface returned an invalid answer variant" {
		t.Fatalf("invalid answer became success: %+v", result)
	}
	config.ID = ""
	config.Workspace = filepath.Join(t.TempDir(), "not-created")
	if _, err := NewRuntimeSession(config); err == nil {
		t.Fatal("invalid constructor succeeded")
	}
	if _, err := os.Stat(config.Workspace); !os.IsNotExist(err) {
		t.Fatal("invalid constructor created workspace")
	}
}
