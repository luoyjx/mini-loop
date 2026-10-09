package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/audit"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
)

func TestLocalAuditFullProfileUsesActualServicesAndNoDiagnosticCalls(t *testing.T) {
	settings := settingsFor(t, map[string]string{"MINILOOP_FEATURES": "1"})
	// Match Source's default trajectory content selection for the frozen CLI row.
	settings.TrajectoryEnabled = true
	settings.TrajectoryCaptureContent = true
	findings, err := AuditDeployment(context.Background(), settings, map[string]string{"MINILOOP_FAKE_LLM": "1"}, Options{DiagnosticsProvider: inspectionDiagnostics{}})
	if err != nil {
		t.Fatal(err)
	}
	text, err := audit.Render(findings)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../testdata/python-audit-local-cli.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct{ Name, Stdout string }
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range fixture.Cases {
		if row.Name == "full-features" {
			found = true
			if text+"\n" != row.Stdout {
				t.Fatal("full profile finding mismatch", text)
			}
		}
	}
	if !found {
		t.Fatal("missing full profile")
	}
	provider := &launcherDiagnostics{}
	if _, err := AuditDeployment(context.Background(), settings, map[string]string{}, Options{DiagnosticsProvider: provider}); err != nil {
		t.Fatal(err)
	}
	if provider.closed.Load() != 0 || len(provider.calls) != 0 {
		t.Fatal("audit touched caller-owned diagnostic service", provider.closed.Load(), provider.calls)
	}
}

func TestLocalAuditRetainsServingBindGuardAndCallerStoreOwnership(t *testing.T) {
	settings := settingsFor(t, nil)
	if _, err := New(context.Background(), settings, config.ServerSettings{Host: "0.0.0.0"}, nil); err == nil {
		t.Fatal("serving guard bypassed")
	}
	store := launcherState(t, filepath.Join(t.TempDir(), "state.db"))
	findings, err := AuditDeployment(context.Background(), settings, map[string]string{"HOST": "0.0.0.0"}, Options{StateStore: store})
	if err != nil {
		t.Fatal(err)
	}
	critical := false
	for _, finding := range findings {
		if finding.Check == audit.HostBind && finding.Severity == audit.Critical {
			critical = true
		}
	}
	if !critical {
		t.Fatal("local audit failed to report open bind", findings)
	}
	if _, err := store.LoadSessions(context.Background()); err != nil {
		t.Fatal("audit closed caller store", err)
	}
	// Even an already constructed app must still check an actual wildcard listener.
	if err := httpapi.RefuseOpenBind("0.0.0.0", nil); err == nil {
		t.Fatal("bind guard changed")
	}
}
