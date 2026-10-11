package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
)

func TestRuntimePostureSourceProfilesAndNativeLabels(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-posture.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name   string
			Direct RuntimePosture
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	env := secrets.Environment{"FIXTURE_API_KEY": "fixture-value", "fixture_token": "fixture-value", "EMPTY_SECRET": "", "ORDINARY": "fixture-value"}
	for _, row := range fixture.Cases {
		switch row.Name {
		case "default-empty", "default-live", "null-registry", "unregistered", "registered", "registered-case-sensitive", "live-overrides", "workflows", "full-settings-custom-manager":
		default:
			continue // SQLite/fault/resources/trajectory and unavailable sandbox require separate profiles.
		}
		t.Run(row.Name, func(t *testing.T) {
			config := managerTestConfig(t.TempDir(), &recordingProvider{})
			switch row.Name {
			case "null-registry":
				config.Services.Secrets = secrets.Null{}
			case "unregistered", "registered", "registered-case-sensitive":
				registry := secrets.New(secrets.Config{})
				if row.Name == "registered" {
					registry.RegisterLookup("FIXTURE_API_KEY", func() (string, error) { t.Fatal("posture resolved credentials"); return "", nil })
					registry.RegisterValue("fixture_token", "short")
				} else if row.Name == "registered-case-sensitive" {
					registry.RegisterValue("FIXTURE_TOKEN", "fixture-value")
				}
				config.Services.Secrets = registry
			case "workflows":
				config.Services.WorkflowTools = true
			}
			manager := makeManager(t, config)
			if row.Name == "default-live" || row.Name == "live-overrides" {
				live := createManaged(t, manager, CreateSessionRequest{Owner: "fixture-owner"})
				if row.Name == "live-overrides" {
					live.core.cachePolicy, live.core.stuckDetector = NullCachePolicy{}, NullStuckDetector{}
					live.core.secrets = secrets.New(secrets.Config{})
				}
			}
			before := manager.Summary().Sessions
			got, err := manager.ObserveRuntimePosture(false, env)
			if err != nil {
				t.Fatal(err)
			}
			want := row.Direct
			// Native absent components and Registry have explicit native labels.

			if want.Sandbox == "NullSandbox" {
				want.Sandbox = "None"
			}
			if row.Name == "live-overrides" {
				// Source's unavailable sandbox override has no native seam yet.
				want.Sandbox, want.SandboxReason = "None", nil
			}
			if want.Secrets == "NullSecretRegistry" && row.Name != "null-registry" {
				want.Secrets = "None"
			}
			if want.Secrets == "SecretRegistry" {
				want.Secrets = "Registry"
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("native=%+v Source (native labels)=%+v", got, want)
			}
			if manager.Summary().Sessions != before {
				t.Fatal("posture created a session")
			}
		})
	}
}

