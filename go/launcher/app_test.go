package launcher

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
	"github.com/luoyjx/mini-loop/go/httpapi"
)

func settingsFor(t *testing.T, extra map[string]string) config.Settings {
	t.Helper()
	env := map[string]string{"MINILOOP_FAKE_LLM": "1", "MINILOOP_TRAJECTORIES": "0", "MINILOOP_SPILL_DIR": ""}
	for key, value := range extra {
		env[key] = value
	}
	settings, err := config.Load(env, config.LoadOptions{Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return settings
}
func stopApp(t *testing.T, app *App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := app.Stop(ctx); err != nil {
		t.Error(err)
	}
}
func startApp(t *testing.T, app *App) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, listener) }()
	t.Cleanup(func() { cancel(); stopApp(t, app) })
	return "http://" + listener.Addr().String(), cancel, done
}
func exchange(t *testing.T, client *http.Client, method, url, body, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, data
}
func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err, string(data))
	}
	return value
}
func stopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("shutdown did not join")
	}
}

func TestLoopbackServingOwnerAndConfiguredRuntime(t *testing.T) {
	settings := settingsFor(t, map[string]string{"MODEL_ID": "configured-model", "MINILOOP_MAX_CONCURRENT_LLM": "2", "MINILOOP_MAX_CONCURRENT_TOOLS": "3"})
	auth, err := httpapi.AuthFromEnvironment(map[string]string{"MINILOOP_API_TOKENS": "alice:alice-token,bob:bob-token"})
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1", Port: 0}, auth)
	if err != nil {
		t.Fatal(err)
	}
	base, cancel, done := startApp(t, app)
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	status, data := exchange(t, client, "GET", base+"/healthz", "", "alice-token")
	health := decode[httpapi.HealthResponse](t, data)
	if status != 200 || !health.Authenticated || !health.FakeLLM || health.Model != "configured-model" || health.ModelConcurrency != 2 || health.ToolConcurrency != 3 || health.Trajectories {
		t.Fatal(status, health)
	}
	status, data = exchange(t, client, "POST", base+"/sessions", `{}`, "alice-token")
	info := decode[httpapi.SessionInfo](t, data)
	if status != 200 || info.Model != "configured-model" {
		t.Fatal(status, info)
	}
	path := base + "/sessions/" + string(info.ID)
	if status, _ := exchange(t, client, "GET", path, "", "bob-token"); status != 404 {
		t.Fatal("owner crossed", status)
	}
	if status, _ := exchange(t, client, "POST", path+"/mode", `{"mode":"auto"}`, "alice-token"); status != 200 {
		t.Fatal(status)
	}
	status, data = exchange(t, client, "POST", path+"/messages", `{"message":"launcher-smoke"}`, "alice-token")
	answer := decode[httpapi.MessageResponse](t, data)
	if status != 200 || !strings.Contains(answer.Final, "handled: launcher-smoke") {
		t.Fatal(status, string(data))
	}
	cancel()
	stopped(t, done)
	if app.manager.State() != agent.ManagerStopped {
		t.Fatal(app.manager.State())
	}
	if response, err := client.Get(base + "/healthz"); err == nil {
		response.Body.Close()
		t.Fatal("listener still accepts requests")
	}
}

func TestShutdownCancelsActiveModelAndQueuedSSE(t *testing.T) {
	settings := settingsFor(t, nil)
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1", FakeDelay: time.Hour}, httpapi.NullAuth{})
	if err != nil {
		t.Fatal(err)
	}
	base, cancel, done := startApp(t, app)
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	_, data := exchange(t, client, "POST", base+"/sessions", `{}`, "")
	info := decode[httpapi.SessionInfo](t, data)
	path := base + "/sessions/" + string(info.ID)
	holder := make(chan error, 1)
	go func() {
		response, err := client.Post(path+"/messages", "application/json", strings.NewReader(`{"message":"active"}`))
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		holder <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		session, err := app.manager.Get("anonymous", info.ID)
		if err != nil {
			t.Fatal(err)
		}
		if session.Info().Busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn never active")
		}
		time.Sleep(time.Millisecond)
	}
	queued := make(chan error, 1)
	go func() {
		response, err := client.Post(path+"/messages/stream", "application/json", strings.NewReader(`{"message":"queued"}`))
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		queued <- err
	}()
	deadline = time.Now().Add(2 * time.Second)
	for {
		session, err := app.manager.Get("anonymous", info.ID)
		if err != nil {
			t.Fatal(err)
		}
		if session.Info().Subscribers > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stream never subscribed")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	stopped(t, done)
	for _, exited := range []<-chan error{holder, queued} {
		select {
		case <-exited:
		case <-time.After(4 * time.Second):
			t.Fatal("request remains active")
		}
	}
	if app.manager.State() != agent.ManagerStopped {
		t.Fatal(app.manager.State())
	}
}

