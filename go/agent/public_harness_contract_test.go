package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sandbox"
	secretpkg "github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/spill"
)

type publicHarnessField string

var publicHarnessFields = []publicHarnessField{
	"tools", "transport", "hooks", "skills", "system_builder", "compactor", "recovery", "stuck_detector",
	"cache_policy", "secrets", "sandbox", "spill", "subagents", "token_efficiency", "role_tool_policy", "injectors",
}

type publicHarnessValueRow struct {
	Field                 publicHarnessField `json:"field"`
	OmittedInherits       bool               `json:"omitted_inherits"`
	ExplicitNoneInherits  bool               `json:"explicit_none_inherits"`
	ExplicitValueWins     bool               `json:"explicit_value_wins"`
	ExplicitFalseWins     bool               `json:"explicit_false_wins"`
	DeriveReplaces        bool               `json:"derive_replaces"`
	DeriveNoneClears      bool               `json:"derive_none_clears"`
	UntouchedFieldsShared bool               `json:"untouched_fields_shared"`
	OriginalUnchanged     bool               `json:"original_unchanged"`
}

type publicHarnessChildField struct {
	SameIdentity     bool                `json:"same_identity"`
	SelectedNames    []protocol.ToolName `json:"selected_names"`
	ExpectedNames    []protocol.ToolName `json:"expected_names"`
	EntriesInherited bool                `json:"entries_inherited"`
}

type publicHarnessContract struct {
	Fields       []publicHarnessField    `json:"fields"`
	ValueRows    []publicHarnessValueRow `json:"value_rows"`
	Construction []struct {
		Name     string                      `json:"name"`
		Bindings map[publicHarnessField]bool `json:"bindings"`
	} `json:"construction"`
	ExplicitEmptyInjectorsReplace bool `json:"explicit_empty_injectors_replace"`
	Runtime                       struct {
		ParentOutput                  string                                         `json:"parent_output"`
		ChildOutput                   string                                         `json:"child_output"`
		ParentModelCalls              int                                            `json:"parent_model_calls"`
		ParentDepth                   int                                            `json:"parent_depth"`
		ChildDepth                    int                                            `json:"child_depth"`
		ChildSystem                   string                                         `json:"child_system"`
		ChildDynamicSystem            bool                                           `json:"child_dynamic_system"`
		ParentHistoryUnchangedByChild bool                                           `json:"parent_history_unchanged_by_child"`
		InjectorParentSeen            bool                                           `json:"injector_parent_seen"`
		InjectorChildSeen             bool                                           `json:"injector_child_seen"`
		ChildFields                   map[publicHarnessField]publicHarnessChildField `json:"child_fields"`
	} `json:"runtime"`
}

func readPublicHarnessContract(t *testing.T) publicHarnessContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-public-harness.json")
	if err != nil {
		t.Fatal(err)
	}
	var source publicHarnessContract
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(source.Fields, publicHarnessFields) || len(source.ValueRows) != 16 || len(source.Construction) != 4 {
		t.Fatal("Source sixteen-field Harness coverage changed")
	}
	for i, row := range source.ValueRows {
		if row.Field != publicHarnessFields[i] || !row.OmittedInherits || !row.ExplicitNoneInherits || !row.ExplicitValueWins || !row.ExplicitFalseWins || !row.DeriveReplaces || !row.DeriveNoneClears || !row.UntouchedFieldsShared || !row.OriginalUnchanged {
			t.Fatalf("actual Source value contract changed: %+v", row)
		}
	}
	for _, construction := range source.Construction {
		assertPublicHarnessFields(t, construction.Bindings)
	}
	if !source.ExplicitEmptyInjectorsReplace || len(source.Runtime.ChildFields) != 16 {
		t.Fatal("Source explicit empty or actual child coverage absent")
	}
	for _, field := range publicHarnessFields {
		observed, present := source.Runtime.ChildFields[field]
		if !present {
			t.Fatalf("Source child field %s missing", field)
		}
		switch field {
		case "tools":
			if observed.SameIdentity || !slices.Equal(observed.SelectedNames, observed.ExpectedNames) || !slices.Equal(observed.SelectedNames, []protocol.ToolName{protocol.ToolBash}) {
				t.Fatal("Source child did not derive the selected nonempty catalogue")
			}
		case "injectors":
			if observed.SameIdentity || !observed.EntriesInherited {
				t.Fatal("Source child injector copy changed")
			}
		default:
			if !observed.SameIdentity {
				t.Fatalf("Source child field %s changed identity", field)
			}
		}
	}
	return source
}

