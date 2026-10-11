package launcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
)

func TestDefaultTrajectoriesAreOwnedConfiguredAndRetained(t *testing.T) {
	root := t.TempDir()
	settings, err := config.Load(map[string]string{"MINILOOP_FAKE_LLM": "1", "MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces"), "MINILOOP_SPILL_DIR": filepath.Join(root, "spill")}, config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !settings.TrajectoryEnabled || len(settings.Unsupported()) != 0 {
		t.Fatal("default recording unavailable", settings.Unsupported())
	}
	_ = Inspect(settings, config.ServerSettings{}, nil)
	if _, err = os.Stat(filepath.Join(root, "workspaces")); !os.IsNotExist(err) {
		t.Fatal("inspection created recording root")
	}
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stopApp(t, app)
	session, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice", PermissionMode: agent.ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "record default"); err != nil {
		t.Fatal(err)
	}
	reader := app.manager.Trajectories()
	if reader == nil {
		t.Fatal("launcher lost recording")
	}
	rows, err := reader.List(agent.TrajectoryQuery{Limit: 100})
	if err != nil || len(rows) != 1 || rows[0].Owner == nil || *rows[0].Owner != "alice" {
		t.Fatal(rows, err)
	}
	paths, _ := filepath.Glob(filepath.Join(settings.WorkspaceRoot, ".trajectories", "*.jsonl"))
	if len(paths) != 1 {
		t.Fatal("wrong default recording root", paths)
	}
	info, err := os.Stat(paths[0])
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("recording not private", err)
	}
	stopApp(t, app)
	encoded, err := reader.JSON(rows[0].ID, 8*1024*1024)
	if err != nil || !strings.Contains(string(encoded), "model_input") {
		t.Fatal("shutdown lost recorded request", err)
	}
}
func TestConfiguredTrajectoryRootFailureIsNotSpillFallback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-directory")
	os.WriteFile(root, []byte("original"), 0600)
	settings := settingsFor(t, map[string]string{"MINILOOP_TRAJECTORIES": "1", "MINILOOP_TRAJECTORY_ROOT": root})
	if app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil); err == nil {
		stopApp(t, app)
		t.Fatal("recording root failure silently disabled recording")
	}
	raw, _ := os.ReadFile(root)
	if string(raw) != "original" {
		t.Fatal("failed construction replaced file")
	}
}
