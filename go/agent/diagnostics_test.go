package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/diagnostics"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type diagnosticProviderFunc struct {
	scope string
	run   func(context.Context, diagnostics.Request) ([]diagnostics.Diagnostic, error)
}

func (provider diagnosticProviderFunc) Scope() string { return provider.scope }
func (provider diagnosticProviderFunc) Diagnose(ctx context.Context, request diagnostics.Request) ([]diagnostics.Diagnostic, error) {
	return provider.run(ctx, request)
}

func diagnosticCall(id string, path *string) protocol.Block {
	return protocol.NewToolUse(id, protocol.DiagnosticsToolInput(protocol.DiagnosticsInput{Path: path}))
}

func diagnosticResult(t *testing.T, session *Session, id string) string {
	t.Helper()
	for _, message := range session.Messages() {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if result, ok := block.ToolResult(); ok && result.ToolUseID == id {
				return result.Content
			}
		}
	}
	t.Fatal("missing diagnostic result", id)
	return ""
}

func TestDiagnosticsExplicitRuntimeAndManagerComposition(t *testing.T) {
	var source struct {
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Risk         ToolRisk
	}
	data, err := os.ReadFile("../testdata/python-diagnostics.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var observed diagnostics.Request
	provider := diagnosticProviderFunc{"fixture types only", func(_ context.Context, request diagnostics.Request) ([]diagnostics.Diagnostic, error) {
		observed = request
		return []diagnostics.Diagnostic{{Path: "module.py", Line: 3, Severity: "error", Message: "wrong type"}}, nil
	}}
	config := runtimeConfig(root, resourceProvider{[]protocol.Block{diagnosticCall("diag", nil)}})
	config.Mode = ModeReadonly
	bare, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bare.gate.catalog.Lookup(protocol.ToolDiagnostics); ok {
		t.Fatal("nil provider installed diagnostics")
	}
	config.DiagnosticsProvider = provider
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := session.gate.catalog.Lookup(protocol.ToolDiagnostics)
	if !ok || definition.readonly != source.Readonly || definition.parallelSafe != source.ParallelSafe || definition.risk != source.Risk {
		t.Fatal("traits differ from Source")
	}
	if _, err := session.Run(context.Background(), "check"); err != nil {
		t.Fatal(err)
	}
	if got := diagnosticResult(t, session, "diag"); got != "[scope: fixture types only]\nmodule.py:3 [error] wrong type" {
		t.Fatal(got)
	}
	if observed.Root() != session.files.Root() {
		t.Fatal(observed.Root())
	}
	if _, present := observed.Target(); present {
		t.Fatal("unexpected tree target")
	}
	mcfg := managerTestConfig(t.TempDir(), resourceProvider{[]protocol.Block{diagnosticCall("diag", nil)}})
	mcfg.Services.DiagnosticsProvider = provider
	manager := makeManager(t, mcfg)
	managed := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	if _, err := managed.Run(context.Background(), "check"); err != nil {
		t.Fatal(err)
	}
	if observed.Root() != managed.core.files.Root() || diagnosticResult(t, managed.core, "diag") == "" {
		t.Fatal("manager provider did not bind its own root")
	}
}

func TestDiagnosticsParallelProvidersAndCancellation(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	provider := diagnosticProviderFunc{"parallel fixture", func(ctx context.Context, _ diagnostics.Request) ([]diagnostics.Diagnostic, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	config := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{diagnosticCall("one", nil), diagnosticCall("two", nil)}})
	config.DiagnosticsProvider = provider
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := session.Run(ctx, "check two"); done <- err }()
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("read providers did not overlap")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if output := diagnosticResult(t, session, id); output != "[scope: parallel fixture]\nNo diagnostics." {
			t.Fatal(output)
		}
	}
	blocked := diagnosticProviderFunc{"blocked fixture", func(ctx context.Context, _ diagnostics.Request) ([]diagnostics.Diagnostic, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	config = runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{diagnosticCall("blocked", nil)}})
	config.DiagnosticsProvider = blocked
	session, err = NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if _, err := session.Run(deadline, "check"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal("cancelled provider left a tool hole", err)
	}
}