func assertPublicHarnessFields(t *testing.T, actual map[publicHarnessField]bool) {
	t.Helper()
	if len(actual) != len(publicHarnessFields) {
		t.Fatalf("got %d field observations", len(actual))
	}
	for _, name := range publicHarnessFields {
		t.Run(string(name), func(t *testing.T) {
			if !actual[name] {
				t.Fatal("typed native seam was not retained")
			}
		})
	}
}

type publicHarnessProvider struct{ requests []protocol.ModelRequest }

func (p *publicHarnessProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	p.requests = append(p.requests, request)
	return protocol.ModelReply{ID: "harness-reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant,
		Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("harness-ok")}, StopReason: protocol.StopEndTurn}, nil
}

type publicHarnessBefore struct{ calls int }

func (h *publicHarnessBefore) BeforeTool(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
	h.calls++
	return BeforeDecision{}, nil
}

type publicHarnessBuilder struct {
	label string
	calls int
}

func (b *publicHarnessBuilder) BuildSystem(SystemContext) (string, error) {
	b.calls++
	return "public-harness " + b.label, nil
}

type publicHarnessInjector struct{ depths []int }

func (*publicHarnessInjector) Name() string { return "public-harness" }
func (i *publicHarnessInjector) Inject(_ context.Context, view TurnContext) ([]protocol.Message, error) {
	i.depths = append(i.depths, view.Depth)
	return nil, nil
}

type publicHarnessSandbox struct {
	backend sandbox.NullSandbox
	calls   int
}

func (s *publicHarnessSandbox) ForWorkspace(string) (shell.Sandbox, error) { return s, nil }
func (s *publicHarnessSandbox) Argv(command string) ([]string, error) {
	s.calls++
	return s.backend.Argv(command)
}

type publicHarnessNative struct {
	services        ManagerServices
	executor        *shell.Executor
	catalogExecutor *shell.Executor
	provider        *publicHarnessProvider
	before          *publicHarnessBefore
	builder         *publicHarnessBuilder
	injector        *publicHarnessInjector
	sandbox         *publicHarnessSandbox
	spillRoot       string
}

