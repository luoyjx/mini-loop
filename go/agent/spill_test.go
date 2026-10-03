package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/spill"
)

func TestActualPythonDefaultStructuredBashDoesNotUseStringSpillPolicy(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-spill.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		DefaultAdapter struct {
			Command, Final string
			Preserved      int
			RenderSHA256   string `json:"render_sha256"`
			RenderChars    int    `json:"render_chars"`
		} `json:"default_adapter"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	store, err := spill.NewLocalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root, Spill: store})
	if err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, resourceProvider{tools: []protocol.Block{protocol.NewBashUse("spill-tool", fixture.DefaultAdapter.Command)}})
	config.Bash, config.Mode = executor, ModeAuto
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	final, err := session.Run(context.Background(), "spill probe")
	if err != nil || final != fixture.DefaultAdapter.Final {
		t.Fatal(final, err)
	}
	messages := session.Messages()
	blocks, ok := messages[len(messages)-2].Content.Blocks()
	if !ok || len(blocks) != 1 {
		t.Fatal("missing paired result")
	}
	result, ok := blocks[0].ToolResult()
	if !ok {
		t.Fatal("missing tool result")
	}
	sum := sha256.Sum256([]byte(result.Content))
	if hex.EncodeToString(sum[:]) != fixture.DefaultAdapter.RenderSHA256 || utf8.RuneCountInString(result.Content) != fixture.DefaultAdapter.RenderChars {
		t.Fatal("default adapter diverges from source")
	}
	paths, err := filepath.Glob(filepath.Join(store.Root(), "session-*", "*.txt"))
	if err != nil || len(paths) != fixture.DefaultAdapter.Preserved {
		t.Fatal("structured adapter started a different policy", paths, err)
	}
}

func TestManagerSpillBindingKeepsCredentialScopeAndForkNamespace(t *testing.T) {
	store, err := spill.NewLocalStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("DEMO", "fixture-secret")
	var original *shell.Executor
	config := managerTestConfig(filepath.Join(t.TempDir(), "workspaces"), NewFakeProvider(FakeProviderConfig{}))
	config.Services.Spill, config.Services.Secrets = store, registry
	config.Services.BashFactory = bashFactoryFunc(func(ctx context.Context, binding SessionBinding) (BashExecutor, error) {
		executor, err := shell.New(shell.Config{Workspace: binding.Workspace})
		original = executor
		return executor, err
	})
	manager := makeManager(t, config)
	session := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	input := protocol.BashInput{Command: "awk 'BEGIN {for(i=0;i<60000;i++)printf \"A\"}'; printf 'fixture-secret'"}
	projected, err := session.core.bash.ExecuteBash(context.Background(), input)
	if err != nil || strings.Contains(projected, "fixture-secret") || !strings.Contains(projected, "full output preserved:") {
		t.Fatal("manager lost scoped preservation", err)
	}
	plain, err := original.ExecuteBash(context.Background(), input)
	if err != nil || !strings.Contains(plain, "fixture-secret") || strings.Contains(plain, "full output preserved:") {
		t.Fatal("binding mutated caller executor", err)
	}
	child, err := manager.Fork(context.Background(), "owner", session.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.core.bash.ExecuteBash(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	directories, err := os.ReadDir(store.Root())
	if err != nil || len(directories) != 2 {
		t.Fatal("fork did not preserve fresh workspace namespace", directories, err)
	}
	paths, _ := filepath.Glob(filepath.Join(store.Root(), "session-*", "*.txt"))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(raw), "fixture-secret") || !strings.Contains(string(raw), secrets.Mask) {
			t.Fatal("scoped masking lost in fork", err)
		}
	}
}
