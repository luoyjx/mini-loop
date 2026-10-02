package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type allRoleTools struct{}

func (allRoleTools) Select(_ AgentRole, parent *ToolCatalog) (*ToolCatalog, error) {
	return NewToolCatalog(parent.ordered...)
}

type recursiveTaskProvider struct {
	calls       int
	refusalSeen bool
}

func (provider *recursiveTaskProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.calls++
	last := request.Messages[len(request.Messages)-1]
	if _, plain := last.Content.Plain(); plain {
		role := protocol.AgentGeneralPurpose
		return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "nested delegation", AgentType: &role}))}, protocol.StopToolUse), nil
	}
	blocks, _ := last.Content.Blocks()
	for _, block := range blocks {
		if result, ok := block.ToolResult(); ok && strings.Contains(result.Content, "delegation refused") {
			provider.refusalSeen = true
		}
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestCustomRolePolicyCanRebindTaskButCannotRemoveDepthGate(t *testing.T) {
	provider := &recursiveTaskProvider{}
	config := runtimeConfig(t.TempDir(), provider)
	config.RoleToolPolicy = allRoleTools{}
	config.SubagentMaxRounds = 3
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.Run(context.Background(), "delegate")
	if err != nil || output != "done" || provider.calls != 6 || !provider.refusalSeen {
		t.Fatalf("recursive child did not bind/enforce quota: calls=%d refused=%v %v", provider.calls, provider.refusalSeen, err)
	}
	refusals := 0
	for _, record := range session.Events() {
		if record.Event.Kind() == EventSubagentRefused {
			refusals++
			if record.Scope.Depth != 2 || record.Scope.RunContext.Authority() != AuthorityPeerAgent {
				t.Fatal("nested refusal lost child provenance")
			}
		}
	}
	if refusals != 1 || protocol.ValidateTranscript(session.Messages()) != nil {
		t.Fatal("refusal did not preserve paired parent history")
	}
}

type mutatingExploreProvider struct{}
type subagentBashSpy struct{ calls int }

func (executor *subagentBashSpy) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	executor.calls++
	return "executed", nil
}

func (mutatingExploreProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	child := request.System != nil && strings.Contains(*request.System, " subagent in ")
	last := request.Messages[len(request.Messages)-1]
	if _, plain := last.Content.Plain(); plain {
		if child {
			return fakeReply([]protocol.Block{
				protocol.NewToolUse("write", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "unexpected", Content: "mutation"})),
				protocol.NewBashUse("shell", "echo mutation > unexpected-shell"),
			}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "explore"}))}, protocol.StopToolUse), nil
	}
	if child {
		blocks, _ := last.Content.Blocks()
		for _, block := range blocks {
			result, _ := block.ToolResult()
			if !result.IsError || !strings.Contains(result.Content, "read-only") {
				return protocol.ModelReply{}, errors.New("explore mutation was not denied")
			}
		}
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestExploreReadonlyHoldsWithWiderCustomCatalogueAndAutoParent(t *testing.T) {
	executor := &subagentBashSpy{}
	config := runtimeConfig(t.TempDir(), mutatingExploreProvider{})
	config.Bash = executor
	config.Mode = ModeAuto
	config.RoleToolPolicy = allRoleTools{}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "explore"); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatal("explore inherited auto execution")
	}
	for _, name := range []string{"unexpected", "unexpected-shell"} {
		if _, err := os.Stat(filepath.Join(session.workspace, name)); !os.IsNotExist(err) {
			t.Fatal("explore wrote a file")
		}
	}
}

type cancelChildProvider struct{ started chan struct{} }

func (provider cancelChildProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.System != nil && strings.Contains(*request.System, " subagent in ") {
		close(provider.started)
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	}
	return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "explore"}))}, protocol.StopToolUse), nil
}
func TestCancelledChildRepairsParentTaskAndEmitsNoCompletion(t *testing.T) {
	started := make(chan struct{})
	provider := cancelChildProvider{started}
	session, err := NewRuntimeSession(runtimeConfig(t.TempDir(), provider))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "delegate"); done <- err }()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	messages := session.Messages()
	if len(messages) != 3 || protocol.ValidateTranscript(messages) != nil {
		t.Fatal("cancelled task left orphaned parent call")
	}
	blocks, _ := messages[2].Content.Blocks()
	result, _ := blocks[0].ToolResult()
	if result.Content != unknownToolResult {
		t.Fatal("cancelled task pretended to complete")
	}
	for _, record := range session.Events() {
		if record.Event.Kind() == EventSubagentEnd {
			t.Fatal("cancelled child emitted completion")
		}
	}
}

func TestSharedSubagentProviderAndRolePolicyKeepSessionsIndependent(t *testing.T) {
	provider := &InProcessSubagents{}
	policy := DefaultRoleToolPolicy()
	sessions := make([]*Session, 2)
	for i := range sessions {
		config := runtimeConfig(t.TempDir(), FakeProvider{})
		config.ID = SessionID(string(rune('a' + i)))
		config.Owner = OwnerID(string(rune('x' + i)))
		config.Subagents = provider
		config.RoleToolPolicy = policy
		var err error
		sessions[i], err = NewRuntimeSession(config)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, session := range sessions {
		wait.Add(1)
		go func(session *Session) {
			defer wait.Done()
			_, err := session.Delegate(context.Background(), "inspect", RoleExplore)
			results <- err
		}(session)
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	ids := make(map[MessageID]bool)
	for _, session := range sessions {
		events := session.Events()
		if len(events) != 2 || events[0].Scope.Label != string(session.ID()) || len(session.Messages()) != 0 {
			t.Fatal("child state leaked into parent or other session")
		}
		id := events[0].Scope.RunContext.MessageID()
		if ids[id] {
			t.Fatal("sessions shared delegation context")
		}
		ids[id] = true
	}
}
