package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestTranscriptSearchLauncherMatchesSourceHTTP(t *testing.T) {
	var fixture struct {
		HTTP []struct {
			Enabled bool
			Output  string
			Names   [][]protocol.ToolName
			Results []string
		}
	}
	data, err := os.ReadFile("../testdata/python-transcript-search-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.HTTP {
		t.Run(fmt.Sprint(row.Enabled), func(t *testing.T) {
			var calls atomic.Int32
			requests := make(chan launcherMCPRequest, 2)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request launcherMCPRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests <- request
				content, reason := `[{"type":"text","text":"done"}]`, "end_turn"
				if calls.Add(1) == 1 && row.Enabled {
					content, reason = `[{"type":"tool_use","id":"search","name":"transcript_search","input":{"query":"needle"}}]`, "tool_use"
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"search-reply","type":"message","role":"assistant","model":"configured","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
			}))
			defer upstream.Close()
			settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured"})
			options := Options{SessionQueryTools: row.Enabled}
			app, err := NewWithOptions(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			base, cancel, done := startApp(t, app)
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			code, body := exchange(t, client, "POST", base+"/sessions", `{"mode":"readonly"}`, "")
			if code != 200 {
				t.Fatal(code, string(body))
			}
			info := decode[httpapi.SessionInfo](t, body)
			code, body = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"search history"}`, "")
			if code != 200 || decode[httpapi.MessageResponse](t, body).Final != row.Output {
				t.Fatal(code, string(body))
			}
			names := [][]protocol.ToolName{}
			results := []string{}
			for range row.Names {
				request := <-requests
				catalog := []protocol.ToolName{}
				for _, schema := range request.Tools {
					catalog = append(catalog, schema.Name)
				}
				names = append(names, catalog)
				results = append(results, launcherMCPResults(request.Messages)...)
			}
			if !reflect.DeepEqual(names, row.Names) || !reflect.DeepEqual(results, row.Results) {
				t.Fatal(names, results)
			}
			if report := InspectWithOptions(settings, config.ServerSettings{}, nil, options); report.SessionQueryTools != row.Enabled || report.StateStore != "process-local" {
				t.Fatal(report)
			}
			cancel()
			stopped(t, done)
		})
	}
}