func TestRuntimePostureReportsActualPersistenceFaultDetached(t *testing.T) {
	store := newRuntimeStateStore()
	config := managerTestConfig(t.TempDir(), &recordingProvider{})
	config.Services.StateStore = store
	manager := makeManager(t, config)
	handle := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	store.mu.Lock()
	store.faults["event"] = errors.New("fixture disk unavailable")
	store.mu.Unlock()
	// Real event append failure, rather than directly seeding the reported error.
	if _, err := handle.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	got, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || got.StateStoreError == nil || *got.StateStoreError != "*errors.errorString: fixture disk unavailable" {
		t.Fatal(got, err)
	}
	*got.StateStoreError = "mutated"
	again, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || *again.StateStoreError != "*errors.errorString: fixture disk unavailable" {
		t.Fatal(again, err)
	}
	if _, err := manager.Delete("owner", handle.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
}

type observingRegistry struct {
	*secrets.Registry
	manager *SessionManager
	fail    bool
	calls   int
}

func (r *observingRegistry) Names() []secrets.Name {
	r.calls++
	if r.fail {
		panic("private registry failure")
	}
	r.manager.Summary() // A callback can observe the manager; its lock must be released.
	return r.Registry.Names()
}

func TestRuntimePostureRegistryCallbackAndFirstLiveOrder(t *testing.T) {
	registry := &observingRegistry{Registry: secrets.New(secrets.Config{})}
	config := managerTestConfig(t.TempDir(), &recordingProvider{})
	config.Services.Secrets = registry
	manager := makeManager(t, config)
	registry.manager = manager
	if _, err := manager.ObserveRuntimePosture(false, secrets.Environment{}); err != nil || registry.calls != 1 {
		t.Fatal(err, registry.calls)
	}
	registry.fail = true
	got, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err == nil || got != (RuntimePosture{}) {
		t.Fatal("failed observation reported posture", got, err)
	}
	registry.fail = false
	first := createManaged(t, manager, CreateSessionRequest{Owner: "first"})
	second := createManaged(t, manager, CreateSessionRequest{Owner: "second"})
	first.core.cachePolicy = NullCachePolicy{}
	got, err = manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || got.CachePolicy != "NullCachePolicy" {
		t.Fatal(got, err)
	}
	if _, err := manager.Delete("first", first.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err = manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || got.CachePolicy != "DefaultCachePolicy" || second.ID() == "" {
		t.Fatal(got, err)
	}
}

type postureSandbox struct{ reason string }

type reboundPostureSandbox struct{ postureSandbox }

func (s postureSandbox) ForWorkspace(root string) (shell.Sandbox, error) {
	if filepath.Base(root) == "changed" {
		return reboundPostureSandbox{s}, nil
	}
	return s, nil
}
func (s postureSandbox) Argv(string) ([]string, error) {
	return nil, errors.New("posture must not execute commands")
}
func (s postureSandbox) SandboxReason() *string { return &s.reason }

type postureBashFactory struct{ sandbox postureSandbox }

func (f postureBashFactory) BashFor(ctx context.Context, binding SessionBinding) (BashExecutor, error) {
	return shell.New(shell.Config{Workspace: binding.Workspace, Sandbox: f.sandbox})
}
func (f postureBashFactory) SandboxPosture() shell.SandboxPosture {
	return shell.SandboxPosture{Name: "postureSandbox", Reason: clonePointer(f.sandbox.SandboxReason())}
}

func TestRuntimePostureObservesInjectedSandboxAndWorkspacePublication(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &recordingProvider{})
	config.Services.BashFactory = postureBashFactory{postureSandbox{"fixture backend unavailable"}}
	manager := makeManager(t, config)
	before, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || before.Sandbox != "postureSandbox" || *before.SandboxReason != "fixture backend unavailable" {
		t.Fatal(before, err)
	}
	handle := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	got, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || got.Sandbox != before.Sandbox || *got.SandboxReason != *before.SandboxReason {
		t.Fatal(got, err)
	}
	target := filepath.Join(t.TempDir(), "changed")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := handle.core.runtime.enterWorkspace(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	got, err = manager.ObserveRuntimePosture(false, secrets.Environment{})
	if err != nil || got.Sandbox != "reboundPostureSandbox" || *got.SandboxReason != *before.SandboxReason {
		t.Fatal(got, err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			view, err := manager.ObserveRuntimePosture(false, secrets.Environment{})
			if err != nil {
				done <- err
				return
			}
			if view.Sandbox != "postureSandbox" && view.Sandbox != "reboundPostureSandbox" {
				done <- errors.New("inconsistent published sandbox")
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 10; i++ {
		if err := handle.core.runtime.enterWorkspace(context.Background(), t.TempDir()); err != nil {
			t.Fatal(err)
		}
		if err := handle.core.runtime.enterWorkspace(context.Background(), target); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRuntimePostureRefusesUnobservableCustomExecutor(t *testing.T) {
	config := managerTestConfig(t.TempDir(), &recordingProvider{})
	config.Services.BashFactory = bashFactoryFunc(func(ctx context.Context, binding SessionBinding) (BashExecutor, error) {
		return shell.New(shell.Config{Workspace: binding.Workspace})
	})
	manager := makeManager(t, config)
	if got, err := manager.ObserveRuntimePosture(false, secrets.Environment{}); err == nil || got != (RuntimePosture{}) {
		t.Fatal(got, err)
	}
	createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	if got, err := manager.ObserveRuntimePosture(false, secrets.Environment{}); err != nil || got.Sandbox != "None" {
		t.Fatal(got, err)
	}
}
