package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type workspaceFactoryFunc func(context.Context, SessionID) (string, error)

func (f workspaceFactoryFunc) WorkspaceFor(c context.Context, id SessionID) (string, error) {
	return f(c, id)
}

type bashFactoryFunc func(context.Context, SessionBinding) (BashExecutor, error)

func (f bashFactoryFunc) BashFor(c context.Context, b SessionBinding) (BashExecutor, error) {
	return f(c, b)
}
func managerTestConfig(root string, provider Provider) ManagerConfig {
	return ManagerConfig{WorkspaceRoot: root, Services: ManagerServices{Provider: provider}, Defaults: SessionDefaults{PermissionMode: ModeAuto, MaxRounds: 2}, DeleteGrace: time.Millisecond, ShutdownGrace: time.Millisecond}
}
func makeManager(t *testing.T, config ManagerConfig) *SessionManager {
	t.Helper()
	m, err := NewSessionManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := m.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}
func createManaged(t *testing.T, m *SessionManager, request CreateSessionRequest) *ManagedSession {
	t.Helper()
	s, err := m.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestManagerCreatesIsolatesOwnersAndSharesDefaultServices(t *testing.T) {
	var bindings []SessionBinding
	var mu sync.Mutex
	config := managerTestConfig(filepath.Join(t.TempDir(), "scratch"), &FakeProvider{})
	config.Services.BashFactory = bashFactoryFunc(func(_ context.Context, b SessionBinding) (BashExecutor, error) {
		mu.Lock()
		bindings = append(bindings, b)
		mu.Unlock()
		return echoExecutor{}, nil
	})
	system := "fixed"
	config.Defaults.System = &system
	m := makeManager(t, config)
	system = "caller mutation"
	a := createManaged(t, m, CreateSessionRequest{Owner: "a"})
	b := createManaged(t, m, CreateSessionRequest{Owner: "b"})
	if len(a.ID()) != 12 || a.ID() == b.ID() || a.Info().Workspace == b.Info().Workspace || a.Info().WorkspaceBound || b.Info().WorkspaceBound {
		t.Fatal("scratch identity/binding drift")
	}
	if a.core.modelLimiter != b.core.modelLimiter || a.core.toolLimiter != b.core.toolLimiter || cap(a.core.modelLimiter.slots) != 8 || cap(a.core.toolLimiter.slots) != 8 || a.approvals != b.approvals || a.core.gate.journal != b.core.gate.journal {
		t.Fatal("default fleet services are not shared")
	}
	if _, err := m.Get("b", a.ID()); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("foreign session visible")
	}
	if _, err := m.Cancel(context.Background(), "b", a.ID(), "foreign"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("foreign cancellation allowed")
	}
	if _, err := m.Delete("b", a.ID(), DeleteSessionOptions{}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("foreign deletion allowed")
	}
	if len(m.List("a")) != 1 || len(m.List("b")) != 1 || len(m.List("")) != 0 {
		t.Fatal("owner listing drift")
	}
	if _, err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if len(b.Messages()) != 0 || len(bindings) != 2 || bindings[0].Owner != "a" || bindings[0].Workspace != a.core.workspace {
		t.Fatal("session state/factory authority mixed")
	}
	built, err := a.core.systemBuilder.BuildSystem(SystemContext{})
	if err != nil || built != "fixed" {
		t.Fatal("default system template mutated", err)
	}
	path := a.Info().Workspace
	if deleted, err := m.Delete("a", a.ID(), DeleteSessionOptions{}); !deleted || err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("idle scratch retained", err)
	}
	owners := m.RememberedOwners()
	if owners[a.ID()] != "a" {
		t.Fatal("deleted owner forgotten")
	}
	owners[a.ID()] = "foreign"
	if m.RememberedOwners()[a.ID()] != "a" {
		t.Fatal("owner map aliases manager")
	}
	if len(m.List("a")) != 0 {
		t.Fatal("deleted session listed")
	}
}

