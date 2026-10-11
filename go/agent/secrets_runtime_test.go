package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	secretpkg "github.com/luoyjx/mini-loop/go/secrets"
	workspacepkg "github.com/luoyjx/mini-loop/go/workspace"
)

const runtimeCanary = "clé-secret-\"café\"\\Ω-0123456789"

func runtimeSecrets() *secretpkg.Registry {
	registry := secretpkg.New(secretpkg.Config{})
	registry.RegisterValue("KEY", runtimeCanary)
	return registry
}

type secretBash struct {
	command string
	calls   int
}

func (executor *secretBash) ExecuteBash(_ context.Context, input protocol.BashInput) (string, error) {
	executor.command = input.Command
	executor.calls++
	return "raw " + runtimeCanary, nil
}

type secretAfter struct{ sawRaw bool }

func (hook *secretAfter) AfterTool(_ context.Context, _ ToolAuthority, _ ToolCall, output string) (string, error) {
	hook.sawRaw = strings.Contains(output, runtimeCanary)
	return "post " + runtimeCanary, nil
}

type secretObserver struct {
	t        *testing.T
	journal  ActionJournal
	observed bool
}

func (observer *secretObserver) OnResult(ctx context.Context, _ ToolAuthority, call ToolCall, outcome ToolOutcome) error {
	observer.observed = true
	if strings.Contains(outcome.Output, runtimeCanary) || !strings.Contains(outcome.Output, secretpkg.Mask) {
		observer.t.Error("observer saw raw result")
	}
	value, _ := call.Input.Bash()
	if !strings.Contains(value.Command, runtimeCanary) {
		observer.t.Error("trusted observer lost live call")
	}
	row, exists, err := observer.journal.Get(ctx, outcome.ActionID)
	if err != nil || !exists || row.Result == nil || *row.Result != outcome.Output {
		observer.t.Error("masking did not precede settlement", row, err)
	}
	return nil
}

type secretModel struct{ sawRawCall, sawMaskedResult bool }

