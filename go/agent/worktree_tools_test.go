package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/tasks"
	"github.com/luoyjx/mini-loop/go/worktrees"
)

func worktreeSession(t *testing.T, calls []protocol.Block) (*Session, *worktrees.Manager, string) {
	t.Helper()
	repo := worktreeRepo(t)
	service, err := worktrees.New(worktrees.Config{Repository: repo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Create(context.Background(), "one", "", nil); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(root, resourceProvider{calls})
	config.Bash, config.Mode = executor, ModeAuto
	config.WorktreeTools, config.Worktrees = true, service
	s, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	target, err := service.PathFor("one")
	if err != nil {
		t.Fatal(err)
	}
	return s, service, target
}

func enterWorktreeCall(id, name string) protocol.Block {
	return protocol.NewToolUse(id, protocol.EnterWorktreeToolInput(protocol.WorktreeNameInput{Name: name}))
}

func writeWorktreeCall(id, path, content string) protocol.Block {
	return protocol.NewToolUse(id, protocol.WriteFileToolInput(protocol.WriteFileInput{Path: path, Content: content}))
}

type worktreeCustomBash struct{ executor *shell.Executor }

func (b worktreeCustomBash) Workspace() string { return b.executor.Workspace() }
func (b worktreeCustomBash) ExecuteBash(ctx context.Context, input protocol.BashInput) (string, error) {
	return b.executor.ExecuteBash(ctx, input)
}

func TestWorktreeCustomFactorySuccessAndCancelledPreparation(t *testing.T) {
	for _, cancelPreparation := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cancelled"}[cancelPreparation], func(t *testing.T) {
			s, _, target := worktreeSession(t, []protocol.Block{enterWorktreeCall("enter", "one"), protocol.NewBashUse("pwd", "pwd")})
			original, oldCatalog := s.executionRoot(), s.gate.catalog
			old := worktreeCustomBash{s.bash.(*shell.Executor)}
			s.bash = old
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			definition, _ := s.gate.catalog.Lookup(protocol.ToolEnterWorktree)
			handler := definition.handler.(*runtimeHandler)
			handler.workspaceBashFactory = bashFactoryFunc(func(_ context.Context, binding SessionBinding) (BashExecutor, error) {
				if binding.Workspace != target || binding.ID != s.id || binding.Owner != s.owner || binding.Mode != ModeAuto {
					t.Fatal("custom factory scope differs")
				}
				next, err := old.executor.WithWorkspace(binding.Workspace)
				if cancelPreparation {
					cancel()
				}
				return worktreeCustomBash{next}, err
			})
			_, err := s.Run(ctx, "custom factory")
			if cancelPreparation {
				if !errors.Is(err, context.Canceled) || s.executionRoot() != original || s.gate.catalog != oldCatalog || s.bash != old {
					t.Fatal("cancelled preparation published new scope", err)
				}
			} else {
				if err != nil || s.executionRoot() != target || s.workspace != original || old.Workspace() != original {
					t.Fatal("custom factory failed to rebind independently", err)
				}
				blocks, _ := s.Messages()[2].Content.Blocks()
				result, _ := blocks[1].ToolResult()
				if strings.TrimSpace(result.Content) != target {
					t.Fatal("custom executor ran in original directory", result.Content)
				}
			}
		})
	}
}

