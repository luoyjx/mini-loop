package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/teams"
)

// Full features also perform memory extraction with a tool-free model request.
// Observe agent turns here; the memory-specific contracts cover those side calls.
type notificationProvider struct{ requests []protocol.ModelRequest }

func (provider *notificationProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if len(request.Tools) == 0 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("[]")}, protocol.StopEndTurn), nil
	}
	provider.requests = append(provider.requests, request.Clone())
	return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
}

func TestManagerCustomInjectorsReplaceLifecycleDefaultsWithoutDraining(t *testing.T) {
	for _, name := range []string{"default", "empty", "custom", "full", "full-empty", "full-custom-tools"} {
		t.Run(name, func(t *testing.T) {
			provider := &notificationProvider{}
			config := managerTestConfig(t.TempDir(), provider)
			config.Services.BackgroundTools, config.Services.TeamTools = true, true
			config.EnableFeatures = strings.HasPrefix(name, "full")
			if config.EnableFeatures {
				config.Services.BackgroundTools, config.Services.TeamTools = false, false
				if name == "full-custom-tools" {
					base := createManaged(t, makeManager(t, managerTestConfig(t.TempDir(), &FakeProvider{})), CreateSessionRequest{Owner: "fixture"})
					config.Services.Tools = base.core.gate.catalog
				} else {
					config.Services.DiagnosticsProvider = inventoryDiagnostics{}
				}
			}
			defaults := name == "default" || name == "full" || name == "full-custom-tools"
			if name == "empty" || name == "full-empty" {
				config.Services.Injectors = []MessageInjector{}
			}
			if name == "custom" {
				config.Services.Injectors = []MessageInjector{namedInjector{"fixture", func(context.Context, TurnContext) ([]protocol.Message, error) {
					return []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("custom fixture")}}, nil
				}}}
			}
			manager := makeManager(t, config)
			session := createManaged(t, manager, CreateSessionRequest{Owner: "fixture"})
			if (session.core.injectors == nil) != defaults {
				t.Fatal("nil/empty selection lost during construction")
			}
			parent := session.core.subagentParent(RunContext{})
			if (parent.injectors == nil) != defaults {
				t.Fatal("nil/empty selection lost in child snapshot")
			}
			ledger := filepath.Join(session.Info().Workspace, ".background")
			if err := os.Mkdir(ledger, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ledger, "bg_0001.json"), []byte(`{"command":"orphan","pid":null}`), 0600); err != nil {
				t.Fatal(err)
			}
			key := session.core.team.Key()
			if _, err := manager.teams.Send(context.Background(), teams.SendRequest{From: key, To: key, Content: "pending fixture"}); err != nil {
				t.Fatal(err)
			}
			if _, err := session.Run(context.Background(), "turn"); err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != 1 {
				t.Fatal("model request missing")
			}
			var text strings.Builder
			for _, message := range provider.requests[0].Messages {
				if value, ok := message.Content.Plain(); ok {
					text.WriteString(value)
					text.WriteByte('\n')
				}
			}
			hasBackground := strings.Contains(text.String(), "<task_notification ")
			hasTeam := strings.Contains(text.String(), "<team_inbox>")
			if hasBackground != defaults || hasTeam != defaults {
				t.Fatalf("unexpected model injection: %s", text.String())
			}
			if defaults && strings.Index(text.String(), "<task_notification ") > strings.Index(text.String(), "<team_inbox>") {
				t.Fatal("default injector order changed")
			}
			if strings.Contains(text.String(), "custom fixture") != (name == "custom") {
				t.Fatal("custom injection lost")
			}
			mail, err := manager.teams.Peek(context.Background(), key)
			if err != nil {
				t.Fatal(err)
			}
			if defaults {
				if len(mail) != 0 || session.core.background.manager == nil {
					t.Fatal("default lifecycle notification not consumed")
				}
			} else {
				if len(mail) != 1 || session.core.background.manager != nil {
					t.Fatal("overridden lifecycle injector still consumed state")
				}
				for _, record := range session.core.Events() {
					if _, ok := record.Event.TeamInbox(); ok {
						t.Fatal("overridden team injector emitted delivery")
					}
					if _, ok := record.Event.BackgroundResult(); ok {
						t.Fatal("overridden background injector emitted delivery")
					}
				}
				// Pending observations remain available for an explicitly restored
				// default selection; overriding never acknowledges or discards them.
				session.core.injectors = nil
				if _, err := session.Run(context.Background(), "next"); err != nil {
					t.Fatal(err)
				}
				var delivered strings.Builder
				for _, message := range provider.requests[1].Messages {
					if value, ok := message.Content.Plain(); ok {
						delivered.WriteString(value)
					}
				}
				if !strings.Contains(delivered.String(), "<task_notification ") || !strings.Contains(delivered.String(), "<team_inbox>") {
					t.Fatal("override discarded pending observations")
				}
			}
		})
	}
}