func newPublicHarnessNative(t *testing.T, workspace, label string) publicHarnessNative {
	t.Helper()
	provider, before := &publicHarnessProvider{}, &publicHarnessBefore{}
	builder, injector := &publicHarnessBuilder{label: label}, &publicHarnessInjector{}
	spillRoot := filepath.Join(t.TempDir(), "spill-"+label)
	store, err := spill.NewLocalStore(spillRoot)
	if err != nil {
		t.Fatal(err)
	}
	spillRoot, err = filepath.EvalSymlinks(spillRoot)
	if err != nil {
		t.Fatal(err)
	}
	backend := &publicHarnessSandbox{}
	secrets := secretpkg.New(secretpkg.Config{})
	executor, err := shell.New(shell.Config{Workspace: workspace, Timeout: time.Second, CaptureLimit: 100_000, Secrets: secrets, Sandbox: backend})
	if err != nil {
		t.Fatal(err)
	}
	// Source built-ins resolve ctx.agent.toolset at invocation. The native
	// catalogue explicitly captures an executor at construction, so compose
	// its selected store before freezing that catalogue. Keep config.Bash
	// store-free to independently prove RuntimeConfig.Spill binds the parent.
	catalogExecutor, err := executor.WithSpill(store)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewBashToolCatalog(catalogExecutor)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := NewDefaultRecovery(RecoveryConfig{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := efficiency.NewRuntime(efficiency.DefaultRuntimeConfig())
	if err != nil {
		t.Fatal(err)
	}
	cache, stuck := NewDefaultCachePolicy(), NewDefaultStuckDetector()
	services := ManagerServices{Tools: catalog, Provider: provider, Hooks: GateHooks{Before: []BeforeHook{before}},
		Skills: skills.EmptyCatalog(), SystemBuilder: builder, Compactor: NewDefaultCompactor(), Recovery: recovery,
		StuckDetector: &stuck, CachePolicy: &cache, Secrets: secrets, Sandbox: backend, Spill: store,
		Subagents: &InProcessSubagents{}, TokenEfficiency: runtime, RoleToolPolicy: DefaultRoleToolPolicy(),
		Injectors: []MessageInjector{injector}}
	return publicHarnessNative{services: services, executor: executor, catalogExecutor: catalogExecutor, provider: provider, before: before, builder: builder, injector: injector, sandbox: backend, spillRoot: spillRoot}
}

func (p publicHarnessNative) config(workspace string) RuntimeConfig {
	s := p.services
	return RuntimeConfig{ID: "public-harness", Owner: "owner", Workspace: workspace, Bash: p.executor, Mode: ModeInteractive, MaxRounds: 2,
		Tools: s.Tools, Provider: s.Provider, Hooks: s.Hooks, Skills: s.Skills, SystemBuilder: s.SystemBuilder, Compactor: s.Compactor,
		Recovery: s.Recovery, StuckDetector: s.StuckDetector, CachePolicy: s.CachePolicy, Secrets: s.Secrets, Spill: s.Spill,
		Subagents: s.Subagents, TokenEfficiency: s.TokenEfficiency, RoleToolPolicy: s.RoleToolPolicy, Injectors: s.Injectors, Label: "main"}
}

// These are explicit typed configuration assignments. Source resolve(None)
// runs before this boundary; Go deliberately has no string-keyed override API.
func publicHarnessBoundFields(t *testing.T, session *Session, p publicHarnessNative) map[publicHarnessField]bool {
	t.Helper()
	s := p.services
	executor, native := session.bash.(*shell.Executor)
	// This bounded operator-side backend probe proves both adapters survive
	// construction and fresh-child inheritance. It is not a model permission
	// or arbitrary shell/backend acceptance claim. Only 60 KB are produced.
	before := p.sandbox.calls
	preserved, err := session.bash.ExecuteBash(context.Background(), protocol.BashInput{Command: "printf '%060000d' 0"})
	if err != nil {
		t.Fatal(err)
	}
	return map[publicHarnessField]bool{
		"tools":            slices.Equal(session.gate.catalog.Names(), s.Tools.Names()),
		"transport":        session.provider == s.Provider,
		"hooks":            len(session.gate.before) == 1 && session.gate.before[0] == p.before,
		"skills":           session.skills == s.Skills,
		"system_builder":   session.systemBuilder == s.SystemBuilder,
		"compactor":        session.compactor == s.Compactor,
		"recovery":         session.recovery == s.Recovery,
		"stuck_detector":   session.stuckDetector == s.StuckDetector,
		"cache_policy":     session.cachePolicy == s.CachePolicy,
		"secrets":          session.secrets == s.Secrets && session.gate.secrets == s.Secrets,
		"sandbox":          native && executor.SandboxConfigured() && p.sandbox.calls == before+1,
		"spill":            s.Spill != nil && strings.Contains(preserved, "[full output preserved: "+p.spillRoot+string(filepath.Separator)),
		"subagents":        session.subagents == s.Subagents,
		"token_efficiency": session.tokenEfficiency == s.TokenEfficiency,
		"role_tool_policy": session.rolePolicy == s.RoleToolPolicy,
		"injectors":        len(session.injectors) == 1 && session.injectors[0] == p.injector,
	}
}

func TestPublicHarnessTypedCompositionMatchesActualSource(t *testing.T) {
	source := readPublicHarnessContract(t)
	workspace := t.TempDir()
	for _, label := range []string{"a", "b"} {
		t.Run(label, func(t *testing.T) {
			p := newPublicHarnessNative(t, workspace, label)
			session, err := NewRuntimeSession(p.config(workspace))
			if err != nil {
				t.Fatal(err)
			}
			assertPublicHarnessFields(t, publicHarnessBoundFields(t, session, p))
			if output, err := session.Run(context.Background(), "parent"); err != nil || output != source.Runtime.ParentOutput {
				t.Fatalf("parent run: %q %v", output, err)
			}
			if _, err := session.Delegate(context.Background(), "child", RoleGeneralPurpose); err != nil {
				t.Fatal(err)
			}
			if len(p.provider.requests) != source.Runtime.ParentModelCalls || !slices.Equal(p.injector.depths, []int{source.Runtime.ParentDepth, source.Runtime.ChildDepth}) {
				t.Fatal("provider or injector did not reach the actual default child")
			}
			if len(session.Messages()) != 2 || !source.Runtime.ParentHistoryUnchangedByChild {
				t.Fatal("child changed parent history")
			}
			// Inspect the same production child constructor used by InProcessSubagents.
			run, err := DefaultRunContext()
			if err != nil {
				t.Fatal(err)
			}
			child, _, err := newSubagent(context.Background(), SubagentRequest{Parent: session.subagentParent(run), Prompt: "inspect", Role: RoleGeneralPurpose, RunContext: run})
			if err != nil {
				t.Fatal(err)
			}
			fields := publicHarnessBoundFields(t, child, p)
			fields["system_builder"] = false // Child fixed-system override is intentional.
			fixed, fixedOK := child.systemBuilder.(FixedSystem)
			fields["system_builder"] = fixedOK && strings.ReplaceAll(string(fixed), child.executionRoot(), "$WORKSPACE") == source.Runtime.ChildSystem
			// The child keeps the selected catalogue's explicitly bound executor.
			// Source keeps Store identity, not Toolset identity; parent runtime
			// and catalogue executors may be different immutable copies.
			fields["spill"] = fields["spill"] && child.bash == p.catalogExecutor
			assertPublicHarnessFields(t, fields)
			if !slices.Equal(child.gate.catalog.Names(), source.Runtime.ChildFields["tools"].SelectedNames) || child.depth != source.Runtime.ChildDepth || child.owner != session.owner {
				t.Fatal("capability selection or lineage changed")
			}
			child.injectors[0] = &publicHarnessInjector{}
			if session.injectors[0] != p.injector {
				t.Fatal("child aliases parent injector container")
			}
		})
	}
}

func TestPublicHarnessManagerBridgeRetainsEveryTypedSeam(t *testing.T) {
	readPublicHarnessContract(t)
	workspace := t.TempDir()
	p := newPublicHarnessNative(t, workspace, "manager")
	config := managerTestConfig(filepath.Join(t.TempDir(), "fleet"), p.provider)
	config.Services = p.services
	manager := makeManager(t, config)
	// Run the real manager composition path before constructing the runtime.
	runtime := manager.baseManagedRuntimeConfig("public-harness", "owner", workspace, ModeInteractive, DefaultModel, nil, p.executor)
	session, err := NewRuntimeSession(runtime)
	if err != nil {
		t.Fatal(err)
	}
	assertPublicHarnessFields(t, publicHarnessBoundFields(t, session, p))
	factory, ok := manager.config.Services.BashFactory.(hostBashFactory)
	if !ok || factory.sandbox != p.services.Sandbox {
		t.Fatal("manager default factory lost the explicit sandbox")
	}
	bound, err := factory.BashFor(context.Background(), SessionBinding{ID: "bound", Owner: "owner", Workspace: workspace, Mode: ModeInteractive})
	if err != nil {
		t.Fatal(err)
	}
	if !bound.(*shell.Executor).SandboxConfigured() {
		t.Fatal("actual factory did not bind sandbox")
	}
	fixed := "explicit system"
	overridden := manager.baseManagedRuntimeConfig("public-harness", "owner", workspace, ModeInteractive, DefaultModel, &fixed, p.executor)
	if actual, err := overridden.SystemBuilder.BuildSystem(SystemContext{}); err != nil || actual != fixed {
		t.Fatal("explicit fixed system did not override manager builder")
	}
	if manager.config.Services.SystemBuilder != p.builder {
		t.Fatal("per-session system changed manager template")
	}
}

func TestPublicHarnessDefaultCatalogueBindsSpillIntoFreshChild(t *testing.T) {
	source := readPublicHarnessContract(t)
	workspace := t.TempDir()
	p := newPublicHarnessNative(t, workspace, "default-catalogue")
	config := p.config(workspace)
	config.Tools = nil
	parent, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	run, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	child, childRun, err := newSubagent(context.Background(), SubagentRequest{Parent: parent.subagentParent(run), Role: RoleGeneralPurpose, RunContext: run})
	if err != nil {
		t.Fatal(err)
	}
	// With no custom catalogue, production builds native bash from the
	// already scoped runtime executor. Verify this separate default path;
	// the explicit bound-catalogue recipe above must not stand in for it.
	if parent.bash == p.executor || child.bash != parent.bash {
		t.Fatal("default catalogue lost its runtime-bound executor")
	}
	for _, session := range []*Session{parent, child} {
		proof := publicHarnessBoundFields(t, session, p)
		if !proof["sandbox"] || !proof["spill"] || !source.Runtime.ChildFields["spill"].SameIdentity {
			t.Fatal("default catalogue failed actual scoped sandbox/store preservation")
		}
	}
	output, err := child.RunWithContext(context.Background(), "child", childRun)
	if err != nil || output != source.Runtime.ChildOutput {
		t.Fatalf("default child run: %q %v", output, err)
	}
	plain, err := p.executor.ExecuteBash(context.Background(), protocol.BashInput{Command: "printf '%060000d' 0"})
	if err != nil || strings.Contains(plain, "full output preserved:") {
		t.Fatal("runtime binding mutated the store-free caller executor", err)
	}
}

func TestPublicHarnessNilAndExplicitEmptyTypedDispositions(t *testing.T) {
	source := readPublicHarnessContract(t)
	config := runtimeConfig(t.TempDir(), &publicHarnessProvider{})
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	// Go requires identity/provider/executor at construction. Optional nil
	// dependencies select native defaults or absence as documented; these are
	// not presented as an invocation of Source Harness.resolve(None).
	if session.skills == nil || session.systemBuilder == nil || session.compactor == nil || session.recovery == nil || session.cachePolicy == nil || session.stuckDetector == nil || session.subagents == nil || session.rolePolicy == nil {
		t.Fatal("optional typed defaults absent")
	}
	if session.secrets != nil || session.tokenEfficiency != nil || session.injectors != nil {
		t.Fatal("absent direct runtime services widened")
	}
	config.Injectors = []MessageInjector{}
	empty, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if empty.injectors == nil || len(empty.injectors) != 0 || !source.ExplicitEmptyInjectorsReplace {
		t.Fatal("explicit empty list collapsed into nil")
	}
	run, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := newSubagent(context.Background(), SubagentRequest{Parent: empty.subagentParent(run), Role: RoleGeneralPurpose, RunContext: run})
	if err != nil {
		t.Fatal(err)
	}
	if child.injectors == nil || len(child.injectors) != 0 {
		t.Fatal("child lost explicit empty injection selection")
	}
	if reflect.ValueOf(config.Injectors).IsNil() {
		t.Fatal("construction mutated caller list")
	}
}