func TestWorktreeSwitchFailureRetainsEveryExecutionBinding(t *testing.T) {
	for _, failure := range []string{"error", "nil", "wrong-root", "unbound", "unavailable"} {
		t.Run(failure, func(t *testing.T) {
			s, _, target := worktreeSession(t, []protocol.Block{enterWorktreeCall("enter", "one"), writeWorktreeCall("write", "still.txt", "old")})
			original := s.executionRoot()
			definition, _ := s.gate.catalog.Lookup(protocol.ToolEnterWorktree)
			handler := definition.handler.(*runtimeHandler)
			s.bash = echoExecutor{}
			if failure != "unavailable" {
				handler.workspaceBashFactory = bashFactoryFunc(func(_ context.Context, b SessionBinding) (BashExecutor, error) {
					if b.ID != s.id || b.Owner != s.owner || b.Workspace != target {
						t.Fatal("factory authority widened")
					}
					switch failure {
					case "error":
						return nil, errors.New("prepare failed")
					case "nil":
						return nil, nil
					case "unbound":
						return echoExecutor{}, nil
					default:
						return shell.New(shell.Config{Workspace: original})
					}
				})
			}
			catalog, policy, files, questions := s.gate.catalog, s.gate.policy, s.files, s.questions
			if _, err := s.Run(context.Background(), "switch"); err != nil {
				t.Fatal(err)
			}
			if s.executionRoot() != original || s.workspace != original || handler.binding.Workspace != original || s.gate.catalog != catalog || s.gate.policy != policy || s.files != files || s.questions != questions {
				t.Fatal("failed preparation published a partial binding")
			}
			if raw, err := os.ReadFile(filepath.Join(original, "still.txt")); err != nil || string(raw) != "old" {
				t.Fatal("old files lost after failed switch", err)
			}
			if _, err := os.Stat(filepath.Join(target, "still.txt")); !os.IsNotExist(err) {
				t.Fatal("failed switch wrote in target")
			}
			blocks, _ := s.Messages()[2].Content.Blocks()
			result, _ := blocks[0].ToolResult()
			if !strings.HasPrefix(result.Content, "Error:") {
				t.Fatal("prepare failure was hidden", result.Content)
			}
		})
	}
}

func TestWorktreeEntryIsBarrierEvenWithParallelClassifier(t *testing.T) {
	calls := []protocol.Block{writeWorktreeCall("a", "a.txt", "old a"), writeWorktreeCall("b", "b.txt", "old b"), enterWorktreeCall("enter", "one"), writeWorktreeCall("c", "c.txt", "new c")}
	s, _, target := worktreeSession(t, calls)
	oldRoot, oldCatalog := s.executionRoot(), s.gate.catalog
	release := make(chan struct{})
	var mu sync.Mutex
	roots := map[string]string{}
	s.gate.before = []BeforeHook{beforeHookFunc(func(ctx context.Context, a ToolAuthority, c ToolCall) (BeforeDecision, error) {
		if c.ID == "a" {
			select {
			case <-release:
			case <-ctx.Done():
				return BeforeDecision{}, ctx.Err()
			}
		}
		if c.ID == "b" {
			close(release)
		}
		mu.Lock()
		roots[c.ID] = a.Workspace
		mu.Unlock()
		return KeepToolCall(), nil
	})}
	var classifiedEntry bool
	definitions := append([]ToolDefinition(nil), s.gate.catalog.ordered...)
	for i := range definitions {
		if definitions[i].name == protocol.ToolWriteFile || definitions[i].name == protocol.ToolEnterWorktree {
			definitions[i] = definitions[i].WithExecutionClassifier(executionClassifierFunc(func(c ToolCall) (ExecutionMode, error) {
				if c.Name() == protocol.ToolEnterWorktree {
					classifiedEntry = true
				}
				return ExecutionParallel, nil
			}))
		}
	}
	s.gate.catalog, _ = NewToolCatalog(definitions...)
	journal, _ := NewInMemoryActionJournal(20)
	s.gate.journal = journal
	var observed []string
	s.gate.observers = []ResultObserver{observerFunc(func(_ context.Context, a ToolAuthority, c ToolCall, _ ToolOutcome) error {
		row, ok, err := journal.Get(context.Background(), a.ActionID)
		if err != nil || !ok || row.Status != ActionCompleted {
			return errors.New("observer ran before settlement")
		}
		mu.Lock()
		observed = append(observed, c.ID)
		mu.Unlock()
		return nil
	})}
	if _, err := s.Run(context.Background(), "barrier"); err != nil {
		t.Fatal(err)
	}
	if classifiedEntry || roots["a"] != oldRoot || roots["b"] != oldRoot || roots["enter"] != oldRoot || roots["c"] != target || len(observed) != 4 {
		t.Fatal("barrier or authority order violated", roots, observed)
	}
	for name, root := range map[string]string{"a.txt": oldRoot, "b.txt": oldRoot, "c.txt": target} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("write landed in obsolete scope", err)
		}
	}
	pinned, _ := oldCatalog.Lookup(protocol.ToolWriteFile)
	if pinned.handler.(workspaceFileHandler).files.Root() != oldRoot {
		t.Fatal("old immutable catalogue mutated")
	}
	current, _ := s.gate.catalog.Lookup(protocol.ToolWriteFile)
	if current.verifier.(workspaceWriteVerifier).files.Root() != target {
		t.Fatal("write replay verifier remained stale")
	}
}