func TestManagerDefaultHostShellAndExplicitSessionOverrides(t *testing.T) {
	m := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	model, system := "custom-model", ""
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner", Model: &model, System: &system, PermissionMode: ModeAuto})
	model = "mutated"
	output, err := s.Run(context.Background(), "real")
	if err != nil || !strings.Contains(output, "handled: real") {
		t.Fatal(output, err)
	}
	if s.Info().Model != "custom-model" {
		t.Fatal("model override changed")
	}
	built, _ := s.core.systemBuilder.BuildSystem(SystemContext{})
	if built != "" {
		t.Fatal("empty explicit system dropped")
	}
	if err = protocol.ValidateTranscript(s.Messages()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerBindingPoliciesBeforeExistenceAndBoundDeletion(t *testing.T) {
	base := t.TempDir()
	allowed := filepath.Join(base, "allowed")
	checkout := filepath.Join(allowed, "repo")
	outside := filepath.Join(base, "outside")
	os.MkdirAll(checkout, 0700)
	os.Mkdir(outside, 0700)
	os.WriteFile(filepath.Join(checkout, "keep"), []byte("source"), 0600)
	config := managerTestConfig(filepath.Join(base, "scratch"), &FakeProvider{})
	config.BindableRoots = []string{allowed, base}
	m := makeManager(t, config)
	// Narrow roots after constructing a separate manager; public config was copied.
	narrowConfig := config
	narrowConfig.WorkspaceRoot = filepath.Join(base, "narrow")
	narrowConfig.BindableRoots = []string{allowed}
	narrow := makeManager(t, narrowConfig)
	os.Symlink(outside, filepath.Join(allowed, "escape"))
	os.Symlink(checkout, filepath.Join(allowed, "alias"))
	for _, entry := range []struct {
		path     string
		status   WorkspaceBindingStatus
		fragment string
	}{
		{outside, BindingForbidden, "outside every"}, {filepath.Join(outside, "absent"), BindingForbidden, "outside every"}, {filepath.Join(allowed, "escape"), BindingForbidden, "outside every"}, {filepath.Join(allowed, "absent"), BindingInvalid, "not an existing"}, {filepath.Join(checkout, "keep"), BindingInvalid, "not an existing"},
	} {
		t.Run(entry.path, func(t *testing.T) {
			_, err := narrow.Create(context.Background(), CreateSessionRequest{Owner: "owner", Workspace: &entry.path})
			var binding *WorkspaceBindingError
			if !errors.As(err, &binding) || binding.Status != entry.status || !strings.Contains(binding.Detail, entry.fragment) {
				t.Fatal(err)
			}
		})
	}
	for _, path := range []string{m.WorkspaceRoot(), filepath.Join(m.WorkspaceRoot(), "absent")} {
		_, err := m.Create(context.Background(), CreateSessionRequest{Owner: "owner", Workspace: &path})
		var binding *WorkspaceBindingError
		if !errors.As(err, &binding) || binding.Status != BindingForbidden || !strings.Contains(binding.Detail, "manager's own") {
			t.Fatal(err)
		}
	}
	path := filepath.Join(allowed, "alias")
	s := createManaged(t, narrow, CreateSessionRequest{Owner: "owner", Workspace: &path})
	resolvedCheckout, _ := filepath.EvalSymlinks(checkout)
	if !s.Info().WorkspaceBound || s.Info().Workspace != resolvedCheckout {
		t.Fatal("binding did not resolve symlink")
	}
	narrow.Delete("owner", s.ID(), DeleteSessionOptions{})
	if data, err := os.ReadFile(filepath.Join(checkout, "keep")); err != nil || string(data) != "source" {
		t.Fatal("bound checkout reclaimed", err)
	}
	off := makeManager(t, managerTestConfig(filepath.Join(base, "off"), &FakeProvider{}))
	_, err := off.Create(context.Background(), CreateSessionRequest{Owner: "owner", Workspace: &checkout})
	var refusal *WorkspaceBindingError
	if !errors.As(err, &refusal) || refusal.Status != BindingForbidden || off.WorkspaceBindingEnabled() {
		t.Fatal("binding enabled by default", err)
	}
}

type drainingManagerProvider struct {
	entered, cancelled, release, exited chan struct{}
	once                                sync.Once
}

func (p *drainingManagerProvider) Complete(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	p.once.Do(func() { close(p.entered) })
	<-ctx.Done()
	close(p.cancelled)
	<-p.release
	close(p.exited)
	return protocol.ModelReply{}, ctx.Err()
}
func newDrainingProvider() *drainingManagerProvider {
	return &drainingManagerProvider{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{})}
}

func TestManagerDeleteStopsAdmissionAndWaitsBeforeScratchReclamation(t *testing.T) {
	provider := newDrainingProvider()
	m := makeManager(t, managerTestConfig(t.TempDir(), provider))
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	path := s.Info().Workspace
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "blocked"); done <- err }()
	receiveSignal(t, provider.entered)
	if deleted, err := m.Delete("owner", s.ID(), DeleteSessionOptions{}); !deleted || err != nil {
		t.Fatal(err)
	}
	receiveSignal(t, provider.cancelled)
	if _, err := s.Run(context.Background(), "queued"); err == nil || !strings.Contains(err.Error(), "session deleted") {
		t.Fatal("deleted handle accepted work", err)
	}
	if _, err := m.Get("owner", s.ID()); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("deleted handle discoverable")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("workspace reclaimed before provider drained", err)
	}
	wait, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := m.WaitCleanup(wait); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cleanup reported success with live worker", err)
	}
	close(provider.release)
	if err := receiveRun(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	receiveSignal(t, provider.exited)
	if err := m.WaitCleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("drained workspace retained", err)
	}
}

