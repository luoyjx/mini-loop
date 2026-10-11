package launcher

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/sqlitestate"
)

func launcherState(t *testing.T, path string) *sqlitestate.Store {
	t.Helper()
	store, err := sqlitestate.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func TestLauncherInjectedSQLiteExplicitRestoreAcrossHTTPRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "state.db")
	store := launcherState(t, path)
	settings := settingsFor(t, map[string]string{"MINILOOP_WORKSPACE_ROOT": filepath.Join(root, "workspaces")})
	auth, err := httpapi.AuthFromEnvironment(map[string]string{"MINILOOP_API_TOKENS": "alice:alice-token,bob:bob-token"})
	if err != nil {
		t.Fatal(err)
	}
	server := config.ServerSettings{Host: "127.0.0.1"}
	options := Options{StateStore: store}
	// Inspection reports only injection, does not assert that arbitrary stores
	// are durable, and cannot run the manager's explicit recovery policy.
	if err := store.WriteAction(ctx, agent.ActionRecord{ActionID: "started", SessionID: "audit", MessageID: "message", Status: agent.ActionStarted}); err != nil {
		t.Fatal(err)
	}
	if report := InspectWithOptions(settings, server, auth, options); report.StateStore != InjectedState {
		t.Fatal(report.StateStore)
	}
	if action, _, err := store.ReadAction(ctx, "started"); err != nil || action.Status != agent.ActionStarted {
		t.Fatal("inspection changed action state", action, err)
	}
	app, err := NewWithOptions(ctx, settings, server, auth, options)
	if err != nil {
		t.Fatal(err)
	}
	if action, _, err := store.ReadAction(ctx, "started"); err != nil || action.Status != agent.ActionUnknown {
		t.Fatal("manager recovery policy not composed", action, err)
	}
	base, cancel, done := startApp(t, app)
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	code, body := exchange(t, client, "POST", base+"/sessions", `{}`, "alice-token")
	info := decode[httpapi.SessionInfo](t, body)
	if code != 200 {
		t.Fatal(code, string(body))
	}
	endpoint := "/sessions/" + string(info.ID)
	if code, body = exchange(t, client, "POST", base+endpoint+"/messages", `{"message":"persisted HTTP turn"}`, "alice-token"); code != 200 {
		t.Fatal(code, string(body))
	}
	code, body = exchange(t, client, "GET", base+endpoint+"/transcript", "", "alice-token")
	history := decode[agent.TranscriptSnapshot](t, body)
	if code != 200 || len(history.Messages) < 2 {
		t.Fatal("HTTP did not persist native history", code, string(body))
	}
	cancel()
	stopped(t, done)
	if rows, err := store.LoadSessions(ctx); err != nil || len(rows) != 1 || rows[0].Owner != "alice" {
		t.Fatal("App shutdown closed caller store or lost owner", rows, err)
	}
	if holder, held, err := store.LeaseHolder(ctx, info.ID); err != nil || held || holder != "" {
		t.Fatal("shutdown retained lease", holder, held, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = launcherState(t, path)
	app, err = NewWithOptions(ctx, settings, server, auth, Options{StateStore: store})
	if err != nil {
		t.Fatal(err)
	}
	base, cancel, done = startApp(t, app)
	// Deliberately serve before operator restoration to verify that construction
	// and Serve do not silently turn persisted rows into published handles.
	if code, _ := exchange(t, client, "GET", base+endpoint, "", "alice-token"); code != 404 {
		t.Fatal("implicit restoration", code)
	}
	restored, err := app.RestoreSessions(ctx)
	if err != nil || len(restored) != 1 || restored[0].ID() != info.ID || restored[0].Owner() != "alice" {
		t.Fatal("operator restoration", restored, err)
	}
	if again, err := app.RestoreSessions(ctx); err != nil || len(again) != 0 {
		t.Fatal("duplicate restoration", again, err)
	}
	for _, suffix := range []string{"", "/transcript", "/events"} {
		if code, _ := exchange(t, client, "GET", base+endpoint+suffix, "", "bob-token"); code != 404 {
			t.Fatal("restoration crossed HTTP owner", suffix, code)
		}
	}
	code, body = exchange(t, client, "GET", base+endpoint+"/transcript", "", "alice-token")
	after := decode[agent.TranscriptSnapshot](t, body)
	if code != 200 || !reflect.DeepEqual(after.Messages, history.Messages) {
		t.Fatal("restart changed HTTP transcript", code, string(body))
	}
	if code, body = exchange(t, client, "POST", base+endpoint+"/messages", `{"message":"resumed HTTP turn"}`, "alice-token"); code != 200 {
		t.Fatal(code, string(body))
	}
	if state := restored[0].PersistenceStatus(); state.Error != nil || !state.LeaseConfirmed || state.RestorePending {
		t.Fatal("native HTTP writes degraded", state)
	}
	cancel()
	stopped(t, done)
	if _, err := app.RestoreSessions(ctx); !errors.Is(err, agent.ErrManagerStopped) {
		t.Fatal("stopped app restored sessions", err)
	}
	if _, err := store.LoadSessions(ctx); err != nil {
		t.Fatal("stopped app closed injected backend", err)
	}
}

func TestLauncherStateDefaultsCancellationAndStartupFailureOwnership(t *testing.T) {
	ctx := context.Background()
	settings := settingsFor(t, nil)
	server := config.ServerSettings{Host: "127.0.0.1"}
	if Inspect(settings, server, nil).StateStore != ProcessLocalState {
		t.Fatal("default backend changed")
	}
	app, err := New(ctx, settings, server, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stopApp(t, app)
	if rows, err := app.RestoreSessions(ctx); err != nil || len(rows) != 0 {
		t.Fatal("default restoration", rows, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := app.RestoreSessions(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal("restoration ignored caller cancellation", err)
	}
	store := launcherState(t, filepath.Join(t.TempDir(), "state.db"))
	settings.Model = ""
	if _, err := NewWithOptions(ctx, settings, server, nil, Options{StateStore: store}); err == nil {
		t.Fatal("invalid model admitted")
	}
	if _, err := store.LoadSessions(ctx); err != nil {
		t.Fatal("failed constructor closed caller backend", err)
	}
}