func TestWorktreeEntryRebindsApprovalAndQuestionWithoutChangingOwner(t *testing.T) {
	calls := []protocol.Block{enterWorktreeCall("enter", "one"), protocol.NewBashUse("bash", "pwd"), protocol.NewToolUse("question", protocol.AskUserToolInput(protocol.AskUserInput{Question: "continue?"}))}
	s, _, target := worktreeSession(t, calls)
	broker, _ := NewApprovalBroker(ApprovalBrokerConfig{})
	defer broker.CancelAll()
	old, err := broker.ForSession(ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: s.executionRoot(), Mode: ModeInteractive}, sessionApprovalSink{s.events})
	if err != nil {
		t.Fatal(err)
	}
	rule, _ := NewPermissionRule("shell-approval", "approve shell", RuleAsk, func(_ ToolAuthority, c ToolCall, _ ToolRisk, _ bool) bool { return c.Name() == protocol.ToolBash })
	s.gate.policy, _ = NewPermissionPolicy([]PermissionRule{rule}, old, []string{"sudo"})
	s.mode = ModeInteractive
	definition, _ := s.gate.catalog.Lookup(protocol.ToolEnterWorktree)
	handler := definition.handler.(*runtimeHandler)
	handler.questions, s.questions = old, old
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "approvals"); done <- err }()
	pending := waitBrokerPending(t, broker, s.id)
	if pending.ToolUseID != "bash" || pending.Kind != ApprovalPermission {
		t.Fatal("new workspace Bash failed before parking", pending)
	}
	if broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: "foreign", Allowed: true}) {
		t.Fatal("foreign approval admitted")
	}
	if !broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: s.id, Allowed: true}) {
		t.Fatal("own approval refused")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rows := broker.List(s.id)
		if len(rows) > 0 && rows[0].ToolUseID == "question" {
			pending = rows[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if pending.ToolUseID != "question" || pending.Kind != ApprovalQuestion {
		t.Fatal("question surface remained obsolete", pending)
	}
	answer := "yes"
	if !broker.Resolve(pending.ApprovalID, ApprovalResolution{SessionID: s.id, Allowed: true, Answer: &answer}) {
		t.Fatal("answer refused")
	}
	if err = receiveRun(t, done); err != nil {
		t.Fatal(err)
	}
	current := s.gate.policy.approver.(*ApprovalSurface)
	if current == old || current.binding.Workspace != target || current.binding.OwnerID != s.owner || current.binding.SessionID != s.id || handler.questions != current || s.questions != current {
		t.Fatal("broker rebind lost scope")
	}
	if old.matches(ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: target}) {
		t.Fatal("old surface widened")
	}
	blocks, _ := s.Messages()[2].Content.Blocks()
	bash, _ := blocks[1].ToolResult()
	question, _ := blocks[2].ToolResult()
	if strings.TrimSpace(bash.Content) != target || question.Content != "The user answered: yes" {
		t.Fatal("execution or question result differs", bash.Content, question.Content)
	}
}

type worktreeChildModel struct {
	target   string
	sawChild bool
	results  []string
}

func (p *worktreeChildModel) Complete(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	if len(r.Messages) != 1 {
		text := "parent done"
		if r.System != nil && strings.HasPrefix(*r.System, "You are a worker subagent") {
			text = "child done"
			blocks, _ := r.Messages[len(r.Messages)-1].Content.Blocks()
			for _, b := range blocks {
				if result, ok := b.ToolResult(); ok {
					p.results = append(p.results, result.Content)
				}
			}
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock(text)}, protocol.StopEndTurn), nil
	}
	if r.System != nil && strings.HasPrefix(*r.System, "You are a worker subagent") {
		p.sawChild = strings.Contains(*r.System, p.target)
		return fakeReply([]protocol.Block{enterWorktreeCall("enter-child", "one"), writeWorktreeCall("write-child", "child.txt", "child"), protocol.NewBashUse("pwd-child", "pwd")}, protocol.StopToolUse), nil
	}
	return fakeReply([]protocol.Block{enterWorktreeCall("enter-parent", "two")}, protocol.StopToolUse), nil
}

