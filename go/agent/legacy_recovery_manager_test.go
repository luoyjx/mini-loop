package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type legacyRecoveryManagerSelection struct {
	Selected      *string
	RecoveryClass string `json:"recovery_class"`
}

type legacyRecoveryManagerCase struct {
	Name                 string
	SettingsEnvironment  legacyRecoveryEnvironment `json:"settings_environment"`
	ManagerEnvironment   legacyRecoveryEnvironment `json:"manager_environment"`
	FirstEnvironment     legacyRecoveryEnvironment `json:"first_environment"`
	SecondEnvironment    legacyRecoveryEnvironment `json:"second_environment"`
	RecoveryKind         string                    `json:"recovery_kind"`
	ExplicitFallback     *string                   `json:"explicit_fallback"`
	SettingsFallback     *string                   `json:"settings_fallback"`
	ManagerRecoveryClass *string                   `json:"manager_recovery_class"`
	First                legacyRecoveryManagerSelection
	Second               legacyRecoveryManagerSelection
	FirstAfterSecond     legacyRecoveryManagerSelection `json:"first_after_second"`
	SharedRecovery       bool                           `json:"shared_recovery"`
	FirstUsesInjected    bool                           `json:"first_uses_injected"`
	SecondUsesInjected   bool                           `json:"second_uses_injected"`
}

type legacyRecoveryManagerFixture struct {
	Managers []legacyRecoveryManagerCase
}

func legacyRecoveryManagerConfig(t *testing.T) ManagerConfig {
	t.Helper()
	provider := stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		t.Fatal("constructing a session must not call the model")
		return protocol.ModelReply{}, nil
	})
	cfg := managerTestConfig(t.TempDir(), provider)
	cfg.Services.BashFactory = bashFactoryFunc(func(context.Context, SessionBinding) (BashExecutor, error) {
		return echoExecutor{}, nil
	})
	return cfg
}

func assertLegacyManagerSelection(t *testing.T, recovery Recovery, want legacyRecoveryManagerSelection) {
	t.Helper()
	selected, class := legacyRecoverySelected(t, recovery)
	got := legacyRecoveryManagerSelection{Selected: selected, RecoveryClass: class}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected=%v class=%s; want selected=%v class=%s", selected, class, want.Selected, want.RecoveryClass)
	}
}

func TestLegacyRecoveryMatchesActualSourceManagers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-legacy-recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture legacyRecoveryManagerFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Managers) != 5 {
		t.Fatalf("Source manager cases=%d; want 5", len(fixture.Managers))
	}
	for _, row := range fixture.Managers {
		t.Run(row.Name, func(t *testing.T) {
			settings := loadLegacyRecoverySettings(t, row.SettingsEnvironment)
			if !reflect.DeepEqual(settings.FallbackModel, row.SettingsFallback) {
				t.Fatal("Settings modern fallback differs from Source", settings.FallbackModel, row.SettingsFallback)
			}
			setLegacyRecoveryEnvironment(t, row.ManagerEnvironment)
			cfg := legacyRecoveryManagerConfig(t)
			// Source Settings contributes only its modern value here. Its legacy
			// environment is deliberately read at each subsequent Agent creation.
			defaults := RecoveryConfig{}
			if settings.FallbackModel != nil {
				defaults.FallbackModel = *settings.FallbackModel
			}
			cfg.Services.DefaultRecoveryConfig = &defaults
			switch row.RecoveryKind {
			case "default":
			case "explicit":
				policyConfig := RecoveryConfig{}
				if row.ExplicitFallback != nil {
					policyConfig.FallbackModel = *row.ExplicitFallback
				}
				cfg.Services.Recovery, err = NewDefaultRecovery(policyConfig)
				if err != nil {
					t.Fatal(err)
				}
			case "direct":
				cfg.Services.Recovery = DirectRecovery{}
			default:
				t.Fatal("unknown Source recovery kind", row.RecoveryKind)
			}
			manager := makeManager(t, cfg)
			var managerClass *string
			if manager.config.Services.Recovery != nil {
				_, class := legacyRecoverySelected(t, manager.config.Services.Recovery)
				managerClass = &class
			}
			if !reflect.DeepEqual(managerClass, row.ManagerRecoveryClass) {
				t.Fatal("manager injected recovery class differs from Source", managerClass, row.ManagerRecoveryClass)
			}
			setLegacyRecoveryEnvironment(t, row.FirstEnvironment)
			first := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
			assertLegacyManagerSelection(t, first.core.recovery, row.First)
			setLegacyRecoveryEnvironment(t, row.SecondEnvironment)
			second := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
			assertLegacyManagerSelection(t, second.core.recovery, row.Second)
			assertLegacyManagerSelection(t, first.core.recovery, row.FirstAfterSecond)
			// These fixture variants contain only comparable built-in policies.
			if shared := first.core.recovery == second.core.recovery; shared != row.SharedRecovery {
				t.Fatal("session policy sharing differs from Source", shared, row.SharedRecovery)
			}
			if used := first.core.recovery == cfg.Services.Recovery; used != row.FirstUsesInjected {
				t.Fatal("first session injected policy differs from Source", used, row.FirstUsesInjected)
			}
			if used := second.core.recovery == cfg.Services.Recovery; used != row.SecondUsesInjected {
				t.Fatal("second session injected policy differs from Source", used, row.SecondUsesInjected)
			}
		})
	}
}