func (provider *secretModel) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	last := request.Messages[len(request.Messages)-1]
	if _, plain := last.Content.Plain(); plain {
		return fakeReply([]protocol.Block{protocol.NewBashUse("u", "echo "+runtimeCanary)}, protocol.StopToolUse), nil
	}
	for _, message := range request.Messages {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if use, ok := block.ToolUse(); ok {
				input, _ := use.Input.Bash()
				provider.sawRawCall = strings.Contains(input.Command, runtimeCanary)
			}
			if result, ok := block.ToolResult(); ok {
				provider.sawMaskedResult = !strings.Contains(result.Content, runtimeCanary) && strings.Contains(result.Content, secretpkg.Mask)
			}
		}
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestRuntimeMaskingPrecedesJournalObserverAndModelResult(t *testing.T) {
	journal, _ := NewInMemoryActionJournal(4)
	executor, provider, after := &secretBash{}, &secretModel{}, &secretAfter{}
	observer := &secretObserver{t: t, journal: journal}
	config := runtimeConfig(t.TempDir(), provider)
	config.Bash, config.Secrets, config.ActionJournal = executor, runtimeSecrets(), journal
	config.Hooks = GateHooks{After: []AfterHook{after}, Observers: []ResultObserver{observer}}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(executor.command, runtimeCanary) || !after.sawRaw || !observer.observed || !provider.sawRawCall || !provider.sawMaskedResult {
		t.Fatal("live versus recorded boundary changed", executor, after, provider)
	}
	// The opt-in changes output handling; the default remains the Null path.
	provider = &secretModel{}
	config = runtimeConfig(t.TempDir(), provider)
	config.Bash = &secretBash{}
	session, err = NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "work"); err != nil {
		t.Fatal(err)
	}
	if provider.sawMaskedResult {
		t.Fatal("masking default became on")
	}
}
func TestRuntimeSecretsBindQuestionRowsWithoutMutatingSharedBroker(t *testing.T) {
	store := &approvalStoreSpy{}
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{Store: store})
	for i := range 2 {
		secret := fmt.Sprintf("question-secret-%d", i)
		registry := secretpkg.New(secretpkg.Config{})
		registry.RegisterValue("KEY", secret)
		provider := &approvalModel{question: true}
		config := runtimeConfig(t.TempDir(), provider)
		config.ID = SessionID(fmt.Sprintf("s%d", i))
		config.Approvals, config.Secrets = broker, registry
		session, err := NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := session.Run(context.Background(), "question"); done <- err }()
		pending := waitBrokerPending(t, broker, config.ID)
		if !broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: config.ID, Allowed: true, Answer: &secret}) {
			t.Fatal("resolve")
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		rows := store.snapshot()
		if rows[len(rows)-1].Answer == nil || *rows[len(rows)-1].Answer != secretpkg.Mask || provider.observed != "The user answered: "+secretpkg.Mask {
			t.Fatal("question row/result leaked", rows, provider.observed)
		}
	}
	if broker.redactor != nil {
		t.Fatal("shared broker configuration mutated")
	}
}
func TestDefaultCompactionMasksArchivesSpillsAndSummaryButPreservesRawRequest(t *testing.T) {
	root := t.TempDir()
	files, err := workspacepkg.NewFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	messages := []protocol.Message{
		{Role: protocol.RoleUser, Content: protocol.PlainContent(runtimeCanary)},
		{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewThinkingBlock(runtimeCanary, runtimeCanary), protocol.NewBashUse("u", "echo "+runtimeCanary))},
		{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("u", strings.Repeat(runtimeCanary, 20), false))},
	}
	provider := &summaryProvider{text: "summary " + runtimeCanary}
	compactor := NewDefaultCompactor()
	compactor.now = func() int64 { return 123 }
	value := CompactionContext{Messages: messages, Files: files, Provider: provider, Model: DefaultModel, Secrets: runtimeSecrets()}
	result, err := compactor.Compact(context.Background(), value)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".transcripts", "transcript_123.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	escaped, _ := protocol.PythonJSON(runtimeCanary, true, false)
	if strings.Contains(string(raw), runtimeCanary) || strings.Contains(string(raw), escaped[1:len(escaped)-1]) || !strings.Contains(string(raw), secretpkg.Mask) {
		t.Fatal("archive leaked raw/escaped credential")
	}
	summary, _ := result.Messages[0].Content.Plain()
	if strings.Contains(summary, runtimeCanary) || !strings.Contains(summary, secretpkg.Mask) {
		t.Fatal("summary unmasked")
	}
	requestText, _ := provider.requests[0].Messages[0].Content.Plain()
	if !strings.Contains(requestText, escaped[1:len(escaped)-1]) {
		t.Fatal("summary provider lost deliberate raw context")
	}
	plain, _ := messages[0].Content.Plain()
	if plain != runtimeCanary {
		t.Fatal("recording changed original history")
	}
	compactor.ResultBudget, compactor.PreviewChars = 100, 20
	if _, err := compactor.budget(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	spill, err := os.ReadFile(filepath.Join(root, ".task_outputs", "tool-results", "u-123.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(spill), runtimeCanary) || !strings.Contains(string(spill), secretpkg.Mask) {
		t.Fatal("spill unmasked")
	}
}

type secretChildModel struct{ childMasked, parentMasked bool }

func (provider *secretChildModel) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	child := request.System != nil && strings.Contains(*request.System, " subagent in ")
	last := request.Messages[len(request.Messages)-1]
	if _, plain := last.Content.Plain(); plain {
		if child {
			return fakeReply([]protocol.Block{protocol.NewBashUse("u", "echo "+runtimeCanary)}, protocol.StopToolUse), nil
		}
		role := protocol.AgentGeneralPurpose
		return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: runtimeCanary, AgentType: &role}))}, protocol.StopToolUse), nil
	}
	blocks, _ := last.Content.Blocks()
	result, _ := blocks[0].ToolResult()
	masked := strings.Contains(result.Content, secretpkg.Mask) && !strings.Contains(result.Content, runtimeCanary)
	if child {
		provider.childMasked = masked
		return fakeReply([]protocol.Block{protocol.NewTextBlock(runtimeCanary)}, protocol.StopEndTurn), nil
	}
	provider.parentMasked = masked
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestFreshChildrenKeepRegistryAndMaskParentEventBacklog(t *testing.T) {
	provider := &secretChildModel{}
	config := runtimeConfig(t.TempDir(), provider)
	config.Secrets, config.Bash = runtimeSecrets(), &secretBash{}
	config.Label = "label " + runtimeCanary
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "delegate"); err != nil {
		t.Fatal(err)
	}
	if !provider.childMasked || !provider.parentMasked {
		t.Fatal("child lost registry", provider)
	}
	for _, record := range session.Events() {
		if strings.Contains(record.Scope.Label, runtimeCanary) {
			t.Fatal("event scope leaked")
		}
		if event, ok := record.Event.Subagent(); ok {
			prompt, _ := event.Prompt()
			summary, _ := event.Summary()
			if strings.Contains(prompt+summary, runtimeCanary) {
				t.Fatal("event data leaked")
			}
		}
	}
}

func TestEventRecordingMasksCallerMetadataWithoutChangingAuthority(t *testing.T) {
	actor := ActorID(runtimeCanary)
	run, err := ExplicitHumanRunContext(HumanRunConfig{ActorID: &actor, Channel: runtimeCanary, StampedBy: runtimeCanary, ApprovedCapabilities: []RunCapability{RunCapability(runtimeCanary)}})
	if err != nil {
		t.Fatal(err)
	}
	events := &sessionEvents{secrets: runtimeSecrets()}
	events.setScope(EventScope{Label: runtimeCanary, RunContext: run})
	events.append(SessionEvent{kind: SessionEventKind(EventProviderStopUnhandled), stop: ProviderStopEvent{kind: EventProviderStopUnhandled, reason: protocol.StopReason(runtimeCanary), detail: runtimeCanary}})
	record := events.snapshot()[0]
	snapshot := record.Scope.RunContext.Snapshot()
	stop, _ := record.Event.Stop()
	if record.Scope.Label != secretpkg.Mask || snapshot.ActorID == nil || string(*snapshot.ActorID) != secretpkg.Mask || snapshot.Channel != secretpkg.Mask || snapshot.StampedBy != secretpkg.Mask || string(snapshot.ApprovedCapabilities[0]) != secretpkg.Mask || string(stop.Reason()) != secretpkg.Mask || stop.Detail() != secretpkg.Mask {
		t.Fatal("recording metadata leaked")
	}
	original := run.Snapshot()
	if string(*original.ActorID) != runtimeCanary || !run.Allows(RunCapability(runtimeCanary)) || run.Authority() != AuthorityExplicitHuman {
		t.Fatal("mask changed trusted caller context")
	}
}