func TestWorktreeEntryPinsFreshChildrenAndForkUsesFreshScratch(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-worktree-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var source worktreeToolFixture
	if err = json.Unmarshal(raw, &source); err != nil || len(source.Cases) == 0 || source.Cases[0].Child == nil {
		t.Fatal("source child corpus missing", err)
	}
	child := source.Cases[0].Child
	repo := worktreeRepo(t)
	service, _ := worktrees.New(worktrees.Config{Repository: repo})
	for _, name := range []worktrees.Name{"one", "two"} {
		if _, err := service.Create(context.Background(), name, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	one, _ := service.PathFor("one")
	two, _ := service.PathFor("two")
	p := &worktreeChildModel{target: two}
	config := managerTestConfig(t.TempDir(), p)
	config.Services.WorktreeTools, config.Services.Worktrees, config.Services.RoleToolPolicy = true, service, allRoleTools{}
	manager := makeManager(t, config)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "alice", PermissionMode: ModeAuto})
	if output, err := parent.Run(context.Background(), "parent"); err != nil || output != child.Output {
		t.Fatal(err)
	}
	if summary, err := parent.core.Delegate(context.Background(), "child", RoleWorker); err != nil || summary != child.Summary {
		t.Fatal(err)
	}
	if !p.sawChild || parent.core.executionRoot() != two {
		t.Fatal("child inherited stale workspace or changed its parent", p.sawChild, parent.core.executionRoot(), two)
	}
	for i, result := range p.results {
		p.results[i] = strings.ReplaceAll(result, service.Repository(), "<REPO>")
	}
	if !reflect.DeepEqual(p.results, child.Results) || strings.ReplaceAll(parent.core.executionRoot(), service.Repository(), "<REPO>") != child.Execution {
		t.Fatal("source fresh child state differs", p.results, child.Results)
	}
	if raw, err := os.ReadFile(filepath.Join(two, "child.txt")); err != nil || string(raw) != child.Proof {
		t.Fatal("child did not inherit parent's current execution files", err)
	}
	if _, err := os.Stat(filepath.Join(one, "child.txt")); !os.IsNotExist(err) {
		t.Fatal("child wrote parent's pinned files")
	}
	fork, err := manager.Fork(context.Background(), "alice", parent.ID())
	if err != nil {
		t.Fatal(err)
	}
	if fork.core.executionRoot() == two || fork.core.executionRoot() != fork.core.workspace {
		t.Fatal("fork inherited execution directory")
	}
	if _, ok := fork.core.gate.catalog.Lookup(protocol.ToolEnterWorktree); !ok {
		t.Fatal("fork lost explicit manager tool activation")
	}
}

type worktreeChildFixture struct {
	Output, Summary, Execution, Proof string
	Results                           []string
}

type worktreeToolFixture struct {
	Schemas  []protocol.ToolSchema
	Metadata []struct {
		Name         protocol.ToolName
		Risk         ToolRisk
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Capabilities []Capability
	}
	Cases []struct {
		Configured bool
		Steps      []worktreeToolStep
		Proof      map[string]string
		Child      *worktreeChildFixture
	}
}
type worktreeToolStep struct {
	Name                         protocol.ToolName
	Input                        json.RawMessage
	Output, Execution, Lifecycle string
	Binding                      *string
	BoardInitialized             bool `json:"board_initialized"`
}
type worktreeFlow struct {
	t              *testing.T
	s              *Session
	steps          []worktreeToolStep
	repo, original string
	stage          int
}

