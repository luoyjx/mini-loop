package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type echoExecutor struct{}

func (echoExecutor) ExecuteBash(_ context.Context, input protocol.BashInput) (string, error) {
	return strings.TrimPrefix(input.Command, "echo "), nil
}

func TestFakeTurnAndSessionIsolation(t *testing.T) {
	a, err := NewSession("a", "owner-a", FakeProvider{}, echoExecutor{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSession("b", "owner-b", FakeProvider{}, echoExecutor{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, pair := range []struct {
		session *Session
		prompt  string
	}{
		{a, "alpha"}, {b, "beta"},
	} {
		group.Add(1)
		go func(session *Session, prompt string) {
			defer group.Done()
			answer, err := session.Run(context.Background(), prompt)
			if err != nil || answer != "Done. Tool said: handled: "+prompt {
				t.Errorf("%s: answer %q, error %v", prompt, answer, err)
			}
		}(pair.session, pair.prompt)
	}
	group.Wait()
	for _, session := range []*Session{a, b} {
		messages := session.Messages()
		if len(messages) != 4 || protocol.ValidateTranscript(messages) != nil {
			t.Errorf("session %s has invalid transcript", session.ID())
		}
	}
	if a.Owner() != "owner-a" || b.Owner() != "owner-b" {
		t.Fatal("owner identities crossed")
	}
}

type failingExecutor struct{}

func (failingExecutor) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	return "", errors.New("tool failed")
}

func TestToolFailureStillPairsTranscript(t *testing.T) {
	session, err := NewSession("a", "owner", FakeProvider{}, failingExecutor{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "prompt"); err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
	blocks, _ := session.Messages()[2].Content.Blocks()
	result, ok := blocks[0].ToolResult()
	if !ok || result.IsError {
		t.Fatal("failed tool response violated Python transcript shape")
	}
	assertToolResultTelemetry(t, session, result.ToolUseID, true, false)
}

func TestFakeProviderTruncatesByCharacters(t *testing.T) {
	prompt := strings.Repeat("界", 61)
	schema, _ := protocol.DefaultToolSchema(protocol.ToolBash)
	reply, err := (FakeProvider{}).Complete(context.Background(), protocol.ModelRequest{Model: DefaultModel, MaxTokens: DefaultMaxTokens, Purpose: protocol.PurposeAgentTurn, Tools: []protocol.ToolSchema{schema}, Messages: []protocol.Message{{
		Role: protocol.RoleUser, Content: protocol.PlainContent(prompt),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	use, ok := reply.Content[1].ToolUse()
	input, bash := use.Input.Bash()
	if !ok || !bash || input.Command != "echo handled: "+strings.Repeat("界", 60) {
		t.Fatalf("Python character truncation changed: %+v", use)
	}
}

type readFileProvider struct{}

func (readFileProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	messages := request.Messages
	if len(messages) > 1 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	return fakeReply([]protocol.Block{protocol.NewToolUse("u1", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "a.txt"}))}, protocol.StopToolUse), nil
}

type countedBashExecutor struct{ calls int }

func (executor *countedBashExecutor) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	executor.calls++
	return "unexpected", nil
}

func TestUnknownToolReturnsResultWithoutFallingThroughToBash(t *testing.T) {
	executor := &countedBashExecutor{}
	session, err := NewSession("s", "owner", readFileProvider{}, executor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "read a.txt"); err != nil {
		t.Fatal(err)
	}
	messages := session.Messages()
	if executor.calls != 0 || len(messages) != 4 {
		t.Fatal("unknown tool reached the bash executor or lacked a paired result")
	}
	blocks, _ := messages[2].Content.Blocks()
	result, _ := blocks[0].ToolResult()
	if result.IsError || result.Content != "Unknown tool: read_file" {
		t.Fatalf("unexpected unknown-tool result: %+v", result)
	}
	assertToolResultTelemetry(t, session, result.ToolUseID, true, false)
}
