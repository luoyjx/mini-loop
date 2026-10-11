package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/cron"
	"github.com/luoyjx/mini-loop/go/httpapi"
)

func TestServeStartsRestoredDisarmedCronAndShutdownJoins(t *testing.T) {
	root := t.TempDir()
	data, e := json.Marshal([]cron.Job{{ID: "restored", Cron: "* * * * *", Prompt: "unattended", Session: "absent", Recurring: true, Durable: true}})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, ".cron.json")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	settings := settingsFor(t, map[string]string{"MINILOOP_WORKSPACE_ROOT": root})
	app, e := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, httpapi.NullAuth{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { stopApp(t, app) })
	scheduler := app.manager.CronScheduler()
	if len(scheduler.Jobs()) != 1 || scheduler.Armed("restored") || scheduler.Problems().Total != 0 {
		t.Fatal("constructor started or armed restored work")
	}
	_, cancel, done := startApp(t, app)
	deadline := time.Now().Add(5 * time.Second)
	for scheduler.Problems().Total == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Serve never started cron lifecycle")
		}
		time.Sleep(time.Millisecond)
	}
	if scheduler.Armed("restored") || scheduler.Jobs()[0].LastFired != "" {
		t.Fatal("startup consumed restored occurrence")
	}
	cancel()
	stopped(t, done)
	if e = scheduler.Wait(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal("stop removed durable schedule", e)
	}
}
