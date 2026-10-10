package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/provider"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/sqlitestate"
)

func TestManagedNonfiniteSDKInputRemainsVisibleThroughMessageSSE(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonfinite-stream.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Store, Name string
			Events      string `json:"tool_events_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 4 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Store+"/"+row.Name, func(t *testing.T) {
			value, err := jsonvalue.Decode(row.Events)
			if err != nil {
				t.Fatal(err)
			}
			expected, _ := value.Array()
			input, _ := expected[0].Lookup("input")
			raw, err := input.MarshalLegacyUTF8()
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"message","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"tool_use","id":"call","name":"bash","input":%s}],"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}`, raw)
			}))
			defer upstream.Close()
			model, err := provider.New(provider.Config{APIKey: "fixture-key", BaseURL: upstream.URL})
			if err != nil {
				t.Fatal(err)
			}
			services := agent.ManagerServices{Provider: model}
			var store *sqlitestate.Store
			if row.Store == "sqlite" {
				store, err = sqlitestate.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				services.StateStore = store
			}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Defaults: agent.SessionDefaults{Model: "fixture-model", PermissionMode: agent.ModeAuto, MaxRounds: 3}, Services: services})
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Stop(context.Background())
			s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
			session := create(t, s, "token-a")
			response := request(s, "POST", "/sessions/"+string(session.ID)+"/messages/stream", `{"message":"hello"}`, "token-a")
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
			var observed []jsonvalue.Value
			done := false
			for _, line := range strings.Split(response.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				v, err := jsonvalue.Decode(strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
				if err != nil {
					t.Fatal(err)
				}
				kind, _ := v.Lookup("type")
				name, _ := kind.Text()
				if name == "tool_use" || name == "tool_result" {
					observed = append(observed, v)
				}
				if name == "done" {
					done = true
				}
			}
			if len(observed) != 2 || !done || requests.Load() != 1 {
				t.Fatal("stream lost tool observation or reached second transport", len(observed), done, requests.Load(), response.Body.String())
			}
			got, _ := observed[0].Lookup("input")
			a, _ := got.Sorted().MarshalLegacyUTF8()
			b, _ := input.Sorted().MarshalLegacyUTF8()
			if string(a) != string(b) {
				t.Fatal(string(a), string(b))
			}
			output, _ := observed[1].Lookup("output")
			want, _ := expected[1].Lookup("output")
			if !output.PythonEqual(want) {
				t.Fatal(output, want)
			}
			if store != nil {
				records, err := store.LoadEvents(context.Background(), session.ID, 0, nil)
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, record := range records {
					if use, ok := record.Event.ToolUse(); ok {
						count++
						if _, present, err := store.ReadAction(context.Background(), use.ActionID); err != nil || present {
							t.Fatal("refused input became action authority", present, err)
						}
					}
				}
				if count != 1 {
					t.Fatal("SQLite did not retain observation", count)
				}
			}
		})
	}
}

type nonfiniteProjectionMasker struct {
	*secrets.Registry
	fail bool
}

func (masker *nonfiniteProjectionMasker) MaskText(text string) string {
	if masker.fail {
		panic("private-token")
	}
	return masker.Registry.MaskText(text)
}

func TestNonfiniteSSEProjectionMasksDecodedKeysAndFailsClosed(t *testing.T) {
	masker := &nonfiniteProjectionMasker{Registry: secrets.New(secrets.Config{})}
	masker.RegisterValue("KEY", "private-token")
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Secrets: masker}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background())
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	record, err := agent.DecodeStoredEvent([]byte(`{"type":"tool_use","seq":1,"ts":1,"session":"s","transcript_epoch":1,"name":"bash","id":"call","input":{"command":"true","keep":NaN,"private-token":"private-token"},"display":{"verb":"run","object":"true"}}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := eventStreamJSON(s, record)
	if err != nil || strings.Contains(string(data), "private-token") || !strings.Contains(string(data), "NaN") || !strings.Contains(string(data), secrets.Mask) {
		t.Fatal(string(data), err)
	}
	original, err := record.MarshalArchiveJSON()
	if err != nil || !strings.Contains(string(original), "private-token") {
		t.Fatal("projection mutated live observation", string(original), err)
	}
	if data, err := recordingJSON(s, record); err == nil || len(data) != 0 {
		t.Fatal("SSE observation entered standard REST JSON", string(data), err)
	}
	masker.fail = true
	if data, err := eventStreamJSON(s, record); err == nil || len(data) != 0 {
		t.Fatal("failed projection returned bytes", string(data), err)
	}
}
