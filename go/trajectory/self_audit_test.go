package trajectory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/selfaudit"
)

func TestSelfAuditObservesActualRuntimeJSONL(t *testing.T) {
	root := t.TempDir()
	store, err := New(Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "ws"), Services: agent.ManagerServices{Provider: &agent.FakeProvider{}, Trajectories: store}, Defaults: agent.SessionDefaults{PermissionMode: agent.ModeAuto, MaxRounds: 3}})
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
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "self audit actual recording"); err != nil {
		t.Fatal(err)
	}
	owner := "alice"
	scope := selfaudit.Scope{Owner: &owner}
	observed := manager.ObserveSelfAudit(context.Background(), scope)
	recordings := observed.Trajectories.BySession[string(session.ID())]
	if len(recordings) != 1 || recordings[0].EventFailure != nil || len(recordings[0].ToolUses) != 1 {
		t.Fatalf("actual JSONL observations %+v", recordings)
	}
	report := selfaudit.BuildReport(observed, scope)
	if !strings.Contains(report, "1 completed") || !strings.Contains(report, "(the 1 most recent recordings)") {
		t.Fatal(report)
	}
	if observed.Problems.Trajectories != nil {
		t.Fatal("owner observed fleet diagnostics")
	}
	fleet := manager.ObserveSelfAudit(context.Background(), selfaudit.Scope{IncludeGlobal: true})
	if fleet.Problems.Trajectories == nil {
		t.Fatal("native store does not implement diagnostic seam")
	}
}