func TestLegacyRecoveryManagerDetachesDefaultConfig(t *testing.T) {
	t.Setenv("FALLBACK_MODEL_ID", "ambient")
	legacy, retries, continuations, escalate := "fixed", 7, 3, false
	waiter := &recoveryTestWaiter{}
	cfg := legacyRecoveryManagerConfig(t)
	cfg.Services.DefaultRecoveryConfig = &RecoveryConfig{
		LegacyFallbackModel: &legacy, MaxRetries: &retries, MaxContinuations: &continuations,
		Escalate: &escalate, Waiter: waiter, Jitter: func() float64 { return 0.25 },
	}
	manager := makeManager(t, cfg)
	legacy, retries, continuations, escalate = "mutated", -1, -1, true
	cfg.Services.DefaultRecoveryConfig.FallbackModel = "mutated-modern"
	session := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	policy, ok := session.core.recovery.(*DefaultRecovery)
	if !ok || policy.fallback != "fixed" || policy.retries != 7 || policy.continuations != 3 || policy.escalate || policy.wait != waiter || policy.jitter() != 0.25 {
		t.Fatalf("manager retained caller configuration: %#v", policy)
	}
	// A delegated child inherits the already selected parent policy, even when
	// the ambient environment changes before fresh child construction.
	t.Setenv("FALLBACK_MODEL_ID", "child-ambient")
	run, err := DefaultRunContext()
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := newSubagent(context.Background(), SubagentRequest{Parent: session.core.subagentParent(run), Role: RoleExplore, RunContext: run})
	if err != nil {
		t.Fatal(err)
	}
	if child.recovery != session.core.recovery {
		t.Fatal("child replaced the parent recovery policy")
	}
}

func TestLegacyRecoveryManagerValidatesOnlySelectedDefault(t *testing.T) {
	for _, kind := range []string{"retries", "continuations"} {
		for _, invalid := range []int{-1, 101} {
			t.Run(kind+"/"+strconv.Itoa(invalid), func(t *testing.T) {
				cfg := legacyRecoveryManagerConfig(t)
				cfg.WorkspaceRoot = filepath.Join(cfg.WorkspaceRoot, "not-created")
				defaults := &RecoveryConfig{}
				if kind == "retries" {
					defaults.MaxRetries = &invalid
				} else {
					defaults.MaxContinuations = &invalid
				}
				cfg.Services.DefaultRecoveryConfig = defaults
				if _, err := NewSessionManager(cfg); err == nil || err.Error() != "invalid recovery configuration" {
					t.Fatal("invalid default accepted", err)
				}
				if _, err := os.Stat(cfg.WorkspaceRoot); !os.IsNotExist(err) {
					t.Fatal("invalid default allocated workspace", err)
				}
				cfg.Services.Recovery = DirectRecovery{}
				manager := makeManager(t, cfg)
				session := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
				if _, ok := session.core.recovery.(DirectRecovery); !ok {
					t.Fatal("unused invalid default replaced explicit recovery")
				}
			})
		}
	}
}
