package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestExplicitGoalLauncherCatalogAndUntrustedHTTP(t *testing.T) {
	var count atomic.Int32
	catalog := make(chan []protocol.ToolSchema, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Tools []protocol.ToolSchema }
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
		}
		catalog <- req.Tools
		content := `[{"type":"text","text":"done"}]`
		reason := "end_turn"
		if count.Add(1) == 1 {
			content = `[{"type":"tool_use","id":"create","name":"goal_create","input":{"objective":"finish"}}]`
			reason = "tool_use"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"goal","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
	app, e := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{GoalTools: true})
	if e != nil {
		t.Fatal(e)
	}
	base, cancel, done := startApp(t, app)
	defer func() { cancel(); stopped(t, done) }()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	code, b := exchange(t, client, "POST", base+"/sessions", `{"permission_mode":"auto"}`, "")
	if code != 200 {
		t.Fatal(code, string(b))
	}
	info := decode[httpapi.SessionInfo](t, b)
	code, b = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"finish"}`, "")
	if code != 200 || decode[httpapi.MessageResponse](t, b).Final != "done" {
		t.Fatal(code, string(b))
	}
	for i := 0; i < 2; i++ {
		select {
		case tools := <-catalog:
			if len(tools) != 15 {
				t.Fatal(len(tools))
			}
		case <-time.After(time.Second):
			t.Fatal("provider request missing")
		}
	}
	code, b = exchange(t, client, "GET", base+"/sessions/"+string(info.ID)+"/goal", "", "")
	state := decode[httpapi.GoalResponse](t, b)
	if code != 200 || state.Goal != nil || state.GoalArmed || count.Load() != 2 {
		t.Fatal(code, string(b), count.Load())
	}
	if Inspect(settings, config.ServerSettings{}, nil).GoalTools {
		t.Fatal("default goal tools enabled")
	}
}