func TestManagerStopDrainsDeletedTurnsAndCanResumeWaiting(t *testing.T) {
	provider := newDrainingProvider()
	m := makeManager(t, managerTestConfig(t.TempDir(), provider))
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), "go"); done <- err }()
	receiveSignal(t, provider.entered)
	m.Delete("owner", s.ID(), DeleteSessionOptions{})
	receiveSignal(t, provider.cancelled)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := m.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) || m.State() != ManagerStopping {
		t.Fatal("shutdown did not await deleted turn", err)
	}
	if _, err := m.Create(context.Background(), CreateSessionRequest{Owner: "owner"}); !errors.Is(err, ErrManagerStopped) {
		t.Fatal("create after stop", err)
	}
	close(provider.release)
	receiveRun(t, done)
	if err := m.Stop(context.Background()); err != nil || m.State() != ManagerStopped {
		t.Fatal(err)
	}
}

func TestManagerSharedRetiringWorkspaceAndSymlinkReclamation(t *testing.T) {
	provider := newDrainingProvider()
	base := t.TempDir()
	shared := filepath.Join(base, "shared")
	config := managerTestConfig(filepath.Join(base, "root"), provider)
	config.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { return shared, nil })
	m := makeManager(t, config)
	a := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	b := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	done := make(chan error, 1)
	go func() { _, err := a.Run(context.Background(), "go"); done <- err }()
	receiveSignal(t, provider.entered)
	m.Delete("owner", a.ID(), DeleteSessionOptions{})
	receiveSignal(t, provider.cancelled)
	m.Delete("owner", b.ID(), DeleteSessionOptions{})
	if _, err := os.Stat(shared); err != nil {
		t.Fatal("second delete reclaimed another retiring turn's workspace", err)
	}
	close(provider.release)
	receiveRun(t, done)
	m.WaitCleanup(context.Background())
	if _, err := os.Stat(shared); !os.IsNotExist(err) {
		t.Fatal("last retiring reference leaked scratch", err)
	}
	other := makeManager(t, managerTestConfig(filepath.Join(base, "other"), &FakeProvider{}))
	s := createManaged(t, other, CreateSessionRequest{Owner: "owner"})
	path := s.Info().Workspace
	target := filepath.Join(base, "operator")
	os.Mkdir(target, 0700)
	os.WriteFile(filepath.Join(target, "keep"), []byte("keep"), 0600)
	os.Rename(path, path+"-saved")
	os.Symlink(target, path)
	other.Delete("owner", s.ID(), DeleteSessionOptions{})
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("scratch link not removed", err)
	}
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Fatal("scratch removal followed replacement symlink", err)
	}
}

