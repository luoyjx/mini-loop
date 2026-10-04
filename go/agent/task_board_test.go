package agent

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/tasks"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type taskToolFixture struct {
	Tools []struct {
		Name   protocol.ToolName
		Input  json.RawMessage
		Output string
	}
	Schemas  []protocol.ToolSchema
	Metadata []struct {
		Name         protocol.ToolName
		Readonly     bool
		Risk         ToolRisk
		Capabilities []Capability
	}
}
type taskFlowProvider struct {
	stage   int
	fixture *taskToolFixture
	id      string
}

func (p *taskFlowProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if p.stage == 0 {
		p.stage++
		input, err := protocol.DecodeToolInput(p.fixture.Tools[0].Name, p.fixture.Tools[0].Input)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		return fakeReply([]protocol.Block{protocol.NewToolUse("create", input)}, protocol.StopToolUse), nil
	}
	if p.stage == 1 {
		p.stage++
		for _, message := range request.Messages {
			blocks, _ := message.Content.Blocks()
			for _, block := range blocks {
				if result, ok := block.ToolResult(); ok && strings.HasPrefix(result.Content, "Created ") {
					p.id = strings.Fields(result.Content)[1]
					p.id = strings.TrimSuffix(p.id, ":")
				}
			}
		}
		var uses []protocol.Block
		for index, row := range p.fixture.Tools[1:] {
			data := strings.ReplaceAll(string(row.Input), "task_000000000001", p.id)
			input, err := protocol.DecodeToolInput(row.Name, []byte(data))
			if err != nil {
				return protocol.ModelReply{}, err
			}
			uses = append(uses, protocol.NewToolUse(string(rune('a'+index)), input))
		}
		return fakeReply(uses, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}
func TestOptionalTaskToolsMatchActualPythonThroughSingleGate(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture taskToolFixture
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Tools) != 6 || len(fixture.Schemas) != 5 || len(fixture.Metadata) != 5 {
		t.Fatal("incomplete Python task fixture")
	}
	provider := &taskFlowProvider{fixture: &fixture}
	config := runtimeConfig(t.TempDir(), provider)
	config.TaskTools = true
	config.Label = "main"
	config.Mode = ModeAuto
	config.MaxRounds = 3
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	for index, schema := range protocol.TaskBoardSchemas() {
		if !reflect.DeepEqual(schema, fixture.Schemas[index]) {
			t.Fatalf("schema %s differs", schema.Name)
		}
	}
	for _, row := range fixture.Metadata {
		definition, ok := session.gate.catalog.Lookup(row.Name)
		if !ok || definition.Risk() != row.Risk || definition.Readonly() != row.Readonly || len(definition.Capabilities()) != len(row.Capabilities) {
			t.Fatalf("traits %s differ", row.Name)
		}
	}
	if _, err = session.Run(context.Background(), "task graph"); err != nil {
		t.Fatal(err)
	}
	if err = protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
	var outputs []string
	for _, message := range session.Messages() {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if result, ok := block.ToolResult(); ok {
				if result.IsError {
					t.Fatal(result.Content)
				}
				outputs = append(outputs, strings.ReplaceAll(result.Content, provider.id, "task_000000000001"))
			}
		}
	}
	if len(outputs) != len(fixture.Tools) {
		t.Fatalf("result count %d", len(outputs))
	}
	for index, output := range outputs {
		if output != fixture.Tools[index].Output {
			t.Fatalf("tool %d differs: %s / %s", index, output, fixture.Tools[index].Output)
		}
	}
	loaded, err := tasks.New(tasks.Config{Workspace: config.Workspace})
	if err != nil {
		t.Fatal(err)
	}
	record, err := loaded.Load(tasks.ID(provider.id))
	if err != nil || record == nil || record.Status != tasks.Completed || record.Owner == nil || *record.Owner != "main" {
		t.Fatal(record, err)
	}
	// Optional writes retain the readonly gate; constructing a session alone does
	// not create the persistent board. The default inventory stays at ten.
	value, _ := protocol.DecodeToolInput(protocol.ToolCreateTask, fixture.Tools[0].Input)
	readonly := runtimeConfig(t.TempDir(), resourceProvider{tools: []protocol.Block{protocol.NewToolUse("denied", value)}})
	readonly.TaskTools = true
	readonly.Mode = ModeReadonly
	readSession, err := NewRuntimeSession(readonly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readSession.Run(context.Background(), "deny mutation"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(readonly.Workspace, ".tasks")); !os.IsNotExist(err) {
		t.Fatal("readonly tool created a board")
	}
	disabled := runtimeConfig(t.TempDir(), resourceProvider{})
	plain, err := NewRuntimeSession(disabled)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.gate.catalog.Names()) != 10 {
		t.Fatal("default inventory changed")
	}
}

func TestManagerTaskBoardsAreExplicitIsolatedAndFreshOnFork(t *testing.T) {
	input := protocol.CreateTaskToolInput(protocol.CreateTaskInput{Subject: "own work"})
	provider := resourceProvider{tools: []protocol.Block{protocol.NewToolUse("create", input)}}
	manager, err := NewSessionManager(ManagerConfig{WorkspaceRoot: t.TempDir(), Services: ManagerServices{Provider: provider, TaskTools: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background())
	var boards []string
	for _, owner := range []OwnerID{"alice", "bob"} {
		session, err := manager.Create(context.Background(), CreateSessionRequest{Owner: owner, PermissionMode: ModeAuto})
		if err != nil {
			t.Fatal(err)
		}
		if len(session.core.gate.catalog.Names()) != 15 {
			t.Fatal("manager dropped optional graph tools")
		}
		if _, err = session.Run(context.Background(), "create own"); err != nil {
			t.Fatal(err)
		}
		store, err := tasks.New(tasks.Config{Workspace: session.Info().Workspace})
		if err != nil {
			t.Fatal(err)
		}
		list, err := store.List()
		if err != nil || len(list) != 1 {
			t.Fatal(list, err)
		}
		boards = append(boards, store.Root())
		if owner == "alice" {
			child, err := manager.Fork(context.Background(), owner, session.ID())
			if err != nil {
				t.Fatal(err)
			}
			if len(child.core.gate.catalog.Names()) != 15 {
				t.Fatal("fork lost tool activation")
			}
			if _, err = os.Stat(filepath.Join(child.Info().Workspace, ".tasks")); !os.IsNotExist(err) {
				t.Fatal("fork borrowed parent board")
			}
		}
	}
	if boards[0] == boards[1] {
		t.Fatal("owner workspaces share a board")
	}
}
