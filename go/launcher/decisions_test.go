package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type launcherDecisionProvider struct{ calls atomic.Int32 }

func (p *launcherDecisionProvider) Evaluate(_ context.Context, r decisions.Request) (decisions.Result, error) {
	p.calls.Add(1)
	return decisions.Estimate(r, "explicit", []byte(`{"q":0.8}`), nil)
}
func TestDecisionLauncherSelection(t *testing.T) {
	backend := &launcherDecisionProvider{}
	for _, r := range []struct {
		mode    string
		options Options
		want    DecisionBackend
	}{
		{"off", Options{}, DecisionOff}, {"llm", Options{}, DecisionLLM}, {"jev", Options{}, DecisionJev},
		{"off", Options{DecisionTools: true}, DecisionLLM}, {"off", Options{DecisionProvider: backend}, DecisionOff},
		{"llm", Options{DecisionProvider: backend}, DecisionCustom}, {"jev", Options{DecisionProvider: backend}, DecisionCustom},
	} {
		s := settingsFor(t, map[string]string{"MINILOOP_DECISIONS": r.mode, "TYPESAFE_API_KEY": "private-typesafe", "MINILOOP_DECISION_MODEL": "selected"})
		if err := s.RequireSupported(); err != nil {
			t.Fatal(err)
		}
		report := InspectWithOptions(s, config.ServerSettings{}, nil, r.options)
		b, e := json.Marshal(report)
		if e != nil || report.DecisionBackend != r.want || strings.Contains(string(b), "private-typesafe") {
			t.Fatal(string(b), e)
		}
		enabled, got, e := configuredDecision(s, r.options)
		if e != nil || enabled != (r.want != DecisionOff) {
			t.Fatal(r.mode, e)
		}
		if r.want == DecisionJev {
			p, ok := got.(*decisions.JevProvider)
			if !ok || p.Model() != "selected" {
				t.Fatal("model lost")
			}
		}
		if r.want == DecisionCustom && got != backend {
			t.Fatal("provider replaced")
		}
	}
	if backend.calls.Load() != 0 {
		t.Fatal("inspection performed I/O")
	}
}
func TestDecisionLauncherCompleteToolTurn(t *testing.T) {
	for _, mode := range []string{"llm", "jev"} {
		t.Run(mode, func(t *testing.T) {
			var parents, children atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Tools    []struct{ Name string }
					Messages []struct {
						Role    string
						Content json.RawMessage
					}
					System    json.RawMessage
					MaxTokens int `json:"max_tokens"`
				}
				if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
					t.Error(e)
				}
				content, reason := `[{"type":"text","text":"done"}]`, "end_turn"
				if len(req.Tools) == 0 {
					children.Add(1)
					var system string
					json.Unmarshal(req.System, &system)
					if len(req.Messages) != 1 || req.MaxTokens != 4096 || system != decisions.LLMSystem || strings.Contains(string(req.Messages[0].Content), "parent private history") {
						t.Error("isolated request changed")
					}
					content = `[{"type":"text","text":"{\"q\":0.8}"}]`
				} else {
					if len(req.Tools) != 11 {
						t.Error("catalog", len(req.Tools))
					}
					if parents.Add(1) == 1 {
						content = `[{"type":"tool_use","id":"judge","name":"decision","input":{"state":"explicit","questions":{"q":{"type":"noul","instructions":"is it working?"}}}}]`
						reason = "tool_use"
					}
				}
				fmt.Fprintf(w, `{"id":"decision","type":"message","role":"assistant","model":"served","content":%s,"stop_reason":%q,"usage":{"input_tokens":5,"output_tokens":3}}`, content, reason)
			}))
			defer upstream.Close()
			s := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture", "ANTHROPIC_BASE_URL": upstream.URL, "MINILOOP_DECISIONS": mode, "TYPESAFE_API_KEY": "offline-key"})
			options := Options{}
			custom := &launcherDecisionProvider{}
			if mode == "jev" {
				options.DecisionProvider = custom
			}
			app, e := NewWithOptions(context.Background(), s, config.ServerSettings{Host: "127.0.0.1"}, nil, options)
			if e != nil {
				t.Fatal(e)
			}
			base, cancel, done := startApp(t, app)
			defer func() { cancel(); stopped(t, done) }()
			client := &http.Client{Timeout: 5 * time.Second}
			defer client.CloseIdleConnections()
			code, b := exchange(t, client, "POST", base+"/sessions", `{"mode":"auto"}`, "")
			if code != 200 {
				t.Fatal(code, string(b))
			}
			info := decode[httpapi.SessionInfo](t, b)
			code, b = exchange(t, client, "POST", base+"/sessions/"+string(info.ID)+"/messages", `{"message":"parent private history"}`, "")
			if code != 200 || decode[httpapi.MessageResponse](t, b).Final != "done" || parents.Load() != 2 {
				t.Fatal(code, string(b))
			}
			if mode == "llm" && children.Load() != 1 {
				t.Fatal("child missing")
			}
			if mode == "jev" && (custom.calls.Load() != 1 || children.Load() != 0) {
				t.Fatal("precedence")
			}
		})
	}
}
