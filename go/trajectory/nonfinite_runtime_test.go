package trajectory

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
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/provider"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestManagedNonfiniteTrajectoryMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonfinite-trajectory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name           string
			Capture        bool    `json:"capture_content"`
			Input          string  `json:"input_json"`
			Snapshots      string  `json:"snapshots_json"`
			Requests       int32   `json:"transport_requests"`
			RawSecret      bool    `json:"raw_contains_secret"`
			RecordingError *string `json:"recording_error"`
			Status         string
			LiveInput      string `json:"live_input_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 4 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/capture=%t", row.Name, row.Capture), func(t *testing.T) {
			var requests atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"message","type":"message","role":"assistant","model":"model","content":[{"type":"tool_use","id":"call","name":"bash","input":%s}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":0}}`, row.Input)
			}))
			defer upstream.Close()
			model, err := provider.New(provider.Config{APIKey: "fixture-key", BaseURL: upstream.URL})
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			store, err := New(Config{Root: filepath.Join(root, "traces"), CaptureContent: row.Capture})
			if err != nil {
				t.Fatal(err)
			}
			registry := secrets.New(secrets.Config{})
			registry.RegisterValue("DEMO", "fixture-secret")
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "ws"), Defaults: agent.SessionDefaults{Model: "model", PermissionMode: agent.ModeAuto, MaxRounds: 3}, Services: agent.ManagerServices{Provider: model, Secrets: registry, Trajectories: store}})
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
			session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = session.Run(context.Background(), "fixture-secret"); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != row.Requests || session.Info().TrajectoryRecordingError != nil || row.RecordingError != nil {
				t.Fatal(requests.Load(), session.Info().TrajectoryRecordingError, row.RecordingError)
			}
			reader, err := New(Config{Root: store.Root(), CaptureContent: row.Capture})
			if err != nil {
				t.Fatal(err)
			}
			traces, err := reader.List(agent.TrajectoryQuery{Limit: 100})
			if err != nil || len(traces) != 1 {
				t.Fatal(traces, err)
			}
			if string(traces[0].Status) != row.Status {
				t.Fatal(traces[0].Status, row.Status)
			}
			document, err := reader.JSON(traces[0].ID, 8*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonvalue.Decode(string(document))
			if err != nil {
				t.Fatal(err)
			}
			eventArray, _ := value.Lookup("events")
			events, _ := eventArray.Array()
			snapshots := []jsonvalue.Value{}
			for _, event := range events {
				kind, _ := event.Lookup("type")
				name, _ := kind.Text()
				var field string
				var observed jsonvalue.Value
				switch name {
				case "model_start":
					field = "messages"
					observed, _ = event.Lookup("model_input")
					if row.Capture {
						observed, _ = observed.Lookup("messages")
					}
				case "model_end":
					field = "output"
					var ok bool
					observed, ok = event.Lookup("model_output")
					if !ok {
						continue
					}
				case "tool_use":
					field = "input"
					observed, _ = event.Lookup("input")
				default:
					continue
				}
				snapshots = append(snapshots, jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "type", Value: kind}, {Name: field, Value: observed}}))
			}
			actual, err := jsonvalue.ArrayValue(snapshots).Sorted().MarshalLegacyUTF8()
			expected, err2 := jsonvalue.Decode(row.Snapshots)
			want, err3 := expected.Sorted().MarshalLegacyUTF8()
			if err != nil || err2 != nil || err3 != nil || string(actual) != string(want) {
				t.Fatalf("snapshots\nnative %s\nSource %s\n%v %v %v", actual, want, err, err2, err3)
			}
			var raw strings.Builder
			if err := reader.Stream(context.Background(), traces[0].ID, &raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw.String(), "fixture-secret") != row.RawSecret {
				t.Fatal("secret masking differs")
			}
			blocks, _ := session.Messages()[1].Content.Blocks()
			use, _ := blocks[0].ToolUse()
			observed, err := use.Input.Observation()
			if err != nil {
				t.Fatal(err)
			}
			live, err := observed.SortedPythonJSON()
			expected, err2 = jsonvalue.Decode(row.LiveInput)
			want, err3 = jsonvalue.AppendLegacyDefault(expected.Sorted())
			if err != nil || err2 != nil || err3 != nil || live != string(want) {
				t.Fatal("recording mutated live input", live, string(want), err, err2, err3)
			}
		})
	}
}
