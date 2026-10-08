package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/astcontext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func init() {
	if os.Getenv("AST_AGENT_CHILD") != "1" {
		return
	}
	args := os.Args[1:]
	cwd, _ := os.Getwd()
	file, err := os.OpenFile(os.Getenv("AST_AGENT_LOG"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(2)
	}
	_ = json.NewEncoder(file).Encode(struct {
		Args []string
		Cwd  string
	}{args, cwd})
	_ = file.Close()
	if len(args) == 1 && args[0] == "--version" {
		fmt.Printf("ast-outline 1.9.0\nhomepage: %s\n", astcontext.Homepage)
		os.Exit(0)
	}
	selected := ""
	for i, arg := range args {
		if arg == "--" && i+1 < len(args) {
			selected = args[i+1]
			break
		}
	}
	body, err := os.ReadFile(selected)
	if err != nil {
		os.Exit(3)
	}
	info, err := os.Stat(selected)
	if err != nil || info.Mode().Perm() != 0400 {
		os.Exit(4)
	}
	if strings.HasPrefix(selected, cwd+string(filepath.Separator)) {
		os.Exit(5)
	}
	_ = json.NewEncoder(os.Stdout).Encode(struct {
		Tool    string `json:"tool"`
		Schema  int    `json:"schema_version"`
		Command string `json:"command"`
		Files   []struct {
			Path    string `json:"path"`
			Text    string `json:"text"`
			Matches []int  `json:"matches"`
		} `json:"files"`
		Results []struct {
			Matches []int `json:"matches"`
		} `json:"results"`
		Cwd string `json:"cwd"`
	}{"ast-outline", 1, args[0], []struct {
		Path    string `json:"path"`
		Text    string `json:"text"`
		Matches []int  `json:"matches"`
	}{{selected, string(body), []int{1}}}, []struct {
		Matches []int `json:"matches"`
	}{{[]int{1}}}, cwd})
	os.Exit(0)
}
func nativeASTConfig(t *testing.T) (astcontext.Config, string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	t.Setenv("AST_AGENT_CHILD", "1")
	t.Setenv("AST_AGENT_LOG", log)
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	p := astcontext.DefaultParameters()
	p.Binary = binary
	p.EnvPassthrough = []string{"AST_AGENT_CHILD", "AST_AGENT_LOG", "GORACE"}
	config, err := astcontext.NewConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	return config, log
}
func astModelCalls(t *testing.T) []protocol.Block {
	t.Helper()
	result := []protocol.Block{}
	for _, test := range []struct {
		name protocol.ToolName
		raw  string
	}{{protocol.ToolRepoMap, `{"paths":["code.py"]}`}, {protocol.ToolFileOutline, `{"paths":["code.py"],"view":"minimal"}`}, {protocol.ToolShowSymbol, `{"path_or_glob":"code.py","symbols":["One"],"signature_only":true}`}, {protocol.ToolSymbolReferences, `{"patterns":["-Option"],"paths":["code.py"],"kinds":["def","call"],"max_per_file":7}`}} {
		input, err := protocol.DecodeToolInput(test.name, []byte(test.raw))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, protocol.NewToolUse(string(test.name), input))
	}
	return result
}
func TestASTRuntimeToolGateSourceTraitsAndActualReads(t *testing.T) {
	ast, log := nativeASTConfig(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, resourceProvider{astModelCalls(t)})
	config.Mode = ModeReadonly
	bare, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range protocol.ASTToolSchemas() {
		if _, exists := bare.gate.catalog.Lookup(schema.Name); exists {
			t.Fatal("default installed AST")
		}
	}
	config.ASTConfig = &ast
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../testdata/python-ast-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Tools []struct {
			Schema       protocol.ToolSchema
			Readonly     bool
			ParallelSafe bool `json:"parallel_safe"`
			Risk         ToolRisk
			Capabilities []Capability
		}
	}
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	for _, tool := range source.Tools {
		definition, ok := session.gate.catalog.Lookup(tool.Schema.Name)
		if !ok || definition.risk != tool.Risk || definition.readonly != tool.Readonly || definition.parallelSafe != tool.ParallelSafe || len(definition.capabilities) != 1 || definition.capabilities[0] != tool.Capabilities[0] {
			t.Fatal("traits differ from Source", definition)
		}
	}
	if _, err = session.Run(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	for _, schema := range protocol.ASTToolSchemas() {
		output := diagnosticResult(t, session, string(schema.Name))
		var result struct {
			Status string
			Data   struct {
				Cwd   string
				Files []struct{ Path, Text string }
			}
		}
		if err = json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatal(output, err)
		}
		if result.Status != "applied" || result.Data.Cwd != session.files.Root() || len(result.Data.Files) != 1 || result.Data.Files[0].Path != filepath.Join(session.files.Root(), "code.py") || result.Data.Files[0].Text != "class One: pass\n" {
			t.Fatal(output)
		}
		if strings.Contains(output, "mini-loop-ast-") {
			t.Fatal("private snapshot path leaked")
		}
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 8 {
		t.Fatal("missing actual probes/operations", len(lines))
	}
	for _, line := range lines {
		var call struct {
			Args []string
			Cwd  string
		}
		if err = json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		if call.Cwd != session.files.Root() {
			t.Fatal(call)
		}
		for _, arg := range call.Args {
			if strings.Contains(arg, "mini-loop-ast-") {
				if _, err = os.Stat(arg); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("tool left source snapshot", arg, err)
				}
			}
		}
	}
}
func TestASTManagerInheritanceAndAuthorityBinding(t *testing.T) {
	ast, _ := nativeASTConfig(t)
	config := managerTestConfig(t.TempDir(), resourceProvider{})
	config.Services.ASTConfig = &ast
	manager := makeManager(t, config)
	ast = astcontext.Config{}
	managed := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	// Manager copied the operator config; reassignment above cannot change future sessions.
	definition, ok := managed.core.gate.catalog.Lookup(protocol.ToolFileOutline)
	if !ok {
		t.Fatal("manager omitted AST")
	}
	handler := definition.handler.(*runtimeHandler)
	input := protocol.FileOutlineToolInput(protocol.FileOutlineInput{Paths: []string{"code.py"}})
	for _, foreign := range []ToolAuthority{{SessionID: "foreign", OwnerID: handler.binding.OwnerID, Workspace: handler.binding.Workspace}, {SessionID: handler.binding.SessionID, OwnerID: "foreign", Workspace: handler.binding.Workspace}, {SessionID: handler.binding.SessionID, OwnerID: handler.binding.OwnerID, Workspace: t.TempDir()}} {
		if _, err := handler.ExecuteTool(context.Background(), foreign, input); err == nil {
			t.Fatal("foreign authority reached AST")
		}
	}
	next := t.TempDir()
	if err := os.WriteFile(filepath.Join(next, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Rebinding uses the same workspace transition seam as the existing file tools.
	handler.workspaceBashFactory = hostBashFactory{}
	handler.mu.Lock()
	err := handler.enterWorkspace(context.Background(), next)
	handler.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	output, err := handler.ExecuteTool(context.Background(), handler.binding, input)
	if err != nil || !strings.Contains(output, managed.core.files.Root()) {
		t.Fatal(output, err)
	}
}
func TestASTTaskChildBindsSelectedReadTools(t *testing.T) {
	ast, log := nativeASTConfig(t)
	parentRounds, childRounds := 0, 0
	model := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		child := request.System != nil && strings.Contains(*request.System, " subagent in ")
		if child {
			childRounds++
			if childRounds == 1 {
				count := 0
				for _, schema := range request.Tools {
					if protocol.IsASTTool(schema.Name) {
						count++
					}
				}
				if count != 4 {
					t.Fatal("read capabilities did not inherit four AST tools", count)
				}
				return fakeReply([]protocol.Block{protocol.NewToolUse("child-ast", protocol.FileOutlineToolInput(protocol.FileOutlineInput{Paths: []string{"code.py"}}))}, protocol.StopToolUse), nil
			}
			observed := false
			for _, message := range request.Messages {
				blocks, _ := message.Content.Blocks()
				for _, block := range blocks {
					if result, ok := block.ToolResult(); ok && result.ToolUseID == "child-ast" {
						var payload struct{ Status string }
						if err := json.Unmarshal([]byte(result.Content), &payload); err != nil || payload.Status != "applied" {
							t.Fatal("child AST read failed", result.Content, err)
						}
						observed = true
					}
				}
			}
			if !observed {
				t.Fatal("child model did not receive AST result")
			}
			return stateFinal(), nil
		}
		parentRounds++
		if parentRounds == 1 {
			role := protocol.AgentExplore
			return fakeReply([]protocol.Block{protocol.NewToolUse("task", protocol.TaskToolInput(protocol.TaskInput{Prompt: "inspect", AgentType: &role}))}, protocol.StopToolUse), nil
		}
		return stateFinal(), nil
	})
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, model)
	config.Mode = ModeAuto
	config.ASTConfig = &ast
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "delegate"); err != nil {
		t.Fatal(err)
	}
	if childRounds < 2 {
		t.Fatal("child did not complete native AST turn", childRounds)
	}
	data, err := os.ReadFile(log)
	if err != nil || len(strings.Split(strings.TrimSpace(string(data)), "\n")) != 2 {
		t.Fatal("child did not probe and invoke the real CLI", string(data), err)
	}
}

func TestASTGuardDenialAndInvalidConfigDoNotExecute(t *testing.T) {
	ast, log := nativeASTConfig(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, resourceProvider{astModelCalls(t)})
	config.ASTConfig = &ast
	config.Mode = ModeReadonly
	config.Hooks.Guards = []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
		return "denied semantic read", true, nil
	})}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("guard denial reached AST executable", err)
	}
	for _, schema := range protocol.ASTToolSchemas() {
		if !strings.Contains(diagnosticResult(t, session, string(schema.Name)), "denied semantic read") {
			t.Fatal("guard denial omitted")
		}
	}
	zero := astcontext.Config{}
	config.ASTConfig = &zero
	if _, err = NewRuntimeSession(config); err == nil {
		t.Fatal("zero AST config admitted")
	}
	managerConfig := managerTestConfig(t.TempDir(), resourceProvider{})
	managerConfig.Services.ASTConfig = &zero
	if _, err = NewSessionManager(managerConfig); err == nil {
		t.Fatal("manager admitted zero AST config")
	}
}