func TestDiagnosticsProviderFailuresStayInCommonToolPipeline(t *testing.T) {
	for _, failure := range []string{"error", "panic"} {
		t.Run(failure, func(t *testing.T) {
			provider := diagnosticProviderFunc{"fault fixture", func(context.Context, diagnostics.Request) ([]diagnostics.Diagnostic, error) {
				if failure == "panic" {
					panic("private panic canary")
				}
				return nil, errors.New("diagnostic provider failed")
			}}
			config := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{diagnosticCall("fault", nil)}})
			config.DiagnosticsProvider = provider
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			_, runErr := session.Run(context.Background(), "check")
			if failure == "error" && runErr != nil {
				t.Fatal(runErr)
			}
			if failure == "panic" && (runErr == nil || strings.Contains(runErr.Error(), "private panic canary")) {
				t.Fatal("parallel panic lost its type-only run fault", runErr)
			}
			output := diagnosticResult(t, session, "fault")
			if strings.Contains(output, "No diagnostics.") || strings.Contains(output, "private panic canary") || failure == "error" && !strings.Contains(output, "Error") {
				t.Fatal("provider fault became a clean result or leaked panic payload", output)
			}
			if err := protocol.ValidateTranscript(session.Messages()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiagnosticsRebindsExecutionWorkspaceAndRefusesForeignAuthority(t *testing.T) {
	var observed diagnostics.Request
	provider := diagnosticProviderFunc{"binding fixture", func(_ context.Context, request diagnostics.Request) ([]diagnostics.Diagnostic, error) {
		observed = request
		return nil, nil
	}}
	config := runtimeConfig(t.TempDir(), resourceProvider{})
	config.DiagnosticsProvider = provider
	config.WorkspaceBashFactory = hostBashFactory{}
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	definition, _ := session.gate.catalog.Lookup(protocol.ToolDiagnostics)
	handler := definition.handler.(*runtimeHandler)
	newRoot := t.TempDir()
	handler.mu.Lock()
	err = handler.enterWorkspace(context.Background(), newRoot)
	handler.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	authority := handler.binding
	path := "missing.py"
	input := protocol.DiagnosticsToolInput(protocol.DiagnosticsInput{Path: &path})
	if _, err := handler.ExecuteTool(context.Background(), authority, input); err != nil {
		t.Fatal(err)
	}
	target, present := observed.Target()
	if observed.Root() != session.files.Root() || !present || target != filepath.Join(session.files.Root(), path) {
		t.Fatal("stale workspace was diagnosed", observed.Root(), target)
	}
	for _, foreign := range []ToolAuthority{
		{SessionID: "foreign", OwnerID: authority.OwnerID, Workspace: authority.Workspace},
		{SessionID: authority.SessionID, OwnerID: "foreign", Workspace: authority.Workspace},
		{SessionID: authority.SessionID, OwnerID: authority.OwnerID, Workspace: t.TempDir()},
	} {
		if _, err := handler.ExecuteTool(context.Background(), foreign, input); err == nil {
			t.Fatal("foreign authority reached provider")
		}
	}
}

type diagnosticsTaskPolicy struct{}

func (diagnosticsTaskPolicy) Select(_ AgentRole, parent *ToolCatalog) (*ToolCatalog, error) {
	definition, ok := parent.Lookup(protocol.ToolDiagnostics)
	if !ok {
		return nil, errors.New("missing diagnostics")
	}
	return NewToolCatalog(definition)
}

func TestDiagnosticsTaskChildSelectedProviderBindsFreshRuntime(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "default exclusion", true: "explicit selection"}[explicit], func(t *testing.T) {
			calls := 0
			var root string
			provider := diagnosticProviderFunc{"child fixture", func(_ context.Context, request diagnostics.Request) ([]diagnostics.Diagnostic, error) {
				calls++
				root = request.Root()
				return nil, nil
			}}
			parentRounds, childRounds := 0, 0
			model := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				child := request.System != nil && strings.Contains(*request.System, " subagent in ")
				if child {
					childRounds++
					if childRounds == 1 {
						for _, schema := range request.Tools {
							if schema.Name == protocol.ToolDiagnostics {
								return fakeReply([]protocol.Block{diagnosticCall("child", nil)}, protocol.StopToolUse), nil
							}
						}
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
			config := runtimeConfig(t.TempDir(), model)
			config.Mode = ModeAuto
			config.DiagnosticsProvider = provider
			if explicit {
				config.RoleToolPolicy = diagnosticsTaskPolicy{}
			}
			session, err := NewRuntimeSession(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.Run(context.Background(), "delegate"); err != nil {
				t.Fatal(err)
			}
			if childRounds == 0 {
				t.Fatal("no actual child model invocation")
			}
			if explicit && (calls != 1 || root != session.files.Root()) || !explicit && calls != 0 {
				t.Fatal(calls, root)
			}
		})
	}
}
