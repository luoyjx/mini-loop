package launcher

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestExplicitCronLauncherPublishesToolsAndServesOwnedJobs(t *testing.T) {
	var calls atomic.Int32
	tools := make(chan []protocol.ToolSchema, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Tools []protocol.ToolSchema }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		tools <- req.Tools
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"id":"cron","type":"message","role":"assistant","model":"configured","content":[{"type":"tool_use","id":"schedule","name":"schedule_cron","input":{"cron":"0 0 31 2 *","prompt":"launcher"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
		} else {
			io.WriteString(w, `{"id":"final","type":"message","role":"assistant","model":"configured","content":[{"type":"text","text":"scheduled"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":3}}`)
		}
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
	app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{CronTools: true})
	if err != nil {
		t.Fatal(err)
	}
	base, cancel, done := startApp(t, app)
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	code, data := exchange(t, client, "POST", base+"/sessions", `{}`, "")
	info := decode[httpapi.SessionInfo](t, data)
	if code != 200 {
		t.Fatal(code, string(data))
	}
	path := base + "/sessions/" + string(info.ID)
	if code, _ = exchange(t, client, "POST", path+"/mode", `{"mode":"auto"}`, ""); code != 200 {
		t.Fatal(code)
	}
	code, data = exchange(t, client, "POST", path+"/messages", `{"message":"schedule"}`, "")
	if answer := decode[httpapi.MessageResponse](t, data); code != 200 || answer.Final != "scheduled" {
		t.Fatal(code, string(data))
	}
	first := <-tools
	if len(first) != 13 {
		t.Fatal(len(first))
	}
	found := false
	for _, v := range first {
		if v.Name == protocol.ToolScheduleCron {
			found = true
		}
		if v.Name == "arm_cron" {
			t.Fatal("model arm installed")
		}
	}
	if !found {
		t.Fatal("cron tools absent")
	}
	code, data = exchange(t, client, "GET", path+"/cron", "", "")
	jobs := decode[httpapi.CronListResponse](t, data)
	if code != 200 || len(jobs.Jobs) != 1 || jobs.Jobs[0].Prompt != "launcher" || !jobs.Jobs[0].Armed {
		t.Fatal(code, string(data))
	}
	code, data = exchange(t, client, "DELETE", path+"/cron/"+string(jobs.Jobs[0].ID), "", "")
	if code != 200 {
		t.Fatal(code, string(data))
	}
	cancel()
	stopped(t, done)
	if app.manager.State() != agent.ManagerStopped {
		t.Fatal("cron launcher did not join manager")
	}
	if Inspect(settings, config.ServerSettings{}, nil).CronTools {
		t.Fatal("default cron tools enabled")
	}
}
