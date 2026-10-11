package agent

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

	"github.com/luoyjx/mini-loop/go/background"
	"github.com/luoyjx/mini-loop/go/provider"
)

func TestBackgroundSDKOptionsMatchSourceManagedAdmissionAndTasks(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-sdk.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Managed []struct {
			Name, Final   string
			Response      string `json:"response_json"`
			FinalResponse string `json:"final_response_json"`
			Output        string `json:"tool_output"`
			Requests      int32  `json:"transport_requests"`
			Messages      int
			Trace         []string
			Initialized   bool `json:"background_initialized"`
			Recorded      bool `json:"action_recorded"`
			Task          *struct{ Status, Result string }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Managed) != 48 {
		t.Fatal("Source managed option inventory changed")
	}
	for _, row := range fixture.Managed {
		t.Run(row.Name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if count == 1 {
					fmt.Fprint(w, row.Response)
				} else {
					fmt.Fprint(w, row.FinalResponse)
				}
			}))
			defer server.Close()
			model, err := provider.New(provider.Config{APIKey: "fixture-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			journal, err := NewInMemoryActionJournal(100)
			if err != nil {
				t.Fatal(err)
			}
			trace := []string{}
			hooks := GateHooks{
				Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
					trace = append(trace, "before")
					return KeepToolCall(), nil
				})},
				Guards: []GuardHook{guardHookFunc(func(context.Context, ToolAuthority, ToolCall) (string, bool, error) {
					trace = append(trace, "guard")
					return "", false, nil
				})},
				After: []AfterHook{afterHookFunc(func(_ context.Context, _ ToolAuthority, _ ToolCall, out string) (string, error) {
					trace = append(trace, "after")
					return out, nil
				})},
				Observers: []ResultObserver{observerFunc(func(context.Context, ToolAuthority, ToolCall, ToolOutcome) error {
					trace = append(trace, "result")
					return nil
				})},
			}
			manager, err := NewSessionManager(ManagerConfig{WorkspaceRoot: t.TempDir(), Defaults: SessionDefaults{Model: "model", PermissionMode: ModeAuto, MaxRounds: 3}, Services: ManagerServices{Provider: model, BackgroundTools: true, ActionJournal: journal, Hooks: hooks}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := manager.Stop(ctx); err != nil {
					t.Error(err)
				}
			}()
			session, err := manager.Create(context.Background(), CreateSessionRequest{Owner: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			final, err := session.Run(context.Background(), "hello")
			if err != nil {
				t.Fatal(err)
			}
			if requests.Load() != row.Requests || !reflect.DeepEqual(trace, row.Trace) || session.core.backgroundInitialized() != row.Initialized {
				t.Fatal("Source managed admission changed", requests.Load(), trace, row)
			}
			if len(session.Messages()) != row.Messages {
				t.Fatal("Source message phase changed", len(session.Messages()), row.Messages)
			}
			if final != row.Final {
				t.Fatal("Source final reply changed", final, row.Final)
			}
			uses, results := 0, 0
			for _, event := range session.Events() {
				if use, ok := event.Event.ToolUse(); ok {
					uses++
					_, exists, err := journal.Get(context.Background(), use.ActionID)
					if err != nil || exists != row.Recorded {
						t.Fatal("Source journal admission changed", exists, err)
					}
				}
				if result, ok := event.Event.ToolResult(); ok {
					results++
					if result.Output != row.Output {
						t.Fatal("Source tool output changed", result.Output, row.Output)
					}
				}
			}
			if uses != 1 || results != 1 {
				t.Fatal("Source event inventory changed", uses, results)
			}
			if row.Task != nil {
				owned, err := session.core.background.get(context.Background(), false)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := owned.Wait(ctx, background.ID("bg_0001")); err != nil {
					t.Fatal(err)
				}
				task, ok := owned.Get(background.ID("bg_0001"))
				if !ok || string(task.Status) != row.Task.Status || task.Result == nil || *task.Result != row.Task.Result {
					t.Fatal("Source managed task changed", task, row.Task)
				}
			}
		})
	}
}