func TestBindRefusalPrecedesWorkspaceEffectsAndChecksActualListener(t *testing.T) {
	settings := settingsFor(t, nil)
	if _, err := New(context.Background(), settings, config.ServerSettings{Host: "0.0.0.0"}, nil); err == nil {
		t.Fatal("accepted unauthed wildcard")
	}
	if _, err := os.Stat(settings.WorkspaceRoot); !os.IsNotExist(err) {
		t.Fatal("refusal created workspace", err)
	}
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stopApp(t, app)
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Serve(context.Background(), listener); err == nil {
		t.Fatal("actual wildcard listener accepted")
	}
	if _, err := listener.Accept(); err == nil {
		t.Fatal("rejected listener not closed")
	}
}

func TestInspectIsSideEffectFreeAndReportsUnavailableDefaults(t *testing.T) {
	directory := t.TempDir()
	settings, err := config.Load(map[string]string{"ANTHROPIC_API_KEY": "secret-provider", "TYPESAFE_API_KEY": "secret-decision", "ANTHROPIC_BASE_URL": "https://user:secret-password@provider.invalid?key=secret-query"}, config.LoadOptions{Directory: directory})
	if err != nil {
		t.Fatal(err)
	}
	report := Inspect(settings, config.ServerSettings{Host: "127.0.0.1", Port: 8000}, nil)
	data, err := json.Marshal(report)
	if err != nil || strings.Contains(string(data), "secret-") || report.Kind != "settings-and-availability" || len(report.Unsupported) != 0 || report.DotEnvDiscovery {
		t.Fatal(err, string(data))
	}
	if _, err := os.Stat(filepath.Join(directory, "workspaces")); !os.IsNotExist(err) {
		t.Fatal("inspection created workspace", err)
	}
	settings.APIKey = config.Secret{}
	if Inspect(settings, config.ServerSettings{}, nil).Provider.Credential != "<absent>" {
		t.Fatal("missing provider credential reported present")
	}
}

func TestConfiguredHTTPProviderAndBoundBashTimeout(t *testing.T) {
	type wireRequest struct {
		Model     string
		MaxTokens int `json:"max_tokens"`
		Messages  json.RawMessage
		System    json.RawMessage
	}
	captured := make(chan wireRequest, 2)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "fixture-key" {
			t.Error("credential not wired")
		}
		var request wireRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		captured <- request
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"id":"msg-tool","type":"message","role":"assistant","model":"configured","content":[{"type":"tool_use","id":"toolu-timeout","name":"bash","input":{"command":"sleep 10"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":3}}`)
		} else {
			io.WriteString(w, `{"id":"msg-final","type":"message","role":"assistant","model":"configured","content":[{"type":"text","text":"configured provider"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":3}}`)
		}
	}))
	defer upstream.Close()
	settings := settingsFor(t, map[string]string{"MINILOOP_FAKE_LLM": "0", "ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": upstream.URL, "MODEL_ID": "configured", "MINILOOP_MAX_TOKENS": "2048", "MINILOOP_BASH_TIMEOUT": "1"})
	app, err := New(context.Background(), settings, config.ServerSettings{Host: "127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stopApp(t, app)
	session, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "test", PermissionMode: agent.ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	result, err := session.Run(ctx, "config probe")
	if err != nil || result != "configured provider" || time.Since(started) > 3*time.Second {
		t.Fatal(result, err, time.Since(started))
	}
	first, second := <-captured, <-captured
	if first.Model != "configured" || first.MaxTokens != 2048 || !strings.Contains(string(first.System), "code_review") {
		t.Fatal(first)
	}
	if !strings.Contains(string(second.Messages), "Error: Timeout (1s)") {
		t.Fatal("configured timeout did not reach the real bound shell", string(second.Messages))
	}
}
