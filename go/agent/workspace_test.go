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

type fileWorkflowProvider struct{}

func (fileWorkflowProvider) Complete(_ context.Context, messages []protocol.Message) (protocol.ModelReply, error) {
	if len(messages) > 1 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	return fakeReply([]protocol.Block{
		protocol.NewToolUse("w", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "nested/note.txt", Content: "你好\n"})),
		protocol.NewToolUse("e", protocol.EditFileToolInput(protocol.EditFileInput{Path: "nested/note.txt", OldText: "你好", NewText: "新内容"})),
		protocol.NewToolUse("r", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "nested/note.txt"})),
	}, protocol.StopToolUse), nil
}

func TestWorkspaceSessionUsesTypedFileHandlers(t *testing.T) {
	session, err := NewWorkspaceSession("s", "owner", fileWorkflowProvider{}, echoExecutor{}, t.TempDir(), ModeInteractive, 2)
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.Run(context.Background(), "write edit read")
	if err != nil || output != "done" {
		t.Fatalf("turn=%q %v", output, err)
	}
	messages := session.Messages()
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	results, _ := messages[2].Content.Blocks()
	want := []string{"Wrote 3 bytes to nested/note.txt", "Edited nested/note.txt", "新内容"}
	if len(results) != len(want) {
		t.Fatalf("results=%+v", results)
	}
	for i, block := range results {
		result, ok := block.ToolResult()
		if !ok || result.IsError || result.Content != want[i] {
			t.Fatalf("result %d: %+v", i, result)
		}
	}
}

func TestReadonlyWorkspaceSessionDeniesChangesAndAllowsRead(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "note.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := NewWorkspaceSession("s", "owner", fileWorkflowProvider{}, echoExecutor{}, root, ModeReadonly, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "write edit read"); err != nil {
		t.Fatal(err)
	}
	results, _ := session.Messages()[2].Content.Blocks()
	for _, block := range results[:2] {
		result, _ := block.ToolResult()
		if !result.IsError || !strings.Contains(result.Content, "read-only") {
			t.Fatalf("write was allowed: %+v", result)
		}
	}
	read, _ := results[2].ToolResult()
	if read.IsError || read.Content != "original" {
		t.Fatalf("read failed: %+v", read)
	}
}

func TestWorkspaceCatalogMatchesPythonMetadata(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-default-tool-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var metadata []struct {
		Name         protocol.ToolName `json:"name"`
		Risk         ToolRisk          `json:"risk"`
		Readonly     bool              `json:"readonly"`
		ParallelSafe bool              `json:"parallel_safe"`
		Capabilities []Capability      `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewWorkspaceToolCatalog(echoExecutor{}, files)
	if err != nil {
		t.Fatal(err)
	}
	want := []protocol.ToolName{protocol.ToolBash, protocol.ToolReadFile, protocol.ToolWriteFile, protocol.ToolEditFile}
	if !reflect.DeepEqual(catalog.Names(), want) {
		t.Fatalf("registered names=%v", catalog.Names())
	}
	for _, expected := range metadata[:4] {
		definition, ok := catalog.Lookup(expected.Name)
		if !ok || definition.Risk() != expected.Risk || definition.Readonly() != expected.Readonly || definition.ParallelSafe() != expected.ParallelSafe || !reflect.DeepEqual(definition.Capabilities(), expected.Capabilities) {
			t.Fatalf("metadata drift for %s", expected.Name)
		}
	}
}

func TestFileBackendChecksPathAgainAfterApproval(t *testing.T) {
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	inside := filepath.Join(files.Root(), "inside")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(files.Root(), "link")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewWorkspaceToolCatalog(echoExecutor{}, files)
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := NewPermissionRule("boundary", "Path escapes workspace", RuleDeny,
		func(authority ToolAuthority, call ToolCall, _ ToolRisk, _ bool) bool {
			return pathEscapesWorkspace(authority, call)
		})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := NewPermissionRule("ask", "Approve write", RuleAsk,
		func(ToolAuthority, ToolCall, ToolRisk, bool) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPermissionPolicy([]PermissionRule{boundary, ask}, approverFunc(func(context.Context, ApprovalRequest) (bool, error) {
		if err := os.Remove(link); err != nil {
			return false, err
		}
		if err := os.Symlink(outside, link); err != nil {
			return false, err
		}
		return true, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(catalog, policy, GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	authority := gateTestAuthority(ModeInteractive)
	authority.Workspace = files.Root()
	call := ToolCall{ID: "w", Input: protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "link/escaped.txt", Content: "bad"})}
	outcome, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !outcome.Failed || !strings.Contains(outcome.Output, "Path escapes workspace") {
		t.Fatalf("changed path was executed: %+v %v", outcome, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside workspace changed: %+v %v", entries, err)
	}
}

func TestFileHandlerRejectsDifferentWorkspaceAuthority(t *testing.T) {
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewWorkspaceToolCatalog(echoExecutor{}, files)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	authority := gateTestAuthority(ModeAuto)
	authority.Workspace = t.TempDir()
	call := ToolCall{ID: "w", Input: protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "note.txt", Content: "bad"})}
	outcome, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || !outcome.Failed || !strings.Contains(outcome.Output, "does not match tool authority") {
		t.Fatalf("authority mismatch was accepted: %+v %v", outcome, err)
	}
	entries, err := os.ReadDir(files.Root())
	if err != nil || len(entries) != 0 {
		t.Fatalf("bound workspace changed: %+v %v", entries, err)
	}
}
