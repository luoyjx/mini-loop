package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
)

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
