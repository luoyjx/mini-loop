package agent

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestToolSelectionPreservesSourceSubsetSemantics(t *testing.T) {
	for _, test := range []struct {
		name      string
		selection ToolSelection
		want      []protocol.ToolName
	}{
		{"order-duplicate-unknown", SelectTools(protocol.ToolGlob, protocol.ToolReadFile, protocol.ToolGlob, "missing"), []protocol.ToolName{protocol.ToolReadFile, protocol.ToolGlob}},
		{"empty", SelectTools(), nil},
		{"unknown", SelectTools("missing"), nil},
		{"cannot-enable-optional", SelectTools(protocol.ToolGoalCreate), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := runtimeConfig(t.TempDir(), &FakeProvider{})
			config.ToolSelection = test.selection
			s, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(s.gate.CatalogNames(), test.want) {
				t.Fatalf("catalogue: %v", s.gate.CatalogNames())
			}
			if s.permissionMode() != ModeInteractive {
				t.Fatal("selection changed permission mode")
			}
		})
	}
	config := runtimeConfig(t.TempDir(), &FakeProvider{})
	s, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.gate.CatalogNames()) != 10 {
		t.Fatal("zero selection changed defaults")
	}
}

type selectionProvider struct {
	t    *testing.T
	want []protocol.ToolName
}

func (p selectionProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	got := make([]protocol.ToolName, len(request.Tools))
	for i, schema := range request.Tools {
		got[i] = schema.Name
	}
	if !slices.Equal(got, p.want) {
		p.t.Errorf("advertised tools: %v, want %v", got, p.want)
	}
	return (resourceProvider{[]protocol.Block{
		protocol.NewToolUse("write", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "forbidden", Content: "bad"})),
		protocol.NewToolUse("bash", protocol.BashToolInput(protocol.BashInput{Command: "touch forbidden-bash"})),
		protocol.NewToolUse("read", protocol.ReadFileToolInput(protocol.ReadFileInput{Path: "proof"})),
	}}).Complete(context.Background(), request)
}

func TestSelectedToolsEnforceActualGateAndRetainReads(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "proof"), []byte("allowed evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	bash := &countedBashExecutor{}
	names := []protocol.ToolName{protocol.ToolReadFile, protocol.ToolGlob}
	config := runtimeConfig(root, selectionProvider{t, names})
	config.Mode, config.Bash, config.ToolSelection = ModeAuto, bash, SelectTools(names...)
	s, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delegate(context.Background(), "probe", RoleExplore); err != nil {
		t.Fatal(err)
	}
	if bash.calls != 0 {
		t.Fatal("excluded bash reached executor")
	}
	if _, err := os.Stat(filepath.Join(root, "forbidden")); !os.IsNotExist(err) {
		t.Fatal("excluded write reached filesystem", err)
	}
	messages := s.Messages()
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	blocks, _ := messages[2].Content.Blocks()
	for i, block := range blocks {
		result, _ := block.ToolResult()
		if i < 2 && !strings.Contains(result.Content, "Unknown tool") {
			t.Fatalf("excluded result: %+v", result)
		}
		if i == 2 && !strings.Contains(result.Content, "allowed evidence") {
			t.Fatalf("read failed: %+v", result)
		}
	}
	if len(blocks) != 3 {
		t.Fatal("missing paired results")
	}
}

func TestExplicitEmptySelectionRefusesForcedTools(t *testing.T) {
	config := runtimeConfig(t.TempDir(), selectionProvider{t, nil})
	config.ToolSelection, config.Mode = SelectTools(), ModeAuto
	s, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background(), "probe"); err != nil {
		t.Fatal(err)
	}
	blocks, _ := s.Messages()[2].Content.Blocks()
	if len(blocks) != 3 {
		t.Fatal("missing refused results")
	}
	for _, block := range blocks {
		result, _ := block.ToolResult()
		if !strings.Contains(result.Content, "Unknown tool") {
			t.Fatalf("tool ran: %+v", result)
		}
	}
	entries, err := os.ReadDir(config.Workspace)
	if err != nil || len(entries) != 0 {
		t.Fatal("empty selection touched workspace", entries, err)
	}
}

func TestManagerCapturesDetachedSelectionBeforeFactories(t *testing.T) {
	root := t.TempDir()
	names := []protocol.ToolName{protocol.ToolReadFile}
	selection := SelectTools(names...)
	names[0] = protocol.ToolWriteFile
	config := managerTestConfig(root, &FakeProvider{})
	config.WorkspaceFactory = workspaceFactoryFunc(func(_ context.Context, id SessionID) (string, error) {
		selection = SelectTools(protocol.ToolBash)
		return filepath.Join(root, string(id)), nil
	})
	m := makeManager(t, config)
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner", ToolSelection: selection})
	if !slices.Equal(s.core.gate.CatalogNames(), []protocol.ToolName{protocol.ToolReadFile}) {
		t.Fatal("factory/caller changed captured selection")
	}
	other := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	if len(other.core.gate.CatalogNames()) != 10 {
		t.Fatal("selection leaked to another session")
	}
}
