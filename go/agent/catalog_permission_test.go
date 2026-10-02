package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBashCatalogMatchesPythonDefaultMetadata(t *testing.T) {
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
	if len(metadata) != 10 || metadata[0].Name != protocol.ToolBash {
		t.Fatalf("Python default inventory changed: %+v", metadata)
	}
	catalog, err := NewBashToolCatalog(echoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	definition, exists := catalog.Lookup(protocol.ToolBash)
	if !exists || definition.Risk() != metadata[0].Risk || definition.Readonly() != metadata[0].Readonly || definition.ParallelSafe() != metadata[0].ParallelSafe || !reflect.DeepEqual(definition.Capabilities(), metadata[0].Capabilities) {
		t.Fatalf("Go bash metadata drifted from Python: %+v", metadata[0])
	}
	names := catalog.Names()
	names[0] = "changed"
	if catalog.Names()[0] != protocol.ToolBash {
		t.Fatal("caller changed catalogue names")
	}
}

func TestCatalogCopiesCapabilitiesAndRejectsDuplicates(t *testing.T) {
	capabilities := []Capability{CapabilityProcessExec}
	definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec, Capabilities: capabilities}, &gateHandler{})
	if err != nil {
		t.Fatal(err)
	}
	capabilities[0] = CapabilityRepoRead
	catalog, err := NewToolCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := catalog.Lookup(protocol.ToolBash)
	if got.Capabilities()[0] != CapabilityProcessExec {
		t.Fatal("catalogue retained caller-owned capability slice")
	}
	if _, err := NewToolCatalog(definition, definition); err == nil {
		t.Fatal("duplicate tool accepted")
	}
	if _, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec, Readonly: true}, &gateHandler{}); err == nil {
		t.Fatal("read-only claim admitted an exec-risk tool")
	}
}

func TestPermissionModesAndImmutableDenyList(t *testing.T) {
	handler := &gateHandler{output: "ran"}
	gate, err := NewToolGate(gateTestCatalog(t, handler), DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode    PermissionMode
		command string
		denied  bool
		rule    string
	}{
		{ModeReadonly, "echo hi", true, "readonly-mode"},
		{ModeInteractive, "echo hi", false, ""},
		{ModeInteractive, "rm file", true, "destructive-shell"},
		{ModeAuto, "rm file", false, "destructive-shell"},
		{ModeAuto, "sudo shutdown now", true, "immutable-deny-list"},
	} {
		outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(tc.mode), gateTestCall(tc.command))
		if err != nil || outcome.Denied != tc.denied {
			t.Fatalf("%s %q outcome=%+v err=%v", tc.mode, tc.command, outcome, err)
		}
		if tc.rule != "" {
			events := outcome.PermissionEvents()
			if len(events) != 1 || events[0].Rule != tc.rule {
				t.Fatalf("permission audit=%+v", events)
			}
		}
	}
	if handler.calls != 2 {
		t.Fatalf("denied calls reached handler: %d", handler.calls)
	}
	if _, err := NewSessionWithGate("s", "owner", FakeProvider{}, gate, "invalid", "", 2); err == nil {
		t.Fatal("invalid permission mode admitted")
	}
}

func TestAutoModeKeepsCustomDenial(t *testing.T) {
	handler := &gateHandler{output: "ran"}
	rule, err := NewPermissionRule("custom-denial", "Blocked by custom policy", RuleDeny,
		func(ToolAuthority, ToolCall, ToolRisk, bool) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewPermissionPolicy([]PermissionRule{rule}, approverFunc(func(context.Context, ApprovalRequest) (bool, error) {
		t.Fatal("an explicit denial requested approval")
		return true, nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(gateTestCatalog(t, handler), policy, GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := gate.Dispatch(context.Background(), gateTestAuthority(ModeAuto), gateTestCall("echo hi"))
	if err != nil || !outcome.Denied || handler.calls != 0 {
		t.Fatalf("auto mode widened custom policy: %+v, %v", outcome, err)
	}
	events := outcome.PermissionEvents()
	if len(events) != 1 || events[0].Decision != PermissionDeny || events[0].Rule != "custom-denial" {
		t.Fatalf("missing denial event: %+v", events)
	}
}

func TestReadonlySessionDeniesBashAndKeepsTranscriptPaired(t *testing.T) {
	executor := &countedBashExecutor{}
	catalog, err := NewBashToolCatalog(executor)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSessionWithGate("s", "owner", FakeProvider{}, gate, ModeReadonly, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 0 {
		t.Fatal("readonly session executed bash")
	}
	messages := session.Messages()
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	blocks, _ := messages[2].Content.Blocks()
	result, _ := blocks[0].ToolResult()
	if result.IsError || result.Content == "" {
		t.Fatalf("readonly denial was not returned: %+v", result)
	}
	assertToolResultTelemetry(t, session, result.ToolUseID, false, true)
}

type fileProbeHandler struct{ calls int }

func (handler *fileProbeHandler) ExecuteTool(_ context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
	if _, ok := input.WriteFile(); !ok {
		return "", os.ErrInvalid
	}
	handler.calls++
	return "wrote", nil
}

func TestWorkspaceBoundaryChecksParentTraversalAndSymlinks(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}
	handler := &fileProbeHandler{}
	definition, err := NewToolDefinition(protocol.ToolWriteFile, ToolTraits{Risk: RiskWrite, Capabilities: []Capability{CapabilityWorkspaceWrite}}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewToolCatalog(definition)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	authority := gateTestAuthority(ModeAuto)
	authority.Workspace = workspace
	for _, path := range []string{"../escape.txt", filepath.Join(outside, "absolute.txt"), "link/through.txt"} {
		call := ToolCall{ID: "u1", Input: protocol.WriteFileToolInput(protocol.WriteFileInput{Path: path, Content: "x"})}
		outcome, err := gate.Dispatch(context.Background(), authority, call)
		if err != nil || !outcome.Denied || outcome.PermissionEvents()[0].Rule != "workspace-boundary" {
			t.Fatalf("path %q escaped: %+v, %v", path, outcome, err)
		}
	}
	call := ToolCall{ID: "u2", Input: protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "inside/new.txt", Content: "x"})}
	outcome, err := gate.Dispatch(context.Background(), authority, call)
	if err != nil || outcome.IsError() || handler.calls != 1 {
		t.Fatalf("safe path denied: %+v, %v", outcome, err)
	}
}
