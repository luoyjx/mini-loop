package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/launcher"
)

func TestDumpConfigReportsDefaultsWithoutStartupOrSecrets(t *testing.T) {
	directory := t.TempDir()
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(directory, "workspaces"), "ANTHROPIC_API_KEY": "secret-provider-key", "TYPESAFE_API_KEY": "secret-typesafe-key", "MINILOOP_API_TOKEN": "secret-auth-token"}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	if strings.Contains(out.String()+errout.String(), "secret-") {
		t.Fatal("credential exposed")
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Kind != "settings-and-availability" || !report.Authenticated || len(report.Unsupported) != 0 || report.StateStore != "process-local" {
		t.Fatal(report)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("dump-config created workspace", err)
	}
}

func TestMemoryFlagsAndRootsReportSelectionWithoutStartup(t *testing.T) {
	root := t.TempDir()
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "ws"), "MINILOOP_MEMORY_ROOT": filepath.Join(root, "memory"), "MINILOOP_USER_RESOURCES_ROOT": filepath.Join(root, "users"), "MINILOOP_FEATURES": "1"}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--memory-tools", "--memory-auto=false", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.MemoryBackend != launcher.OwnerMemory || !report.MemoryTools || report.MemoryAuto || len(report.Unsupported) != 1 {
		t.Fatal(report, err)
	}
	for _, name := range []string{"ws", "memory", "users"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("dump started runtime", name, err)
		}
	}
}

func TestBackgroundFlagReportsExplicitSelectionWithoutStartup(t *testing.T) {
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "ws"), "MINILOOP_FEATURES": "1"}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--background-tools", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.BackgroundTools || len(report.Unsupported) != 1 {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("inspection started runtime")
	}
}
func TestUnavailableConfigurationsAndArgumentsFailBeforeRuntimeEffects(t *testing.T) {
	for _, row := range []struct {
		name   string
		args   []string
		env    map[string]string
		code   int
		detail string
	}{
		{"unavailable-feature", nil, map[string]string{"MINILOOP_FEATURES": "1"}, 1, "MINILOOP_FEATURES"},
		{"missing-key", nil, map[string]string{"MINILOOP_TRAJECTORIES": "0", "MINILOOP_SPILL_DIR": ""}, 1, "API key"},
		{"open-bind", nil, map[string]string{"HOST": "0.0.0.0"}, 1, "refusing to bind"},
		{"empty-host", nil, map[string]string{"HOST": ""}, 1, "HOST must not"},
		{"reload", nil, map[string]string{"MINILOOP_RELOAD": "1"}, 1, "not implemented"},
		{"unknown-flag", []string{"--guess"}, map[string]string{}, 2, "flag provided"},
		{"positional", []string{"start"}, map[string]string{}, 2, "accepts"},
		{"help", []string{"--help"}, map[string]string{}, 0, "Usage"},
	} {
		t.Run(row.name, func(t *testing.T) {
			directory := t.TempDir()
			row.env["MINILOOP_WORKSPACE_ROOT"] = filepath.Join(directory, "workspaces")
			var out, errout bytes.Buffer
			if code := execute(context.Background(), row.args, row.env, &out, &errout); code != row.code || !strings.Contains(errout.String(), row.detail) {
				t.Fatal(code, out.String(), errout.String())
			}
			if _, err := os.Stat(row.env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
				t.Fatal("failure created workspace", err)
			}
		})
	}
}

func TestCronFlagReportsSelectionWithoutStartingRuntime(t *testing.T) {
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "ws"), "MINILOOP_FEATURES": "1"}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--cron-tools", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.CronTools || report.BackgroundTools || len(report.Unsupported) != 1 {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("inspection started cron")
	}
}

func TestPlanModeFlagReportsSelectionWithoutStartingRuntime(t *testing.T) {
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "ws")}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--plan-mode-tools", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.PlanModeTools || report.CronTools || report.BackgroundTools {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("inspection started runtime")
	}
}

func TestGoalFlagReportsSelectionWithoutStartingRuntime(t *testing.T) {
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "ws")}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--goal-tools", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.GoalTools || report.CronTools || report.BackgroundTools {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("inspection started runtime")
	}
}

func TestDecisionEnvironmentDumpDoesNotStartProviders(t *testing.T) {
	for _, mode := range []string{"llm", "jev"} {
		root := filepath.Join(t.TempDir(), "workspace")
		env := map[string]string{"MINILOOP_WORKSPACE_ROOT": root, "MINILOOP_DECISIONS": mode, "TYPESAFE_API_KEY": "secret-key"}
		var out, errout bytes.Buffer
		if code := execute(context.Background(), []string{"--dump-config"}, env, &out, &errout); code != 0 {
			t.Fatal(code, errout.String())
		}
		var report launcher.Report
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || string(report.DecisionBackend) != mode || len(report.Unsupported) != 0 || strings.Contains(out.String(), "secret-key") {
			t.Fatal(out.String(), err)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("inspection created workspace", err)
		}
	}
}

func TestSelfAuditFlagReportsSelectionWithoutStartingRuntime(t *testing.T) {
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "ws")}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--self-audit-tools", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.SelfAuditTools || report.GoalTools || report.CronTools || report.BackgroundTools {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("inspection started runtime")
	}
}

func TestTeamFlagReportsSelectionWithoutStartingRuntime(t *testing.T) {
	var out, errout bytes.Buffer
	env := map[string]string{"MINILOOP_FEATURES": "1"}
	if code := execute(context.Background(), []string{"--team-tools", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.TeamTools || report.CronTools || report.BackgroundTools || len(report.Unsupported) != 1 {
		t.Fatal(report, err)
	}
	out.Reset()
	errout.Reset()
	if code := execute(context.Background(), []string{"--dump-config"}, nil, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.TeamTools {
		t.Fatal(report, err)
	}
}

func TestWorkflowSelectionDumpIsPureAndSupportsSourceEnvironment(t *testing.T) {
	for _, row := range []struct {
		args    []string
		env     map[string]string
		enabled bool
	}{
		{[]string{"--workflow-tools", "--dump-config"}, map[string]string{}, true},
		{[]string{"--dump-config"}, map[string]string{"MINILOOP_EXPERIMENTAL_WORKFLOWS": "1"}, true},
		{[]string{"--dump-config"}, map[string]string{}, false},
	} {
		root := filepath.Join(t.TempDir(), "uncreated")
		row.env["MINILOOP_WORKSPACE_ROOT"] = root
		var out, errout bytes.Buffer
		if code := execute(context.Background(), row.args, row.env, &out, &errout); code != 0 {
			t.Fatal(code, errout.String())
		}
		var report launcher.Report
		if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.WorkflowTools != row.enabled || len(report.Unsupported) != 0 {
			t.Fatal(report, err)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("inspection created workspace", err)
		}
	}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--workflow-tools"}, map[string]string{"MINILOOP_FEATURES": "1"}, &out, &errout); code != 1 || !strings.Contains(errout.String(), "MINILOOP_FEATURES") {
		t.Fatal(code, errout.String())
	}
}

func TestMCPFlagReportsSelectionWithoutStartingRuntime(t *testing.T) {
	env := map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(t.TempDir(), "ws"), "MINILOOP_FEATURES": "1"}
	var out, errout bytes.Buffer
	if code := execute(context.Background(), []string{"--mcp-tools", "--dump-config"}, env, &out, &errout); code != 0 {
		t.Fatal(code, errout.String())
	}
	var report launcher.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || !report.MCPTools || len(report.Unsupported) != 1 {
		t.Fatal(report, err)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("inspection started runtime")
	}
}