func TestManagerConcurrentSharedScratchDeletesReclaimLastReference(t *testing.T) {
	base := t.TempDir()
	shared := filepath.Join(base, "shared")
	config := managerTestConfig(filepath.Join(base, "root"), &FakeProvider{})
	config.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { return shared, nil })
	m := makeManager(t, config)
	a := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	b := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	m.workspaceMu.Lock()
	deleted := make(chan error, 2)
	for _, session := range []*ManagedSession{a, b} {
		go func() { _, err := m.Delete("owner", session.ID(), DeleteSessionOptions{}); deleted <- err }()
	}
	deadline := time.After(3 * time.Second)
	for {
		m.mu.Lock()
		retired := len(m.retiring) == 2
		m.mu.Unlock()
		if retired {
			break
		}
		select {
		case <-deadline:
			m.workspaceMu.Unlock()
			t.Fatal("deletes did not unregister both handles")
		default:
			runtime.Gosched()
		}
	}
	m.workspaceMu.Unlock()
	for range 2 {
		if err := <-deleted; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(shared); !os.IsNotExist(err) {
		t.Fatal("last reference left shared scratch behind", err)
	}
}

func TestManagerHomeBindingPreservesSymlinkParentOrder(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	outside := filepath.Join(base, "outside")
	checkout := filepath.Join(outside, "checkout")
	for _, path := range []string{home, filepath.Join(outside, "branch"), checkout, filepath.Join(home, "checkout")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "branch"), filepath.Join(home, "alias")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	config := managerTestConfig(filepath.Join(base, "root"), &FakeProvider{})
	config.BindableRoots = []string{outside}
	m := makeManager(t, config)
	requested := "~/alias/../checkout"
	session := createManaged(t, m, CreateSessionRequest{Owner: "owner", Workspace: &requested})
	want, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		t.Fatal(err)
	}
	if session.Info().Workspace != want {
		t.Fatal("home expansion cleaned symlink parent before resolution", session.Info().Workspace, want)
	}
}

func TestManagerStopDuringCreateAndFailedFactoryOwnership(t *testing.T) {
	base := t.TempDir()
	entered, release := make(chan struct{}), make(chan struct{})
	path := filepath.Join(base, "allocation")
	config := managerTestConfig(filepath.Join(base, "root"), &FakeProvider{})
	config.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { close(entered); <-release; return path, nil })
	m := makeManager(t, config)
	created := make(chan error, 1)
	go func() { _, err := m.Create(context.Background(), CreateSessionRequest{Owner: "owner"}); created <- err }()
	receiveSignal(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := m.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown skipped in-flight allocation", err)
	}
	close(release)
	if err := receiveRun(t, created); !errors.Is(err, ErrManagerStopped) {
		t.Fatal("in-flight create published after stop", err)
	}
	m.Stop(context.Background())
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rejected allocation retained", err)
	}
	sentinel := filepath.Join(base, "sentinel")
	os.Mkdir(sentinel, 0700)
	for _, failure := range []string{"error", "empty"} {
		t.Run(failure, func(t *testing.T) {
			cfg := managerTestConfig(filepath.Join(base, failure), &FakeProvider{})
			cfg.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) {
				if failure == "error" {
					return sentinel, errors.New("not allocated")
				}
				return "", nil
			})
			bad := makeManager(t, cfg)
			if _, err := bad.Create(context.Background(), CreateSessionRequest{Owner: "owner"}); err == nil {
				t.Fatal("factory failure ignored")
			}
			if _, err := os.Stat(sentinel); err != nil {
				t.Fatal("failed factory path was reclaimed", err)
			}
		})
	}
}

