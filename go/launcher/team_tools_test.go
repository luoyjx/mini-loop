package launcher

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/teams"
)

type teamHTTPView struct{ Inbox *[]struct{ Content string } }

func TestExplicitTeamLauncherExecutesModelToolsAndPreservesOwnedPeek(t *testing.T) {
	var calls atomic.Int32
	observed := make(chan []protocol.ToolSchema, 4)
	injected := make(chan string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools    []protocol.ToolSchema
			Messages []struct{ Content jsonvalue.Value }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		observed <- req.Tools
		text := ""
		for _, message := range req.Messages {
			if plain, ok := message.Content.Text(); ok {
				text += plain
			}
			if blocks, ok := message.Content.Array(); ok {
				for _, block := range blocks {
					value, _ := block.Lookup("text")
					plain, _ := value.Text()
					text += plain
				}
			}
		}
		injected <- text
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			io.WriteString(w, `{"id":"send","type":"message","role":"assistant","model":"configured","content":[{"type":"tool_use","id":"send","name":"send_message","input":{"to":"lead","content":"launcher inbox"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
		case 3:
			io.WriteString(w, `{"id":"read","type":"message","role":"assistant","model":"configured","content":[{"type":"tool_use","id":"read","name":"read_inbox","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
		default:
			io.WriteString(w, `{"id":"done","type":"message","role":"assistant","model":"configured","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":3}}`)
		}
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
	app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, Options{TeamTools: true})
	if err != nil {
		t.Fatal(err)
	}
	base, cancel, done := startApp(t, app)
	defer cancel()
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
	code, data = exchange(t, client, "POST", path+"/messages", `{"message":"send"}`, "")
	if reply := decode[httpapi.MessageResponse](t, data); code != 200 || reply.Final != "done" {
		t.Fatal(code, string(data))
	}
	schemas := <-observed
	count := 0
	for _, schema := range schemas {
		for _, expected := range protocol.TeamSchemas() {
			if expected.Name == schema.Name {
				count++
			}
		}
	}
	if count != 10 {
		t.Fatal("team catalog", count)
	}
	// A self-send is delivered at the next model round, before explicit read tools.
	<-injected
	if text := <-injected; !strings.Contains(text, "<team_inbox>") || !strings.Contains(text, "launcher inbox") {
		t.Fatal("model did not receive automatic delivery", text)
	}
	root := filepath.Join(settings.WorkspaceRoot, ".teams")
	bus := teams.New(teams.Config{Root: &root})
	if _, err := bus.Send(context.Background(), teams.SendRequest{From: teams.Key(teams.TeamID(info.ID), "peer"), To: teams.Key(teams.TeamID(info.ID), teams.Lead), Content: "retained for model"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		code, data = exchange(t, client, "GET", path+"/team", "", "")
		view := decode[teamHTTPView](t, data)
		if code != 200 || view.Inbox == nil || len(*view.Inbox) != 1 {
			t.Fatal("peek consumed", code, string(data))
		}
		if content := (*view.Inbox)[0].Content; content != "retained for model" {
			t.Fatal(content)
		}
	}
	code, data = exchange(t, client, "POST", path+"/messages", `{"message":"read"}`, "")
	if reply := decode[httpapi.MessageResponse](t, data); code != 200 || reply.Final != "done" {
		t.Fatal(code, string(data))
	}
	if text := <-injected; !strings.Contains(text, "retained for model") || !strings.Contains(text, "<team_inbox>") {
		t.Fatal("GET consumed data before model delivery", text)
	}
	code, data = exchange(t, client, "GET", path+"/team", "", "")
	if view := decode[teamHTTPView](t, data); code != 200 || view.Inbox == nil || len(*view.Inbox) != 0 {
		t.Fatal(code, string(data))
	}
	cancel()
	stopped(t, done)
	if app.manager.State() != agent.ManagerStopped {
		t.Fatal("manager not joined")
	}
	if Inspect(settings, config.ServerSettings{}, nil).TeamTools {
		t.Fatal("default activated")
	}
}
