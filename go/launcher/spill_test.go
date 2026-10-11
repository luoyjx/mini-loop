package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/spill"
)

func TestSpillConstructionMatchesActualPythonManager(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-spill.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Managers []struct {
			Name      string
			Available bool
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Managers {
		t.Run(row.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "spill")
			if row.Name == "root-is-file" {
				if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			override := map[string]string{"MINILOOP_SPILL_DIR": root}
			if row.Name == "disabled" {
				override["MINILOOP_SPILL_DIR"] = ""
			}
			settings := settingsFor(t, override)
			if err := settings.RequireSupported(); err != nil {
				t.Fatal(err)
			}
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
			if err != nil {
				t.Fatal("best-effort root stopped startup", err)
			}
			defer stopApp(t, app)
			if (app.spill != nil) != row.Available {
				t.Fatal("configured/available state differs")
			}
			if row.Available {
				info, err := os.Stat(root)
				if err != nil || info.Mode().Perm() != 0700 {
					t.Fatal("root is not private", err)
				}
				if _, err := app.spill.SaveText(context.Background(), spill.Request{Namespace: "evidence", SuggestedName: "bash.txt", Content: "already masked"}); err != nil {
					t.Fatal(err)
				}
				// Stop drains runtime handles; it must not remove preserved evidence.
				stopApp(t, app)
				paths, _ := filepath.Glob(filepath.Join(root, "session-*", "*.txt"))
				if len(paths) != 1 {
					t.Fatal("stop removed artifacts")
				}
			}
		})
	}
}
func TestInspectionDoesNotCreateConfiguredSpillRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "spill")
	settings := settingsFor(t, map[string]string{"MINILOOP_SPILL_DIR": root})
	report := Inspect(settings, config.ServerSettings{}, nil)
	for _, entry := range report.Unsupported {
		if entry.Variable == "MINILOOP_SPILL_DIR" {
			t.Fatal("implemented setting rejected")
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("inspect creates root", err)
	}
}