func TestManagerOwnerAndCleanupDiagnosticsStayBoundedDetached(t *testing.T) {
	m := makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{}))
	for i := 0; i < MaxRememberedOwners+3; i++ {
		session := &ManagedSession{core: &Session{id: SessionID(fmt.Sprint(i)), owner: "owner"}}
		m.mu.Lock()
		m.rememberOwnerLocked(session)
		m.mu.Unlock()
	}
	owners := m.RememberedOwners()
	if len(owners) != MaxRememberedOwners || owners["0"] != "" || owners["10002"] != "owner" {
		t.Fatal("owner retention unbounded or wrong eviction")
	}
	for i := 0; i < MaxCleanupErrors+3; i++ {
		m.recordCleanupError(SessionID(fmt.Sprint(i)), "path", errors.New(strings.Repeat("x", 1000)))
	}
	diagnostics := m.CleanupErrors()
	if len(diagnostics) != MaxCleanupErrors || diagnostics[0].SessionID != "3" || len(diagnostics[0].Error) > 530 {
		t.Fatal("cleanup diagnostics unbounded")
	}
	diagnostics[0].Error = "mutated"
	if m.CleanupErrors()[0].Error == "mutated" {
		t.Fatal("diagnostics aliases manager")
	}
}

func TestManagerStopClosesQueuedAdmissionBeforeCancellingHolder(t *testing.T) {
	provider := newDrainingProvider()
	close(provider.release)
	m := makeManager(t, managerTestConfig(t.TempDir(), provider))
	s := createManaged(t, m, CreateSessionRequest{Owner: "owner"})
	holder := make(chan error, 1)
	queued := make(chan error, 1)
	started := make(chan struct{})
	go func() { _, err := s.Run(context.Background(), "holder"); holder <- err }()
	receiveSignal(t, provider.entered)
	go func() { close(started); _, err := s.Run(context.Background(), "queued"); queued <- err }()
	receiveSignal(t, started)
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := receiveRun(t, holder); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := receiveRun(t, queued); err == nil || !strings.Contains(err.Error(), "session manager stopped") {
		t.Fatal("queued turn ran during shutdown", err)
	}
	if s.Info().RunCount != 1 || s.Info().Busy {
		t.Fatal("queued turn changed terminal state")
	}
	if _, err := os.Stat(s.Info().Workspace); err != nil {
		t.Fatal("stop removed a surviving session's scratch", err)
	}
}

func TestManagerInvalidDefaultsDoNotAllocateWorkspace(t *testing.T) {
	for _, kind := range []string{"provider", "budget", "mode", "pool", "prompt", "injector"} {
		t.Run(kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "unallocated")
			cfg := managerTestConfig(root, &FakeProvider{})
			switch kind {
			case "provider":
				cfg.Services.Provider = nil
			case "budget":
				cfg.Defaults.MaxRounds = -1
			case "mode":
				cfg.Defaults.PermissionMode = "unknown"
			case "pool":
				cfg.Services.ModelLimiter = &ConcurrencyLimiter{}
			case "prompt":
				cfg.Services.UserPromptHooks = []UserPromptHook{nil}
			case "injector":
				cfg.Services.Injectors = []MessageInjector{nil}
			}
			if _, err := NewSessionManager(cfg); err == nil {
				t.Fatal("invalid manager config accepted")
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("invalid constructor allocated workspace", err)
			}
		})
	}
}