func (p *worktreeFlow) normalize(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, p.original, "<WORKSPACE>"), p.repo, "<REPO>")
	return regexp.MustCompile(`(?m)(\s)[0-9a-f]{7,40}(\s)`).ReplaceAllString(text, "${1}<HEAD>${2}")
}
func (p *worktreeFlow) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if p.stage > 0 {
		step := p.steps[p.stage-1]
		blocks, _ := request.Messages[len(request.Messages)-1].Content.Blocks()
		result, ok := blocks[0].ToolResult()
		if !ok {
			p.t.Fatal("missing paired result")
		}
		text := p.normalize(result.Content)
		if step.Name == protocol.ToolListWorktrees {
			lines := strings.Split(text, "\n")
			for i := range lines {
				lines[i] = strings.Join(strings.Fields(lines[i]), " ")
			}
			text = strings.Join(lines, "\n")
		}
		if text != step.Output {
			p.t.Fatalf("step %d %s: %s / %s", p.stage-1, step.Name, text, step.Output)
		}
		if p.normalize(p.s.executionRoot()) != step.Execution || p.normalize(p.s.workspace) != step.Lifecycle {
			p.t.Fatalf("step %d workspace scope differs", p.stage-1)
		}
		definition, _ := p.s.gate.catalog.Lookup(protocol.ToolCreateWorktree)
		handler := definition.handler.(*runtimeHandler)
		if (handler.taskStore != nil) != step.BoardInitialized {
			p.t.Fatalf("step %d lazy board differs", p.stage-1)
		}
		board, err := tasks.New(tasks.Config{Workspace: p.original})
		if err != nil {
			p.t.Fatal(err)
		}
		linked, err := board.Load("task_link")
		if err != nil || !reflect.DeepEqual(linked.Worktree, step.Binding) {
			p.t.Fatal("task root/binding differs", err)
		}
	}
	if request.System == nil || !strings.Contains(*request.System, p.s.executionRoot()) {
		p.t.Fatal("model sees obsolete workspace")
	}
	if p.stage == len(p.steps) {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	step := p.steps[p.stage]
	p.stage++
	input, err := protocol.DecodeToolInput(step.Name, step.Input)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	return fakeReply([]protocol.Block{protocol.NewToolUse("worktree_call_"+string(rune('a'+p.stage)), input)}, protocol.StopToolUse), nil
}

func worktreeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v / %s", args, err, data)
		}
	}
	git("init", "-b", "main")
	for key, value := range map[string]string{"user.name": "Go parity", "user.email": "parity@example.invalid", "commit.gpgsign": "false", "core.hooksPath": "/dev/null", "core.autocrlf": "false"} {
		git("config", key, value)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".worktrees/\n.tasks/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", ".gitignore")
	git("commit", "-m", "base")
	return root
}

