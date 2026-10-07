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

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

type auditToolObserver struct {
	collect func(context.Context, selfaudit.Scope) selfaudit.Observations
}

func (observer auditToolObserver) ObserveSelfAudit(ctx context.Context, scope selfaudit.Scope) selfaudit.Observations {
	return observer.collect(ctx, scope)
}

func auditToolResult(t *testing.T, session *Session) protocol.ToolResultBlock {
	t.Helper()
	blocks, _ := session.Messages()[2].Content.Blocks()
	result, ok := blocks[0].ToolResult()
	if !ok {
		t.Fatal("missing audit tool result")
	}
	return result
}

func TestSelfAuditToolSourceNoticeAndReadTraits(t *testing.T) {
	var source struct {
		NoManagerOutput string `json:"no_manager_output"`
		Readonly        bool
		Risk            ToolRisk
		ParallelSafe    bool `json:"parallel_safe"`
	}
	data, err := os.ReadFile("../testdata/python-self-audit-tool.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewToolUse("audit", protocol.SelfAuditToolInput())}})
	cfg.SelfAuditTools = true
	cfg.Mode = ModeReadonly
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := session.gate.catalog.Lookup(protocol.ToolSelfAudit)
	if !ok || definition.readonly != source.Readonly || definition.risk != source.Risk || definition.parallelSafe != source.ParallelSafe {
		t.Fatal("source traits differ")
	}
	if _, err := session.Run(context.Background(), "audit"); err != nil {
		t.Fatal(err)
	}
	result := auditToolResult(t, session)
	if result.IsError || result.Content != source.NoManagerOutput {
		t.Fatal(result)
	}

	cfg.SelfAuditTools = false
	plain, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, installed := plain.gate.catalog.Lookup(protocol.ToolSelfAudit); installed {
		t.Fatal("default installation")
	}
	cfg.SelfAuditTools = true
	cfg.ToolSelection = SelectTools(protocol.ToolReadFile)
	selected, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, installed := selected.gate.catalog.Lookup(protocol.ToolSelfAudit); installed {
		t.Fatal("selection did not remove audit")
	}
}

func TestSelfAuditToolManagerScopeAndActiveTurnDoesNotDeadlock(t *testing.T) {
	for _, view := range []SelfAuditView{SelfAuditOwnerView, SelfAuditOperatorView} {
		cfg := managerTestConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewToolUse("audit", protocol.SelfAuditToolInput())}})
		cfg.Services.SelfAuditTools, cfg.Services.SelfAuditView = true, view
		manager := makeManager(t, cfg)
		alice := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
		bob := createManaged(t, manager, CreateSessionRequest{Owner: "bob"})
		alice.core.gate.recordProblem("alice-private")
		bob.core.gate.recordProblem("bob-private")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := alice.Run(ctx, "audit")
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		result := auditToolResult(t, alice.core)
		if result.IsError || !strings.Contains(result.Content, "alice-private") {
			t.Fatal(result)
		}
		if strings.Contains(result.Content, "bob-private") != (view == SelfAuditOperatorView) {
			t.Fatal("report visibility differs", result.Content)
		}
		if strings.Contains(result.Content, "## cron") != (view == SelfAuditOperatorView) {
			t.Fatal("fleet ledger visibility differs", result.Content)
		}
	}
}

func TestSelfAuditToolActualSourceEmptyManagerReport(t *testing.T) {
	var fixture struct {
		Cases []struct{ Name, Report string }
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-self-audit-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	manager := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	cfg := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewToolUse("audit", protocol.SelfAuditToolInput())}})
	cfg.SelfAuditTools, cfg.SelfAuditView, cfg.SelfAuditObserver = true, SelfAuditOperatorView, manager
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "audit"); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		if row.Name == "empty" {
			result := auditToolResult(t, session)
			if result.IsError || result.Content != row.Report {
				t.Fatalf("got %s want %s", result.Content, row.Report)
			}
			return
		}
	}
	t.Fatal("missing empty source fixture")
}

