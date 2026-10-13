package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// cliTestResourceEnvironment keeps the caller's explicit application environment
// and inherits only the parent's Go resource controls. Parent controls replace
// every child entry for those keys, so stale or duplicate entries cannot override
// the outer budget. An empty parent value removes the child value, except that
// GOMAXPROCS retains the test suite's default of 2.
func cliTestResourceEnvironment(base []string) []string {
	environment := make([]string, 0, len(base)+4)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GOMAXPROCS", "GOFLAGS", "GOMEMLIMIT", "GOGC":
			continue
		}
		environment = append(environment, entry)
	}
	for _, key := range [...]string{"GOMAXPROCS", "GOFLAGS", "GOMEMLIMIT", "GOGC"} {
		value := os.Getenv(key)
		if key == "GOMAXPROCS" && value == "" {
			value = "2"
		}
		if value != "" {
			environment = append(environment, key+"="+value)
		}
	}
	return environment
}

// Construction is separate from starting the real reload process so its budget
// inheritance can be checked without launching the compiler or a listener.
func reloadCLIProcess(binary, module, builds string) *exec.Cmd {
	command := exec.Command(binary, "--reload-module", module)
	command.Dir = module
	command.Env = cliTestResourceEnvironment([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + builds,
		"MINILOOP_WORKSPACE_ROOT=" + filepath.Join(module, "workspaces"),
	})
	return command
}

func TestCLIProcessInheritsResourceBudget(t *testing.T) {
	budget := map[string]string{
		"GOMAXPROCS": "1",
		"GOFLAGS":    "-p=1 -trimpath",
		"GOMEMLIMIT": "256MiB",
		"GOGC":       "50",
	}
	for key, value := range budget {
		t.Setenv(key, value)
	}
	for _, key := range []string{"MODEL_ID", "MINILOOP_API_KEY", "ANTHROPIC_API_KEY", "MINILOOP_RELOAD", "MINILOOP_CLI_TEST_AMBIENT_ONLY"} {
		t.Setenv(key, "ambient-must-not-leak")
	}
	root := t.TempDir()
	for _, test := range []struct {
		name  string
		build func() *exec.Cmd
		model string
	}{
		{name: "startup", build: func() *exec.Cmd { return cliProcess(t, root) }, model: "incoming"},
		{name: "reload", build: func() *exec.Cmd {
			return reloadCLIProcess(filepath.Join(root, "miniloop"), root, filepath.Join(root, "compiler-temporaries"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := test.build()
			values := make(map[string][]string)
			for _, entry := range command.Env {
				key, value, _ := strings.Cut(entry, "=")
				values[key] = append(values[key], value)
			}
			for key, want := range budget {
				if got := values[key]; len(got) != 1 || got[0] != want {
					t.Errorf("%s = %q, want exactly [%q]", key, got, want)
				}
			}
			for _, key := range []string{"MINILOOP_API_KEY", "ANTHROPIC_API_KEY", "MINILOOP_RELOAD", "MINILOOP_CLI_TEST_AMBIENT_ONLY"} {
				if got := values[key]; len(got) != 0 {
					t.Errorf("ambient %s inherited: %q", key, got)
				}
			}
			if got := values["MODEL_ID"]; test.model == "" && len(got) != 0 || test.model != "" && (len(got) != 1 || got[0] != test.model) {
				t.Errorf("MODEL_ID = %q, want explicit child value %q only", got, test.model)
			}
		})
	}
}

func TestCLIResourceEnvironmentDefaultsAndDeduplicates(t *testing.T) {
	base := []string{
		"MODEL_ID=explicit-child",
		"GOMAXPROCS=8", "GOMAXPROCS=2",
		"GOFLAGS=-p=8", "GOFLAGS=-p=4",
		"GOMEMLIMIT=2GiB", "GOMEMLIMIT=1GiB",
		"GOGC=100", "GOGC=200",
	}
	original := append([]string(nil), base...)
	for _, test := range []struct {
		name   string
		parent [4]string
		want   []string
	}{
		{name: "empty_parent", want: []string{"MODEL_ID=explicit-child", "GOMAXPROCS=2"}},
		{
			name:   "parent_replaces_duplicates",
			parent: [4]string{"1", "-p=1 -trimpath", "256MiB", "50"},
			want:   []string{"MODEL_ID=explicit-child", "GOMAXPROCS=1", "GOFLAGS=-p=1 -trimpath", "GOMEMLIMIT=256MiB", "GOGC=50"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for index, key := range [...]string{"GOMAXPROCS", "GOFLAGS", "GOMEMLIMIT", "GOGC"} {
				t.Setenv(key, test.parent[index])
			}
			got := cliTestResourceEnvironment(base)
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("environment = %q, want %q", got, test.want)
			}
			got[0] = "MODEL_ID=mutated-result"
			if !reflect.DeepEqual(base, original) {
				t.Errorf("caller environment changed: %q", base)
			}
		})
	}
}
