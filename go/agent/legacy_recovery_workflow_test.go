package agent

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type legacyWorkflowCase struct {
	name     string
	modern   string
	legacy   *string
	explicit bool
	first    string
	second   string
}

func TestLegacyRecoveryWorkflowSelectsAtEachWorkerConstruction(t *testing.T) {
	fixed, empty := "fixed", ""
	for _, row := range []legacyWorkflowCase{
		{name: "ambient", first: "first", second: "second"},
		{name: "modern", modern: "modern", first: "modern", second: "modern"},
		{name: "fixed-environment", legacy: &fixed, first: "fixed", second: "fixed"},
		{name: "empty-environment", legacy: &empty, first: "primary", second: "primary"},
		{name: "explicit", explicit: true, first: "injected", second: "injected"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv("FALLBACK_MODEL_ID", "at-manager")
			var models []string
			provider := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				models = append(models, request.Model)
				if len(models) <= 3 {
					return protocol.ModelReply{}, &recoveryFixtureError{detail: protocol.ModelFailure{Kind: protocol.ModelFailureOther, Class: "Overloaded", Message: "fixture overloaded", Status: 529}}
				}
				if len(models) == 4 {
					return fakeReply([]protocol.Block{protocol.NewToolUse("artifact", protocol.ReturnArtifactToolInput(jsonvalue.TextValue("done")))}, protocol.StopToolUse), nil
				}
				return streamedReply("done"), nil
			})
			cfg := legacyRecoveryManagerConfig(t)
			cfg.Services.Provider = provider
			cfg.Services.WorkflowTools = true
			cfg.Defaults.Model = "primary"
			cfg.Services.DefaultRecoveryConfig = &RecoveryConfig{FallbackModel: row.modern, LegacyFallbackModel: row.legacy, Waiter: &recoveryTestWaiter{}, Jitter: func() float64 { return 0 }}
			if row.explicit {
				injected, err := NewDefaultRecovery(RecoveryConfig{FallbackModel: "injected", Waiter: &recoveryTestWaiter{}, Jitter: func() float64 { return 0 }})
				if err != nil {
					t.Fatal(err)
				}
				cfg.Services.Recovery = injected
				invalid := -1
				cfg.Services.DefaultRecoveryConfig.MaxRetries = &invalid
			}
			manager := makeManager(t, cfg)
			workerConfig := manager.Workflows().config.Worker
			if workerConfig.DefaultRecoveryConfig == nil {
				t.Fatal("manager did not forward the default worker configuration")
			}
			launch := workflowLaunch(t)
			workerConfig.Owner, workerConfig.Workspace = "owner", t.TempDir()
			workerConfig.ResolveContext = func(context.Context, workflows.NodeAttempt) (RunContext, error) { return launch, nil }
			runner, err := NewFreshWorkflowRunner(workerConfig)
			if err != nil {
				t.Fatal(err)
			}
			// Both retained composition layers own their pointer values.
			cfg.Services.DefaultRecoveryConfig.FallbackModel = "caller-mutated"
			workerConfig.DefaultRecoveryConfig.FallbackModel = "service-mutated"
			for i, want := range []string{row.first, row.second} {
				ambient := []string{"first", "second"}[i]
				t.Setenv("FALLBACK_MODEL_ID", ambient)
				models = nil
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				submission, err := runner.Run(ctx, workflowExecution(t, `{"type":"string"}`))
				cancel()
				if err != nil || submission == nil {
					t.Fatal("workflow recovery", submission, err)
				}
				if got, err := submission.Value().MarshalJSON(); err != nil || string(got) != `"done"` {
					t.Fatal("workflow artifact", string(got), err)
				}
				wantModels := []string{"primary", "primary", "primary", want, want}
				if !reflect.DeepEqual(models, wantModels) {
					t.Fatalf("worker models=%q; want %q", models, wantModels)
				}
			}
		})
	}
}

type legacyWorkflowFactorySnapshot struct {
	modern, legacy         string
	retries, continuations int
	escalate               bool
}

