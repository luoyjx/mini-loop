package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
)

type backgroundToolFixture struct {
	Schemas  []protocol.ToolSchema
	Metadata []struct {
		Name                   protocol.ToolName
		Readonly, ParallelSafe bool
		Risk                   ToolRisk
		Capabilities           []Capability
	}
	Variants []struct {
		Name                protocol.ToolName
		Input               json.RawMessage
		Candidate, Proposed []string
	}
	Modes []struct {
		Enabled bool
		Input   json.RawMessage
		Mode    ExecutionMode
	}
	Steps []struct {
		Name   protocol.ToolName
		Input  json.RawMessage
		Output string
	}
	Disabled struct {
		Input      json.RawMessage
		Output     string
		HasManager bool `json:"has_manager"`
	}
	Interruption struct {
		Text             string
		Marked, Repaired bool
	}
}

func backgroundFixture(t *testing.T) backgroundToolFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-background-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var f backgroundToolFixture
	if err = json.Unmarshal(data, &f); err != nil || len(f.Steps) != 15 {
		t.Fatal("incomplete background source fixture", err)
	}
	return f
}
func backgroundRuntime(t *testing.T, root string, p Provider, enabled bool) *Session {
	t.Helper()
	native, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, p)
	config.Bash = native
	config.BackgroundTools = enabled
	config.MaxRounds = 4
	config.Mode = ModeAuto
	config.Label = "main"
	s, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.CloseBackground(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func backgroundDispatch(t *testing.T, s *Session, input protocol.ToolInput) ToolOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := s.gate.Dispatch(ctx, ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: s.permissionMode()}, ToolCall{ID: "native-bg", Input: input})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func backgroundManager(t *testing.T, s *Session) *background.Manager {
	t.Helper()
	manager, err := s.background.get(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}
func TestBackgroundToolsMatchActualSourceGateSchemasAndClassifiers(t *testing.T) {
	f := backgroundFixture(t)
	s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, true)
	if !reflect.DeepEqual(protocol.BackgroundSchemas(), f.Schemas) {
		t.Fatal("optional background schemas differ")
	}
	for _, row := range f.Metadata {
		def, ok := s.gate.catalog.Lookup(row.Name)
		if !ok || def.Risk() != row.Risk || def.Readonly() != row.Readonly || def.ParallelSafe() != row.ParallelSafe || len(def.Capabilities()) != len(row.Capabilities) {
			t.Fatal("source metadata differs", row.Name)
		}
	}
	for _, row := range f.Variants {
		input, err := protocol.DecodeToolInput(row.Name, row.Input)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(DefaultGrantCandidate(input).Tokens(), row.Candidate) || !slices.Equal(ProposedGrantCandidate(input).Tokens(), row.Proposed) {
			t.Fatal("shell grant identity differs", row.Name, string(row.Input))
		}
	}
	for _, row := range f.Modes {
		selected := s
		if !row.Enabled {
			selected = backgroundRuntime(t, t.TempDir(), &recordingProvider{}, false)
		}
		input, err := protocol.DecodeToolInput(protocol.ToolBash, row.Input)
		if err != nil {
			t.Fatal(err)
		}
		def, _ := selected.gate.catalog.Lookup(protocol.ToolBash)
		if got := def.ExecutionMode(ToolCall{ID: "classifier", Input: input}); got != row.Mode {
			t.Fatal("per-call source classifier differs", got, row.Mode)
		}
	}
	for index, row := range f.Steps {
		input, err := protocol.DecodeToolInput(row.Name, row.Input)
		if err != nil {
			t.Fatal(err)
		}
		out := backgroundDispatch(t, s, input)
		if out.Output != row.Output {
			t.Fatalf("step %d %s: %q != %q", index, row.Name, out.Output, row.Output)
		}
		if strings.HasPrefix(out.Output, "Started background task") {
			id := background.ID(strings.TrimSuffix(strings.Fields(out.Output)[3], ":"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := backgroundManager(t, s).Wait(ctx, id)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := out.CommandResult(); ok {
				t.Fatal("queued command fabricated foreground process metadata")
			}
		}
		if row.Name == protocol.ToolBash && strings.Contains(row.Output, "foreground") {
			meta, ok := out.CommandResult()
			if !ok || meta.ExitCode == nil || *meta.ExitCode != 3 {
				t.Fatal("enabled background lost foreground failure metadata")
			}
		}
	}
	disabled := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, false)
	input, _ := protocol.DecodeToolInput(protocol.ToolBash, f.Disabled.Input)
	if out := backgroundDispatch(t, disabled, input); out.Output != f.Disabled.Output || disabled.background != nil {
		t.Fatal("disabled background flag changed foreground semantics", out)
	}
}

type backgroundNotifyProvider struct {
	s     *Session
	stage int
	saw   bool
}

func (p *backgroundNotifyProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.stage++
	switch p.stage {
	case 1:
		return fakeReply([]protocol.Block{protocol.NewToolUse("start-bg", protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "printf completion"}))}, protocol.StopToolUse), nil
	case 2:
		manager, err := p.s.background.get(ctx, false)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		if err = manager.Wait(ctx, "bg_0001"); err != nil {
			return protocol.ModelReply{}, err
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("check-bg", protocol.CheckBackgroundToolInput(protocol.CheckBackgroundInput{}))}, protocol.StopToolUse), nil
	default:
		for _, m := range request.Messages {
			if text, ok := m.Content.Plain(); ok && strings.Contains(text, "<task_notification id=\"bg_0001\" status=\"completed\">\ncompletion\n</task_notification>") {
				p.saw = true
			}
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
}
func TestBackgroundCompletionEntersNextModelRequestAndTypedEvent(t *testing.T) {
	p := &backgroundNotifyProvider{}
	s := backgroundRuntime(t, t.TempDir(), p, true)
	p.s = s
	if _, err := s.Run(context.Background(), "background"); err != nil {
		t.Fatal(err)
	}
	if !p.saw {
		t.Fatal("completion never entered model request")
	}
	count := 0
	for _, r := range s.Events() {
		if value, ok := r.Event.BackgroundResult(); ok {
			count++
			if value.Count != 1 || value.Dropped != 0 {
				t.Fatal(value)
			}
			data, err := json.Marshal(r)
			if err != nil || !strings.Contains(string(data), `"type":"background_result"`) || !strings.Contains(string(data), `"count":1`) || !strings.Contains(string(data), `"dropped":0`) {
				t.Fatal(string(data), err)
			}
		}
	}
	if count != 1 {
		t.Fatal("completion was duplicated or hidden", count)
	}
	if err := protocol.ValidateTranscript(s.Messages()); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundOrphansSurfaceWithoutToolCallAndNewestBatchIsBounded(t *testing.T) {
	root := t.TempDir()
	ledger := filepath.Join(root, ".background")
	if err := os.Mkdir(ledger, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 53; i++ {
		name := filepath.Join(ledger, backgroundIDForTest(i+1)+".json")
		if err := os.WriteFile(name, []byte(`{"command":"orphan","pid":null}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := backgroundRuntime(t, root, &recordingProvider{}, true)
	if s.background.manager != nil {
		t.Fatal("background was eagerly constructed")
	}
	if err := s.injectBackground(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(s.Messages()) != 1 {
		t.Fatal("orphan notification absent")
	}
	text, ok := s.Messages()[0].Content.Plain()
	if !ok || strings.Count(text, "<task_notification ") != 50 || !strings.Contains(text, "3 earlier background result(s) omitted") || strings.Contains(text, `id="bg_0001"`) {
		t.Fatal("notification cap/order changed")
	}
	if s.background.manager.Check("bg_0001") == "Unknown: bg_0001" {
		t.Fatal("omitted orphan became undiscoverable")
	}
	before := len(s.Messages())
	if err := s.injectBackground(context.Background()); err != nil || len(s.Messages()) != before {
		t.Fatal("orphan was redelivered", err)
	}
	value, ok := s.Events()[0].Event.BackgroundResult()
	if !ok || value.Count != 50 || value.Dropped != 3 {
		t.Fatal(value)
	}
}
func backgroundIDForTest(n int) string {
	return "bg_" + strings.Repeat("0", 4-len(strconv.Itoa(n))) + strconv.Itoa(n)
}

type backgroundCancelProvider struct {
	s     *ManagedSession
	stage int
	ready chan struct{}
}

func (p *backgroundCancelProvider) Complete(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	p.stage++
	if p.stage == 1 {
		return fakeReply([]protocol.Block{protocol.NewToolUse("survive", protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "sleep 30"}))}, protocol.StopToolUse), nil
	}
	p.s.core.streamedText = "partial source"
	close(p.ready)
	<-ctx.Done()
	return protocol.ModelReply{}, ctx.Err()
}
func TestCancelledManagedTurnNamesBackgroundSurvivorAndLeavesItOwned(t *testing.T) {
	f := backgroundFixture(t)
	p := &backgroundCancelProvider{ready: make(chan struct{})}
	root := t.TempDir()
	native, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, p)
	config.Bash = native
	config.BackgroundTools = true
	config.Mode = ModeAuto
	s, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	p.s = s
	t.Cleanup(func() { s.CloseBackground(context.Background()) })
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "survive"); done <- err }()
	select {
	case <-p.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never reached cancellation boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ok, err := s.Cancel(ctx, "source bg cancel"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.core.backgroundLive() != 1 {
		t.Fatal("turn cancellation ended background ownership")
	}
	messages := s.Messages()
	blocks, _ := messages[len(messages)-1].Content.Blocks()
	text, _ := blocks[0].Text()
	if !f.Interruption.Marked || f.Interruption.Repaired || text.Text != f.Interruption.Text {
		t.Fatal("actual source interruption marker differs", text.Text, f.Interruption.Text)
	}
	before := len(messages)
	s.core.repairedToolUses = []string{"pending"}
	s.core.recordInterruption("source bg cancel")
	if len(s.Messages()) != before {
		t.Fatal("repaired transcript got a duplicate marker")
	}
	if err := s.CloseBackground(ctx); err != nil || s.core.backgroundLive() != 0 {
		t.Fatal("explicit close failed to join", err)
	}
}

func TestBackgroundUsesCommonPermissionAndMaskedCredentialScope(t *testing.T) {
	root := t.TempDir()
	native, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("BG_API_KEY", "long-private-credential")
	config := runtimeConfig(root, &recordingProvider{})
	config.Bash = native
	config.BackgroundTools = true
	config.Secrets = registry
	config.Mode = ModeReadonly
	s, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.CloseBackground(context.Background()) })
	out := backgroundDispatch(t, s, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "printf denied > proof"}))
	if !out.Denied || s.background.manager != nil {
		t.Fatal("readonly background bypassed permission or laziness", out)
	}
	if _, err := os.Stat(filepath.Join(s.workspace, ".background")); !os.IsNotExist(err) {
		t.Fatal("denial wrote ledger")
	}
	s.mode = ModeInteractive
	out = backgroundDispatch(t, s, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "rm -rf proof"}))
	if !out.Denied {
		t.Fatal("destructive background skipped approval")
	}
	s.mode = ModeAuto
	out = backgroundDispatch(t, s, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: `printf '%s' "$BG_API_KEY"`}))
	if !strings.HasPrefix(out.Output, "Started") {
		t.Fatal(out)
	}
	manager := backgroundManager(t, s)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Wait(ctx, "bg_0001"); err != nil {
		t.Fatal(err)
	}
	out = backgroundDispatch(t, s, protocol.CheckBackgroundToolInput(protocol.CheckBackgroundInput{ID: stringForBackgroundTest("bg_0001")}))
	if out.Output != "[completed] "+secrets.Mask {
		t.Fatal("background credential scope escaped", out)
	}
	// Foreign authority cannot borrow the service even after a valid grant.
	def, _ := s.gate.catalog.Lookup(protocol.ToolBackgroundRun)
	if _, err := def.handler.ExecuteTool(ctx, ToolAuthority{SessionID: "foreign", OwnerID: s.owner, Workspace: s.workspace, Mode: ModeAuto}, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: ":"})); err == nil {
		t.Fatal("foreign service authority was borrowed")
	}
}
func stringForBackgroundTest(s string) *string { return &s }

func TestBackgroundExecutionRebindPinsInflightTaskAndLedger(t *testing.T) {
	s := backgroundRuntime(t, t.TempDir(), &recordingProvider{}, true)
	old := s.workspace
	next := t.TempDir()
	out := backgroundDispatch(t, s, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "while [ ! -f release ]; do sleep .01; done; pwd"}))
	if !strings.HasPrefix(out.Output, "Started") {
		t.Fatal(out)
	}
	manager := backgroundManager(t, s)
	def, _ := s.gate.catalog.Lookup(protocol.ToolBackgroundRun)
	handler := def.handler.(*runtimeHandler)
	handler.mu.Lock()
	err := handler.enterWorkspace(context.Background(), next)
	handler.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	out = backgroundDispatch(t, s, protocol.BackgroundRunToolInput(protocol.BackgroundRunInput{Command: "pwd"}))
	if !strings.HasPrefix(out.Output, "Started") {
		t.Fatal(out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Wait(ctx, "bg_0002"); err != nil {
		t.Fatal(err)
	}
	if got := manager.Check("bg_0002"); got != "[completed] "+s.executionRoot() {
		t.Fatal("future background did not rebind", got, s.executionRoot())
	}
	if _, err := os.Stat(filepath.Join(old, ".background/bg_0001.json")); err != nil {
		t.Fatal("ledger moved from original root", err)
	}
	if err := os.WriteFile(filepath.Join(old, "release"), []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Wait(ctx, "bg_0001"); err != nil {
		t.Fatal(err)
	}
	if got := manager.Check("bg_0001"); got != "[completed] "+old {
		t.Fatal("inflight task changed execution root", got)
	}
}

type backgroundChildProvider struct {
	result      string
	sawOptional bool
}

func (p *backgroundChildProvider) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	for _, tool := range r.Tools {
		if tool.Name == protocol.ToolBackgroundRun || tool.Name == protocol.ToolCheckBackground {
			p.sawOptional = true
		}
	}
	if len(r.Messages) == 1 {
		flag := true
		return fakeReply([]protocol.Block{protocol.NewToolUse("child-native", protocol.BashToolInput(protocol.BashInput{Command: "printf child", RunInBackground: &flag}))}, protocol.StopToolUse), nil
	}
	blocks, _ := r.Messages[len(r.Messages)-1].Content.Blocks()
	for _, block := range blocks {
		if value, ok := block.ToolResult(); ok {
			p.result = value.Content
		}
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("child done")}, protocol.StopEndTurn), nil
}
func TestBackgroundParentKeepsDefaultChildBashNativeAndStateFresh(t *testing.T) {
	p := &backgroundChildProvider{}
	s := backgroundRuntime(t, t.TempDir(), p, true)
	if out, err := s.Delegate(context.Background(), "child", RoleWorker); err != nil || out != "child done" {
		t.Fatal(out, err)
	}
	if p.result != "child" || p.sawOptional || s.background.manager != nil {
		t.Fatal("child borrowed background catalogue/state or lost native executor", p.result, p.sawOptional)
	}
}
func TestBackgroundActivationRequiresNativeMatchingScope(t *testing.T) {
	root := t.TempDir()
	native, err := shell.New(shell.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var nilNative *shell.Executor
	for _, executor := range []BashExecutor{echoExecutor{}, native, nilNative} {
		config := runtimeConfig(root, &recordingProvider{})
		config.BackgroundTools = true
		config.Bash = executor
		if _, err := NewRuntimeSession(config); err == nil {
			t.Fatal("background accepted unbound/mismatched/native-nil executor")
		}
	}
}
