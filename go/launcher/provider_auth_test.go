package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
)

func TestLauncherAuthenticationMatchesSourceSettings(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SettingsCases []struct {
			Name        string
			Environment map[string]string
			Calls       []struct{ Headers map[string]string }
		} `json:"settings_cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.SettingsCases) != 4 {
		t.Fatal("settings auth corpus changed")
	}
	for _, row := range fixture.SettingsCases {
		t.Run(row.Name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				got := make(map[string]string)
				for _, name := range []string{"x-api-key", "authorization"} {
					if v, ok := r.Header[http.CanonicalHeaderKey(name)]; ok {
						got[name] = v[0]
					}
				}
				if !reflect.DeepEqual(got, row.Calls[0].Headers) {
					t.Errorf("authentication differs from Source: %v", got)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"fixture","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer upstream.Close()
			env := make(map[string]string)
			for k, v := range row.Environment {
				env[k] = v
			}
			env["ANTHROPIC_BASE_URL"] = upstream.URL
			env["MINILOOP_FAKE_LLM"] = "0"
			settings := settingsFor(t, env)
			app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer stopApp(t, app)
			if calls.Load() != 0 {
				t.Fatal("construction contacted provider")
			}
			report, err := json.Marshal(Inspect(settings, config.ServerSettings{}, nil))
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"env-token", "env-key", "custom-token", "custom-key"} {
				if strings.Contains(string(report)+fmt.Sprintf("%#v", settings), secret) {
					t.Fatal("configuration leaked credential")
				}
			}
			snap := settings.Snapshot()
			if snap.Settings.AuthToken.Present() || snap.Settings.ProviderHeaders.Present() {
				t.Fatal("snapshot retained live authentication")
			}
			session, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "fixture-owner"})
			if err != nil {
				t.Fatal(err)
			}
			answer, err := session.Run(context.Background(), "hello")
			if err != nil || answer != "ok" || calls.Load() != 1 {
				t.Fatal("configured auth turn failed", answer, err, calls.Load())
			}
		})
	}
}

func TestUncredentialedLauncherConstructsAndRefusesModelBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "", "ANTHROPIC_BASE_URL": server.URL})
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
	if err != nil {
		t.Fatal("missing key refused composition", err)
	}
	defer stopApp(t, app)
	if calls.Load() != 0 || app.manager.Summary().Sessions != 0 {
		t.Fatal("composition called provider or created session")
	}
	handle, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "fixture-owner"})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := handle.Run(context.Background(), "hello")
	if err != nil || !strings.HasPrefix(answer, "[Error] ") || !strings.Contains(answer, "Could not resolve authentication method") {
		t.Fatal("model refusal was not projected through the existing run boundary", answer, err)
	}
	if calls.Load() != 0 {
		t.Fatal("missing key reached HTTP", calls.Load())
	}
}