func TestInstalledWorktreeToolsMatchSourceAndScratchDeletionRetainsEnteredWork(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-worktree-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var source worktreeToolFixture
	if err = json.Unmarshal(data, &source); err != nil || len(source.Cases) != 3 || len(source.Schemas) != 5 || len(source.Metadata) != 5 {
		t.Fatal("incomplete source tool corpus", err)
	}
	if !reflect.DeepEqual(source.Schemas, protocol.WorktreeSchemas()) {
		t.Fatal("source schemas differ")
	}
	for _, scenario := range source.Cases {
		t.Run(map[bool]string{true: "configured", false: "unconfigured"}[scenario.Configured], func(t *testing.T) {
			repo := worktreeRepo(t)
			service, _ := worktrees.New(worktrees.Config{Repository: repo})
			p := &worktreeFlow{t: t, repo: service.Repository(), steps: scenario.Steps}
			config := managerTestConfig(t.TempDir(), p)
			config.Defaults.MaxRounds = 30
			config.Services.TaskTools, config.Services.WorktreeTools = true, true
			if scenario.Configured {
				config.Services.Worktrees = service
			}
			manager := makeManager(t, config)
			managed := createManaged(t, manager, CreateSessionRequest{Owner: "alice"})
			p.s, p.original = managed.core, managed.core.workspace
			if scenario.Configured && len(scenario.Steps) > 0 && scenario.Steps[0].Name == protocol.ToolEnterWorktree {
				if _, err := service.Create(context.Background(), "one", "", nil); err != nil {
					t.Fatal(err)
				}
			}
			board, err := tasks.New(tasks.Config{Workspace: p.original})
			if err != nil {
				t.Fatal(err)
			}
			if err = board.Save(tasks.Task{ID: "task_link", Subject: "linked task", Status: tasks.Pending, BlockedBy: []tasks.ID{}}); err != nil {
				t.Fatal(err)
			}
			for _, expected := range source.Metadata {
				d, ok := managed.core.gate.catalog.Lookup(expected.Name)
				if !ok || d.risk != expected.Risk || d.readonly != expected.Readonly || d.parallelSafe != expected.ParallelSafe || len(d.capabilities) != len(expected.Capabilities) {
					t.Fatalf("traits %s differ", expected.Name)
				}
			}
			if text, err := managed.Run(context.Background(), "exercise worktrees"); err != nil || text != "done" {
				t.Fatal(text, err)
			}
			for name, want := range scenario.Proof {
				got, err := os.ReadFile(filepath.Join(repo, ".worktrees", name, "proof.txt"))
				if err != nil || string(got) != want {
					t.Fatal("landed proof differs", err)
				}
			}
			if _, err := manager.Get("bob", managed.ID()); err != ErrSessionNotFound {
				t.Fatal("foreign session admitted")
			}
			if ok, err := manager.Delete("alice", managed.ID(), DeleteSessionOptions{}); !ok || err != nil {
				t.Fatal("delete", err)
			}
			if err := manager.WaitCleanup(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(p.original); !os.IsNotExist(err) {
				t.Fatal("original scratch not reclaimed")
			}
			for name, want := range scenario.Proof {
				got, err := os.ReadFile(filepath.Join(repo, ".worktrees", name, "proof.txt"))
				if err != nil || string(got) != want {
					t.Fatal("entered work was erased", err)
				}
			}
		})
	}
}

func TestReadonlyWorktreeRefusalAndDefaultCatalogue(t *testing.T) {
	repo := worktreeRepo(t)
	service, _ := worktrees.New(worktrees.Config{Repository: repo})
	root := t.TempDir()
	executor, err := shell.New(shell.Config{Workspace: root})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewRuntimeSession(RuntimeConfig{ID: "session", Owner: "alice", Workspace: root, Bash: executor, Provider: &FakeProvider{}, Mode: ModeReadonly, MaxRounds: 2, WorktreeTools: true, Worktrees: service})
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{ID: "create", Input: protocol.CreateWorktreeToolInput(protocol.CreateWorktreeInput{Name: "blocked"})}
	out, err := s.gate.Dispatch(context.Background(), ToolAuthority{SessionID: s.id, OwnerID: s.owner, Workspace: root, Mode: ModeReadonly}, call)
	if err != nil || !out.Denied {
		t.Fatal("readonly worktree admitted", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".tasks")); !os.IsNotExist(err) {
		t.Fatal("denied create made a board")
	}
	if _, err := os.Stat(service.Root()); !os.IsNotExist(err) {
		t.Fatal("denied create made a worktree")
	}
	plain, err := NewRuntimeSession(RuntimeConfig{ID: "plain", Owner: "alice", Workspace: root, Bash: executor, Provider: &FakeProvider{}, Mode: ModeAuto, MaxRounds: 2})
	if err != nil || len(plain.gate.catalog.Names()) != 10 {
		t.Fatal("opt-in changed defaults", err)
	}
}
