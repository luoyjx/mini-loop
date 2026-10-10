package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
)

func TestNonfiniteManagedBashMatchesSourceBatchAndJournalStages(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonfinite-bash.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Managed []struct {
			Name     string
			Response string `json:"response_json"`
			Input    string `json:"input_json"`
			Trace    []string
			Requests int32 `json:"transport_requests"`
			Uses     int   `json:"tool_uses"`
			Messages int
			Output   *string `json:"tool_output"`
			Recorded bool    `json:"action_recorded"`
			Error    *struct{ Class, Message string }
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Managed) != 24 {
		t.Fatal("Source managed inventory changed")
	}
	for _, row := range fixture.Managed {
		t.Run(row.Name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, row.Response)
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
			manager, err := NewSessionManager(ManagerConfig{WorkspaceRoot: t.TempDir(), Defaults: SessionDefaults{Model: "model", PermissionMode: ModeAuto, MaxRounds: 3}, Services: ManagerServices{Provider: model, ActionJournal: journal, Hooks: hooks}})
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
			_, err = session.Run(context.Background(), "hello")
			if row.Error != nil {
				var failure protocol.ModelBatchInputFailure
				if !errors.As(err, &failure) || failure.SourceClass() != row.Error.Class || failure.Error() != row.Error.Message {
					t.Fatal("Source batch error changed", err, row.Error)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if requests.Load() != row.Requests || len(session.Messages()) != row.Messages || !reflect.DeepEqual(trace, row.Trace) {
				t.Fatal("Source phase changed", requests.Load(), len(session.Messages()), trace, row)
			}
			uses := 0
			for _, event := range session.Events() {
				if use, ok := event.Event.ToolUse(); ok {
					uses++
					_, exists, err := journal.Get(context.Background(), use.ActionID)
					if err != nil || exists != row.Recorded {
						t.Fatal("historical input acquired an action", exists, err)
					}
				}
				if result, ok := event.Event.ToolResult(); ok && (row.Output == nil || result.Output != *row.Output) {
					t.Fatal("Source journal result changed", result.Output, row.Output)
				}
			}
			if uses != row.Uses {
				t.Fatal("Source tool-use phase changed", uses, row.Uses)
			}
		})
	}
}
