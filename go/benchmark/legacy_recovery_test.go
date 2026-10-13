package benchmark

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type benchmarkLegacyRecoveryCase struct {
	name        string
	environment map[string]string
	fallback    string
}

type benchmarkLegacyOverloaded struct{}

func (benchmarkLegacyOverloaded) Error() string { return "benchmark fixture overloaded" }

func (benchmarkLegacyOverloaded) RecoveryFailure() protocol.ModelFailure {
	zero := 0.0
	return protocol.ModelFailure{
		Kind: protocol.ModelFailureStatus, Class: "BenchmarkOverloaded", Status: 529,
		Message: "benchmark fixture overloaded", RetryAfterSeconds: &zero,
	}
}

type benchmarkLegacyRecoveryProvider struct {
	models []string
}

func (provider *benchmarkLegacyRecoveryProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.models = append(provider.models, request.Model)
	if len(provider.models) <= 3 {
		return protocol.ModelReply{}, benchmarkLegacyOverloaded{}
	}
	return protocol.ModelReply{
		ID: "benchmark-recovery", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant,
		Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn,
		Usage: protocol.TokenUsage{InputTokens: protocol.IntegerUsageCount(1), OutputTokens: protocol.IntegerUsageCount(1)},
	}, nil
}

func fakeLegacyRecoverySettings(t *testing.T, environment map[string]string) config.Settings {
	t.Helper()
	root := t.TempDir()
	settings, err := config.Load(environment, config.LoadOptions{Directory: root, HomeDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Keep the constructed manager's resources local during ambient lookup tests.
	settings.WorkspaceRoot = root
	settings.MemoryRoot = nil
	settings.UserResourcesRoot = nil
	settings.SkillsDir = config.BuiltinSkills
	settings.TrajectoryEnabled = false
	return settings
}

func assertFakeLegacyRecovery(t *testing.T, cfg agent.ManagerConfig, fallback string) {
	t.Helper()
	if cfg.Services.Recovery != nil || cfg.Services.DefaultRecoveryConfig == nil {
		t.Fatal("fake manager must defer default recovery selection to session creation")
	}
	provider := &benchmarkLegacyRecoveryProvider{}
	cfg.Services.Provider = provider
	cfg.Defaults.Model = "primary"
	manager, err := agent.NewSessionManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("FALLBACK_MODEL_ID", "at-session")
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "recovery-owner"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FALLBACK_MODEL_ID", "after-session")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, prompt := range []string{"safe probe", "next turn"} {
		if output, err := session.Run(ctx, prompt); err != nil || output != "done" {
			t.Fatalf("managed recovery: output=%q error=%v", output, err)
		}
	}
	wantModel := fallback
	if wantModel == "" {
		wantModel = "primary"
	}
	wantModels := []string{"primary", "primary", "primary", wantModel, wantModel}
	if !reflect.DeepEqual(provider.models, wantModels) {
		t.Fatalf("recovery models = %q; want %q", provider.models, wantModels)
	}
	var observed, wantEvents []agent.RecoveryEvent
	for _, record := range session.Events() {
		if event, ok := record.Event.Recovery(); ok {
			observed = append(observed, event)
		}
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt == 3 && fallback != "" {
			wantEvents = append(wantEvents, agent.RecoveryEvent{Action: agent.RecoveryFallback, Model: &fallback})
		}
		n := attempt
		wantEvents = append(wantEvents, agent.RecoveryEvent{Action: agent.RecoveryRetry, Attempt: &n, Error: "BenchmarkOverloaded"})
	}
	if !reflect.DeepEqual(observed, wantEvents) {
		t.Fatalf("recovery events = %#v; want %#v", observed, wantEvents)
	}
}

func TestFakeManagerLegacyRecoveryExplicitEnvironment(t *testing.T) {
	t.Setenv("FALLBACK_MODEL_ID", "host-legacy")
	cases := []benchmarkLegacyRecoveryCase{
		{name: "legacy-only", environment: map[string]string{"FALLBACK_MODEL_ID": "legacy"}, fallback: "legacy"},
		{name: "modern-priority", environment: map[string]string{"FALLBACK_MODEL_ID": "legacy", "MINILOOP_FALLBACK_MODEL": "modern"}, fallback: "modern"},
		{name: "legacy-missing", environment: map[string]string{}},
		{name: "legacy-empty", environment: map[string]string{"FALLBACK_MODEL_ID": ""}},
		{name: "modern-empty", environment: map[string]string{"FALLBACK_MODEL_ID": "legacy", "MINILOOP_FALLBACK_MODEL": ""}, fallback: "legacy"},
		{name: "modern-whitespace", environment: map[string]string{"FALLBACK_MODEL_ID": "legacy", "MINILOOP_FALLBACK_MODEL": " \t "}, fallback: " \t "},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			settings := fakeLegacyRecoverySettings(t, test.environment)
			managerConfig, err := fakeManagerConfig(context.Background(), settings, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("FALLBACK_MODEL_ID", "after-manager")
			assertFakeLegacyRecovery(t, managerConfig, test.fallback)
		})
	}
}

func TestFakeManagerLegacyRecoveryCapturesAmbientAtSessionConstruction(t *testing.T) {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "MINILOOP_") || strings.HasPrefix(name, "ANTHROPIC_") || name == "MODEL_ID" || name == "FALLBACK_MODEL_ID" {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("MINILOOP_FALLBACK_MODEL", "")
	t.Setenv("FALLBACK_MODEL_ID", "before-settings")
	settings := fakeLegacyRecoverySettings(t, nil)
	t.Setenv("FALLBACK_MODEL_ID", "at-manager-construction")
	managerConfig, err := fakeManagerConfig(context.Background(), settings, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FALLBACK_MODEL_ID", "after-manager-construction")
	assertFakeLegacyRecovery(t, managerConfig, "at-session")
}
