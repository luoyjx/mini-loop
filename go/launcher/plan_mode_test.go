package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestExplicitPlanModeLauncherUsesStableCatalogAndNextRequestGuidance(t *testing.T) {
	var count atomic.Int32
	requests := make(chan struct {
		Section bool
		Tools   []protocol.ToolSchema
	}, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			System []struct{ Text string }
			Tools  []protocol.ToolSchema
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		section := false
		for _, block := range req.System {
			section = section || strings.Contains(block.Text, agent.PlanSection)
		}
		requests <- struct {
			Section bool
			Tools   []protocol.ToolSchema
		}{section, req.Tools}
		w.Header().Set("Content-Type", "application/json")
		content := `[{"type":"text","text":"plan accepted"}]`
		reason := "end_turn"
		switch count.Add(1) {
		case 1:
			content = `[{"type":"tool_use","id":"enter","name":"enter_plan_mode","input":{}}]`
			reason = "tool_use"
		case 2:
			content = `[{"type":"tool_use","id":"exit","name":"exit_plan_mode","input":{"plan":"# Execute\n1. do it"}}]`
			reason = "tool_use"
		}
		fmt.Fprintf(w, `{"id":"plan","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
	app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{PlanModeTools: true})
	if err != nil {
		t.Fatal(err)
	}
	base, cancel, done := startApp(t, app)
	defer func() { cancel(); stopped(t, done) }()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	code, data := exchange(t, client, "POST", base+"/sessions", `{}`, "")
	if code != 200 {
		t.Fatal(code, string(data))
	}
	info := decode[httpapi.SessionInfo](t, data)
	code, data = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"plan and present"}`, "")
	if code != 200 || decode[httpapi.MessageResponse](t, data).Final != "plan accepted" {
		t.Fatal(code, string(data))
	}
	var tools []protocol.ToolSchema
	for i, want := range []bool{false, true, false} {
		select {
		case req := <-requests:
			if req.Section != want {
				t.Fatal(i, req.Section)
			}
			if i == 0 {
				tools = req.Tools
			} else if !reflect.DeepEqual(tools, req.Tools) {
				t.Fatal("catalog changed")
			}
		case <-time.After(time.Second):
			t.Fatal("missing request")
		}
	}
	if len(tools) != 12 {
		t.Fatal(len(tools))
	}
	for _, schema := range protocol.PlanModeSchemas() {
		found := false
		for _, actual := range tools {
			if actual.Name == schema.Name {
				found = true
			}
		}
		if !found {
			t.Fatal(schema.Name)
		}
	}
	if Inspect(settings, config.ServerSettings{}, nil).PlanModeTools {
		t.Fatal("default enabled")
	}
}