func legacyWorkflowFactoryValues(config *RecoveryConfig) (legacyWorkflowFactorySnapshot, error) {
	if config == nil || config.LegacyFallbackModel == nil || config.MaxRetries == nil ||
		config.MaxContinuations == nil || config.Escalate == nil {
		return legacyWorkflowFactorySnapshot{}, fmt.Errorf("factory lost recovery configuration fields")
	}
	return legacyWorkflowFactorySnapshot{
		modern: config.FallbackModel, legacy: *config.LegacyFallbackModel,
		retries: *config.MaxRetries, continuations: *config.MaxContinuations, escalate: *config.Escalate,
	}, nil
}

func TestLegacyRecoveryWorkflowFactoryReceivesDetachedAttemptConfig(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit=%t", explicit), func(t *testing.T) {
			legacy, retries, continuations, escalate := "fixed-legacy", 3, 2, false
			var injected Recovery
			if explicit {
				injected = &DirectRecovery{}
				retries = -1 // Unused defaults must not invalidate explicit recovery.
			}
			defaults := &RecoveryConfig{FallbackModel: "fixed-modern", LegacyFallbackModel: &legacy,
				MaxRetries: &retries, MaxContinuations: &continuations, Escalate: &escalate}
			want, err := legacyWorkflowFactoryValues(defaults)
			if err != nil {
				t.Fatal(err)
			}
			observed := make(chan legacyWorkflowFactorySnapshot, 2)
			factory := func(config WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
				if config.Recovery != injected {
					return nil, fmt.Errorf("factory lost explicit recovery identity")
				}
				before, err := legacyWorkflowFactoryValues(config.DefaultRecoveryConfig)
				if err != nil {
					return nil, err
				}
				select {
				case observed <- before:
				default:
					return nil, fmt.Errorf("unexpected extra workflow attempt")
				}
				// A public factory may tune one worker. None of these writes may
				// alter the service template supplied to the next actual attempt.
				config.DefaultRecoveryConfig.FallbackModel = "worker-modern"
				*config.DefaultRecoveryConfig.LegacyFallbackModel = "worker-legacy"
				*config.DefaultRecoveryConfig.MaxRetries = 8
				*config.DefaultRecoveryConfig.MaxContinuations = 9
				*config.DefaultRecoveryConfig.Escalate = true
				return workflows.WorkflowRunnerFunc(func(context.Context, workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
					submission := workflows.NewArtifactSubmission(jsonvalue.ObjectValue(nil), "return_artifact")
					return &submission, nil
				}), nil
			}
			journal, err := NewInMemoryActionJournal(DefaultResultsRetained)
			if err != nil {
				t.Fatal(err)
			}
			parent := WorkflowParent{Owner: "owner", Workspace: t.TempDir()}
			service, err := NewWorkflowService(WorkflowServiceConfig{
				Caps: workflows.DefaultDefinitionCaps(), Journal: journal,
				ResolveParent: func(SessionID) (WorkflowParent, bool) { return parent, true },
				Worker:        WorkflowRunnerConfig{Recovery: injected, DefaultRecoveryConfig: defaults}, WorkerFactory: factory,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := service.Close(ctx); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, action := range []ActionID{"first-attempt", "second-attempt"} {
				launched, err := service.Launch(ctx, nativeWorkflowRequest(t, action))
				if err != nil {
					t.Fatal(err)
				}
				run, err := service.Wait(ctx, launched.RunID)
				if err != nil || run.Status != workflows.RunCompleted {
					t.Fatal("actual workflow attempt failed", run.Status, err)
				}
			}
			if len(observed) != 2 {
				t.Fatalf("factory calls=%d; want two actual attempts", len(observed))
			}
			for i := 0; i < 2; i++ {
				if got := <-observed; got != want {
					t.Fatalf("attempt %d inherited another worker's configuration: %+v; want %+v", i+1, got, want)
				}
			}
			if got, err := legacyWorkflowFactoryValues(service.config.Worker.DefaultRecoveryConfig); err != nil || got != want {
				t.Fatal("factory mutated the service template", got, err)
			}
			if got, err := legacyWorkflowFactoryValues(defaults); err != nil || got != want {
				t.Fatal("factory mutated the caller configuration", got, err)
			}
		})
	}
}
