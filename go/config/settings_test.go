package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/workspace"
)

func TestActualPythonSettings(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-configuration.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			Env      map[string]string
			Settings *Snapshot
			Error    *string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 64 {
		t.Fatalf("source cases: %d", len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			directory := t.TempDir()
			settings, err := Load(row.Env, LoadOptions{Directory: directory})
			if row.Error != nil {
				if err == nil {
					t.Fatalf("accepted Python-rejected setting: %+v", settings)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := settings.Snapshot()
			resolved, err := workspace.ResolvePath(directory)
			if err != nil {
				t.Fatal(err)
			}
			normalize := func(raw string) string {
				raw = strings.ReplaceAll(raw, resolved, "<cwd>")
				return strings.ReplaceAll(raw, filepath.Dir(resolved), "<parent>")
			}
			got.WorkspaceRoot = normalize(got.WorkspaceRoot)
			got.SkillsDir = normalize(got.SkillsDir)
			for i := range got.BindableRoots {
				got.BindableRoots[i] = normalize(got.BindableRoots[i])
			}
			for _, field := range []*string{got.SpillDir, got.UserResourcesRoot, got.MemoryRoot, got.RepoRoot, got.TrajectoryRoot} {
				if field != nil {
					*field = normalize(*field)
				}
			}
			if row.Settings == nil || !reflect.DeepEqual(got, *row.Settings) {
				actual, _ := json.Marshal(got)
				expected, _ := json.Marshal(row.Settings)
				t.Fatalf("Go: %s\nPython: %s", actual, expected)
			}
		})
	}
}

func TestLoadIsPureAndSnapshotsOwnNoCredentials(t *testing.T) {
	directory := t.TempDir()
	settings, err := Load(map[string]string{
		"ANTHROPIC_API_KEY": "secret-provider-key", "TYPESAFE_API_KEY": "secret-decision-key",
		"ANTHROPIC_BASE_URL":      "https://user:secret-url-pass@provider.invalid/v1?key=secret-query#secret-fragment",
		"MINILOOP_BINDABLE_ROOTS": "one", "MINILOOP_FALLBACK_MODEL": "backup",
	}, LoadOptions{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(directory)
	if err != nil || len(files) != 0 {
		t.Fatalf("load created files: %v %v", files, err)
	}
	snapshot := settings.Snapshot()
	if snapshot.Settings.APIKey.Present() || snapshot.Settings.TypesafeAPIKey.Present() {
		t.Fatal("snapshot owns credentials")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range []string{string(encoded), fmt.Sprintf("%v", settings), fmt.Sprintf("%#v", settings), fmt.Sprint(settings.APIKey)} {
		if strings.Contains(view, "secret-") {
			t.Fatalf("credential exposed: %s", view)
		}
	}
	*snapshot.BaseURL = "changed"
	*snapshot.FallbackModel = "changed"
	snapshot.BindableRoots[0] = "changed"
	if *settings.BaseURL == "changed" || *settings.FallbackModel == "changed" || settings.BindableRoots[0] == "changed" {
		t.Fatal("snapshot aliases source")
	}
}

func TestGoRepresentabilityBoundsAreExplicit(t *testing.T) {
	for _, raw := range []string{"nan", "inf", "1e50", "0.0000000001"} {
		if _, err := Load(map[string]string{"MINILOOP_TEAM_IDLE_POLL": raw}, LoadOptions{Directory: t.TempDir()}); err == nil {
			t.Fatalf("accepted unrepresentable duration %s", raw)
		}
	}
	settings, err := Load(map[string]string{"MINILOOP_TEAM_IDLE_POLL": "0.000000001"}, LoadOptions{Directory: t.TempDir()})
	if err != nil || settings.TeamIdlePoll.Duration() != time.Nanosecond {
		t.Fatal(settings.TeamIdlePoll, err)
	}
	if _, err := Load(map[string]string{"MINILOOP_MAX_TOKENS": "99999999999999999999999999999999"}, LoadOptions{Directory: t.TempDir()}); err == nil {
		t.Fatal("integer overflow accepted")
	}
}

func TestUnsupportedActivationAndNetworkSettings(t *testing.T) {
	defaults, err := Load(map[string]string{}, LoadOptions{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults.Unsupported()) != 0 || defaults.RequireSupported() != nil {
		t.Fatal(defaults.Unsupported())
	}
	supported, err := Load(map[string]string{"MINILOOP_TRAJECTORIES": "0", "MINILOOP_SPILL_DIR": ""}, LoadOptions{Directory: t.TempDir()})
	if err != nil || supported.RequireSupported() != nil {
		t.Fatal(err, supported.Unsupported())
	}
	for _, env := range []map[string]string{{"HOST": ""}, {"HOST": " "}, {"PORT": "-1"}, {"PORT": "65536"}, {"PORT": "8k"}, {"PORT": ""}, {"MINILOOP_RELOAD": "0"}, {"MINILOOP_FAKE_DELAY": "nan"}, {"MINILOOP_FAKE_DELAY": "0x1p0"}} {
		if _, err := LoadServer(env); err == nil {
			t.Fatalf("accepted invalid network settings %v", env)
		}
	}
	server, err := LoadServer(map[string]string{"HOST": "::1", "PORT": "０", "MINILOOP_FAKE_DELAY": "0.125"})
	if err != nil || server.Address() != "[::1]:0" || server.FakeDelay != 125*time.Millisecond {
		t.Fatal(server, err)
	}
}
