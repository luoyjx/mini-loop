package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/config"
)

type legacyRecoveryWireRequest struct {
	Model string `json:"model"`
}

type legacyRecoveryWireText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type legacyRecoveryWireUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type legacyRecoveryWireReply struct {
	ID         string                   `json:"id"`
	Type       string                   `json:"type"`
	Role       string                   `json:"role"`
	Model      string                   `json:"model"`
	Content    []legacyRecoveryWireText `json:"content"`
	StopReason string                   `json:"stop_reason"`
	Usage      legacyRecoveryWireUsage  `json:"usage"`
}

// Every request is handled here: the reserved .invalid endpoint never reaches
// the network. Three HTTP failures must reach outer recovery individually.
type legacyRecoveryTransport struct {
	models []string
}

func (transport *legacyRecoveryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != "legacy-recovery.invalid" || request.URL.Path != "/v1/messages" {
		return nil, fmt.Errorf("unexpected legacy recovery endpoint: %s", request.URL)
	}
	defer request.Body.Close()
	var wire legacyRecoveryWireRequest
	if err := json.NewDecoder(request.Body).Decode(&wire); err != nil {
		return nil, err
	}
	transport.models = append(transport.models, wire.Model)
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	header.Set("x-should-retry", "false")
	header.Set("retry-after", "0")
	status := http.StatusOK
	body := `{"type":"error","error":{"type":"overloaded_error","message":"fixture overloaded"}}`
	if len(transport.models) <= 3 {
		status = 529
	} else {
		encoded, err := json.Marshal(legacyRecoveryWireReply{
			ID: "legacy-recovery", Type: "message", Role: "assistant", Model: wire.Model,
			Content:    []legacyRecoveryWireText{{Type: "text", Text: "done"}},
			StopReason: "end_turn", Usage: legacyRecoveryWireUsage{InputTokens: 1, OutputTokens: 1},
		})
		if err != nil {
			return nil, err
		}
		body = string(encoded)
	}
	return &http.Response{StatusCode: status, Header: header,
		Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
}

func legacyRecoveryLauncherEnvironment() map[string]string {
	return map[string]string{
		"MINILOOP_FAKE_LLM": "0", "MINILOOP_TRAJECTORIES": "0", "MINILOOP_SPILL_DIR": "",
		"ANTHROPIC_API_KEY": "fixture-key", "ANTHROPIC_BASE_URL": "https://legacy-recovery.invalid",
		"MODEL_ID": "primary", "MINILOOP_MAX_TOKENS": "64",
	}
}

func assertLauncherLegacyRecovery(t *testing.T, settings config.Settings, selected string) {
	t.Helper()
	transport := &legacyRecoveryTransport{}
	app, err := NewWithOptions(context.Background(), settings,
		config.ServerSettings{Host: "127.0.0.1"}, nil, Options{ProviderTransport: transport})
	if err != nil {
		t.Fatal(err)
	}
	defer stopApp(t, app)
	if len(transport.models) != 0 {
		t.Fatal("launcher construction contacted the provider")
	}
	// Ambient legacy selection occurs when the session is constructed.
	// Explicit settings environments remain fixed through the same entry point.
	t.Setenv("FALLBACK_MODEL_ID", "after-launcher")
	session, err := app.manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "recovery-owner"})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FALLBACK_MODEL_ID", "after-session")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, prompt := range []string{"first turn", "next turn"} {
		if result, err := session.Run(ctx, prompt); err != nil || result != "done" {
			t.Fatalf("managed recovery turn: result=%q error=%v", result, err)
		}
	}
	requestModel := selected
	if requestModel == "" {
		requestModel = "primary"
	}
	wantModels := []string{"primary", "primary", "primary", requestModel, requestModel}
	if !reflect.DeepEqual(transport.models, wantModels) {
		t.Fatalf("provider models=%q, want %q", transport.models, wantModels)
	}
	var actions []agent.RecoveryAction
	var recoveryTurn agent.MessageID
	for _, record := range session.Events() {
		event, ok := record.Event.Recovery()
		if !ok {
			continue
		}
		actions = append(actions, event.Action)
		if recoveryTurn == "" {
			recoveryTurn = record.Scope.RunContext.MessageID()
		}
		if recoveryTurn == "" || record.Scope.RunContext.MessageID() != recoveryTurn ||
			record.Scope.Label != "main" || record.Scope.Depth != 0 {
			t.Fatal("recovery event lost managed turn identity", record.Scope)
		}
		if event.Action == agent.RecoveryFallback && (event.Model == nil || *event.Model != selected) {
			t.Fatal("fallback event did not carry the selected model", event)
		}
	}
	wantActions := []agent.RecoveryAction{agent.RecoveryRetry, agent.RecoveryRetry}
	if selected != "" {
		wantActions = append(wantActions, agent.RecoveryFallback)
	}
	wantActions = append(wantActions, agent.RecoveryRetry)
	if !reflect.DeepEqual(actions, wantActions) {
		t.Fatalf("managed recovery actions=%q, want %q", actions, wantActions)
	}
}

func TestLauncherLegacyRecoveryExplicitEnvironment(t *testing.T) {
	type profile struct {
		name        string
		environment map[string]string
		selected    string
	}
	for _, row := range []profile{
		{name: "legacy-only", environment: map[string]string{"FALLBACK_MODEL_ID": "legacy"}, selected: "legacy"},
		{name: "modern-priority", environment: map[string]string{"FALLBACK_MODEL_ID": "legacy", "MINILOOP_FALLBACK_MODEL": "modern"}, selected: "modern"},
		{name: "missing-legacy", environment: map[string]string{}},
		{name: "empty-legacy", environment: map[string]string{"FALLBACK_MODEL_ID": ""}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv("FALLBACK_MODEL_ID", "host-must-not-leak")
			environment := legacyRecoveryLauncherEnvironment()
			for key, value := range row.environment {
				environment[key] = value
			}
			settings := settingsFor(t, environment)
			assertLauncherLegacyRecovery(t, settings, row.selected)
		})
	}
}

func TestLauncherLegacyRecoveryAmbientConstructionTime(t *testing.T) {
	// Keep Load(nil) genuine while isolating it from a developer's configured
	// feature, credential and workspace settings. Setenv owns every restoration.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "MINILOOP_") || strings.HasPrefix(name, "ANTHROPIC_") ||
			name == "MODEL_ID" || name == "FALLBACK_MODEL_ID" {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, value := range legacyRecoveryLauncherEnvironment() {
		t.Setenv(name, value)
	}
	t.Setenv("FALLBACK_MODEL_ID", "at-settings")
	settings, err := config.Load(nil, config.LoadOptions{Directory: t.TempDir(), HomeDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if settings.FallbackModel != nil {
		t.Fatal("legacy environment leaked into the modern Settings field")
	}
	t.Setenv("FALLBACK_MODEL_ID", "at-launcher")
	assertLauncherLegacyRecovery(t, settings, "after-launcher")
}
