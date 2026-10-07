package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/shell"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/trajectory"
	"github.com/luoyjx/mini-loop/go/userresources"
)

const FakeNote = "fake transport: this exercises the instrument, not the model; real runs stay in the terminal (tools/paired_benchmark.py)"

// FakeReport exposes visible rows and a second, heldout comparison. No provider
// dependency is accepted: this instrument always constructs its own fake clients.
type FakeReport struct {
	Real              bool         `json:"real"`
	Baseline          []TaskResult `json:"baseline"`
	Candidate         []TaskResult `json:"candidate"`
	Comparison        Comparison   `json:"comparison"`
	HeldoutComparison Comparison   `json:"heldout_comparison"`
	Note              string       `json:"note"`
}

// RunFakeComparison owns transient workspaces and four fresh fake clients. Every
// arm joins its manager before removal, including cancellation/error exits.
// Explicit external memory/trajectory roots retain their configured semantics.
func RunFakeComparison(ctx context.Context, settings config.Settings, delay time.Duration) (report FakeReport, err error) {
	if err = ctx.Err(); err != nil {
		return FakeReport{}, err
	}
	settings.FakeLLM = true
	settings.SpillDir = nil
	settings.WorkspaceRoot = os.TempDir()
	if err = settings.Validate(); err != nil {
		return FakeReport{}, err
	}
	if err = settings.RequireSupported(); err != nil {
		return FakeReport{}, err
	}
	root, err := os.MkdirTemp("", "ui-bench-")
	if err != nil {
		return FakeReport{}, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(root)) }()
	run := func(label, directory string, tasks []Task) ([]TaskResult, error) {
		profile := settings
		profile.WorkspaceRoot = filepath.Join(root, directory)
		manager, err := fakeManagerConfig(ctx, profile, delay)
		if err != nil {
			return nil, err
		}
		return RunArm(ctx, label, manager, tasks)
	}
	report.Note = FakeNote
	if report.Baseline, err = run("baseline", "a", DefaultTasks()); err != nil {
		return FakeReport{}, err
	}
	if report.Candidate, err = run("candidate", "b", DefaultTasks()); err != nil {
		return FakeReport{}, err
	}
	heldoutBase, err := run("baseline", "a", HeldoutTasks())
	if err != nil {
		return FakeReport{}, err
	}
	heldoutCandidate, err := run("candidate", "b", HeldoutTasks())
	if err != nil {
		return FakeReport{}, err
	}
	if report.Comparison, err = Compare(report.Baseline, report.Candidate); err != nil {
		return FakeReport{}, err
	}
	if report.HeldoutComparison, err = Compare(heldoutBase, heldoutCandidate); err != nil {
		return FakeReport{}, err
	}
	return report, nil
}

type fakeBashFactory struct{ timeout time.Duration }

func (f fakeBashFactory) BashFor(ctx context.Context, binding agent.SessionBinding) (agent.BashExecutor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return shell.New(shell.Config{Workspace: binding.Workspace, Timeout: f.timeout})
}

func fakeManagerConfig(ctx context.Context, settings config.Settings, delay time.Duration) (agent.ManagerConfig, error) {
	var catalog *skills.Catalog
	var err error
	if settings.SkillsDir == config.BuiltinSkills {
		catalog, err = skills.NewBuiltinCatalog(ctx)
	} else {
		catalog, err = skills.NewCatalog(ctx, settings.SkillsDir)
	}
	if err != nil {
		return agent.ManagerConfig{}, err
	}
	root := filepath.Join(settings.WorkspaceRoot, ".memory")
	if settings.MemoryRoot != nil {
		root = *settings.MemoryRoot
	}
	shared, err := memory.NewStore(ctx, root, nil)
	if err != nil {
		return agent.ManagerConfig{}, err
	}
	var resources *userresources.Resolver
	if settings.UserResourcesRoot != nil {
		resources, err = userresources.NewResolver(ctx, *settings.UserResourcesRoot, catalog, nil)
		if err != nil {
			return agent.ManagerConfig{}, err
		}
	}
	fallback := ""
	if settings.FallbackModel != nil {
		fallback = *settings.FallbackModel
	}
	recovery, err := agent.NewDefaultRecovery(agent.RecoveryConfig{FallbackModel: fallback})
	if err != nil {
		return agent.ManagerConfig{}, err
	}
	var traces agent.TrajectoryStore
	if settings.TrajectoryEnabled {
		root := filepath.Join(settings.WorkspaceRoot, ".trajectories")
		if settings.TrajectoryRoot != nil {
			root = *settings.TrajectoryRoot
		}
		traces, err = trajectory.New(trajectory.Config{Root: root, CaptureContent: settings.TrajectoryCaptureContent})
		if err != nil {
			return agent.ManagerConfig{}, err
		}
	}
	return agent.ManagerConfig{
		WorkspaceRoot: settings.WorkspaceRoot, BindableRoots: settings.BindableRoots,
		ModelConcurrency: agent.ConcurrencyLimit(settings.MaxConcurrentLLM), ToolConcurrency: agent.ConcurrencyLimit(settings.MaxConcurrentTools), ApprovalTimeout: settings.ApprovalTimeout.Duration(),
		Defaults: agent.SessionDefaults{Model: settings.Model, PermissionMode: agent.ModeInteractive, MaxRounds: settings.MaxTurns, MaxTokens: settings.MaxTokens, TokenThreshold: settings.TokenThreshold, SubagentMaxDepth: settings.SubagentMaxDepth, SubagentMaxRounds: settings.SubagentMaxRounds},
		Services: agent.ManagerServices{Provider: agent.NewFakeProvider(agent.FakeProviderConfig{Delay: delay}).ObjectView(), Memory: shared, UserResources: resources, Skills: catalog, Recovery: recovery, Trajectories: traces, BashFactory: fakeBashFactory{time.Duration(settings.BashTimeout) * time.Second}},
	}, nil
}
