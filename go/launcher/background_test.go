package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestExplicitBackgroundLauncherServesNativeToolsAndJoinsOnShutdown(t *testing.T) {
	var calls atomic.Int32
	tools := make(chan []protocol.ToolSchema, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Tools []protocol.ToolSchema }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		tools <- request.Tools
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"id":"msg-bg","type":"message","role":"assistant","model":"configured","content":[{"type":"tool_use","id":"toolu-bg","name":"background_run","input":{"command":"touch started; sleep 30 & wait"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
		} else {
			io.WriteString(w, `{"id":"msg-final","type":"message","role":"assistant","model":"configured","content":[{"type":"text","text":"background started"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":3}}`)
		}
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
	options := Options{BackgroundTools: true}
	app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	base, cancel, done := startApp(t, app)
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	status, data := exchange(t, client, "POST", base+"/sessions", `{}`, "")
	info := decode[httpapi.SessionInfo](t, data)
	if status != 200 {
		t.Fatal(status, string(data))
	}
	path := base + "/sessions/" + string(info.ID)
	if status, _ = exchange(t, client, "POST", path+"/mode", `{"mode":"auto"}`, ""); status != 200 {
		t.Fatal(status)
	}
	status, data = exchange(t, client, "POST", path+"/messages", `{"message":"start background"}`, "")
	if answer := decode[httpapi.MessageResponse](t, data); status != 200 || answer.Final != "background started" {
		t.Fatal(status, string(data))
	}
	first := <-tools
	if len(first) != 12 {
		t.Fatal("launcher did not publish optional catalogue", len(first))
	}
	found := false
	for _, tool := range first {
		if tool.Name == protocol.ToolBackgroundRun {
			found = true
		}
	}
	if !found {
		t.Fatal("native background tool absent")
	}
	var ledger struct {
		PID *int `json:"pid"`
	}
	ledgerPath := filepath.Join(info.Workspace, ".background/bg_0001.json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(ledgerPath); err == nil {
			if err = json.Unmarshal(data, &ledger); err != nil {
				t.Fatal(err)
			}
			if ledger.PID != nil {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	if ledger.PID == nil || syscall.Kill(*ledger.PID, 0) != nil {
		t.Fatal("native task never started")
	}
	cancel()
	stopped(t, done)
	if app.manager.State() != agent.ManagerStopped {
		t.Fatal("serve did not stop manager")
	}
	if err := syscall.Kill(*ledger.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("native group leader survived shutdown", err)
	}
	if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
		t.Fatal("joined task ledger survived", err)
	}
	if _, err := os.Stat(info.Workspace); err != nil {
		t.Fatal("shutdown deleted scratch", err)
	}
}

func TestBackgroundOptionsPreserveComprehensiveFeatureRefusalAndDefault(t *testing.T) {
	settings := settingsFor(t, nil)
	server := config.ServerSettings{Host: "127.0.0.1"}
	if Inspect(settings, server, nil).BackgroundTools || !InspectWithOptions(settings, server, nil, Options{BackgroundTools: true}).BackgroundTools {
		t.Fatal("availability loses explicit activation")
	}
	settings.EnableFeatures = true
	if _, err := NewWithOptions(context.Background(), settings, server, nil, Options{BackgroundTools: true}); err == nil || !strings.Contains(err.Error(), "MINILOOP_FEATURES") {
		t.Fatal("accepted incomplete full feature bundle", err)
	}
	if _, err := os.Stat(settings.WorkspaceRoot); !os.IsNotExist(err) {
		t.Fatal("unsupported bundle created workspace")
	}
}
