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
	if report.Kind != "settings-and-availability" || !report.Authenticated || len(report.Unsupported) != 1 || report.StateStore != "process-local" {
		t.Fatal(report)
	}
	if _, err := os.Stat(env["MINILOOP_WORKSPACE_ROOT"]); !os.IsNotExist(err) {
		t.Fatal("dump-config created workspace", err)
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
		{"defaults", nil, map[string]string{}, 1, "MINILOOP_TRAJECTORIES"},
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