func TestSelfAuditToolBoundScopeAndPrivateFaults(t *testing.T) {
	for _, view := range []SelfAuditView{SelfAuditOwnerView, SelfAuditOperatorView} {
		calls := 0
		binding := selfAuditBinding{owner: "alice", view: view, observer: auditToolObserver{func(ctx context.Context, scope selfaudit.Scope) selfaudit.Observations {
			calls++
			if scope.IncludeGlobal != (view == SelfAuditOperatorView) {
				t.Fatal(scope)
			}
			if view == SelfAuditOwnerView && (scope.Owner == nil || *scope.Owner != "alice") {
				t.Fatal(scope)
			}
			if view == SelfAuditOperatorView && scope.Owner != nil {
				t.Fatal(scope)
			}
			return selfaudit.Observations{}
		}}}
		if _, err := binding.report(context.Background()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := binding.report(ctx); !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatal(err, calls)
		}
	}
	bad := selfAuditBinding{observer: auditToolObserver{func(context.Context, selfaudit.Scope) selfaudit.Observations { panic("private diagnostic") }}}
	if output, err := bad.report(context.Background()); output != "" || err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(output, err)
	}
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.SelfAuditView = 200
	if _, err := NewRuntimeSession(cfg); err == nil {
		t.Fatal("invalid runtime view")
	}
	mgr := managerTestConfig(t.TempDir(), &FakeProvider{})
	mgr.Services.SelfAuditView = 200
	if _, err := NewSessionManager(mgr); err == nil {
		t.Fatal("invalid manager view")
	}
}

func TestSelfAuditSelectedChildRetainsOwnerBinding(t *testing.T) {
	calls := 0
	cfg := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewToolUse("audit", protocol.SelfAuditToolInput())}})
	cfg.SelfAuditTools = true
	cfg.RoleToolPolicy = allRoleTools{}
	cfg.SelfAuditObserver = auditToolObserver{func(ctx context.Context, scope selfaudit.Scope) selfaudit.Observations {
		calls++
		if scope.Owner == nil || *scope.Owner != string(cfg.Owner) || scope.IncludeGlobal {
			t.Fatal("child widened scope")
		}
		return selfaudit.Observations{}
	}}
	session, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Delegate(context.Background(), "audit", RoleExplore); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("child did not execute bound report", calls)
	}
	defaultChild, err := DefaultRoleToolPolicy().Select(RoleExplore, session.gate.catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, installed := defaultChild.Lookup(protocol.ToolSelfAudit); installed {
		t.Fatal("default role gained capability")
	}
}

func TestSelfAuditUsesGuardAndResultMasking(t *testing.T) {
	for _, denied := range []bool{false, true} {
		calls := 0
		cfg := runtimeConfig(t.TempDir(), resourceProvider{[]protocol.Block{protocol.NewToolUse("audit", protocol.SelfAuditToolInput())}})
		cfg.SelfAuditTools = true
		cfg.Secrets = runtimeSecrets()
		cfg.SelfAuditObserver = auditToolObserver{func(context.Context, selfaudit.Scope) selfaudit.Observations {
			calls++
			return selfaudit.Observations{Problems: selfaudit.GlobalLedgers{Skills: &selfaudit.Ledger{Entries: []string{runtimeCanary}}}}
		}}
		cfg.SelfAuditView = SelfAuditOperatorView
		cfg.Hooks.Guards = []GuardHook{guardHookFunc(func(_ context.Context, _ ToolAuthority, call ToolCall) (string, bool, error) {
			if call.Name() != protocol.ToolSelfAudit {
				t.Fatal("guard got different tool")
			}
			return "denied", denied, nil
		})}
		session, err := NewRuntimeSession(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := session.Run(context.Background(), "audit"); err != nil {
			t.Fatal(err)
		}
		result := auditToolResult(t, session)
		if strings.Contains(result.Content, runtimeCanary) || result.IsError {
			t.Fatal(result)
		}
		if denied && result.Content != "denied" {
			t.Fatal(result)
		}
		want := 1
		if denied {
			want = 0
		}
		if calls != want {
			t.Fatal("guard did not precede observation", calls)
		}
	}
}
