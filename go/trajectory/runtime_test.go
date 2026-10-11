package trajectory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/shell"
)

type responder struct{ fail bool }

func (p responder) Respond(_ context.Context, request protocol.ModelRequest) (agent.FakeGeneration, error) {
	if p.fail {
		return agent.FakeGeneration{}, errors.New("fixture provider failure")
	}
	if _, ok := request.Messages[len(request.Messages)-1].Content.Plain(); ok {
		return agent.FakeGeneration{Content: []protocol.Block{protocol.NewBashUse("trajectory-tool", `awk 'BEGIN {for(i=0;i<6000;i++)printf "A"}'`)}, StopReason: protocol.StopToolUse}, nil
	}
	return agent.FakeGeneration{Content: []protocol.Block{protocol.NewTextBlock("done")}, StopReason: protocol.StopEndTurn}, nil
}

type failingStore struct {
	*Store
	stage string
}

func (s failingStore) Start(start agent.TrajectoryStart) (agent.TrajectoryID, error) {
	if s.stage == "start-failure" {
		return "", errors.New("fixture recording failure")
	}
	return s.Store.Start(start)
}
func (s failingStore) Append(id agent.TrajectoryID, event agent.TrajectoryRecord) error {
	if s.stage == "append-failure" {
		return errors.New("fixture recording failure")
	}
	return s.Store.Append(id, event)
}
func (s failingStore) Finish(id agent.TrajectoryID, finish agent.TrajectoryFinish) error {
	if s.stage == "finish-failure" {
		return errors.New("fixture recording failure")
	}
	return s.Store.Finish(id, finish)
}

type managedContract struct {
	Name                 string
	Final                *string
	Status               *agent.TrajectoryStatus
	Owner                *agent.OwnerID
	Count                int
	Active               bool
	RecordingError       bool                    `json:"recording_error"`
	TerminalStatus       agent.TrajectoryStatus  `json:"terminal_status"`
	TerminalPersisted    *bool                   `json:"terminal_persisted"`
	StoredTerminalStatus *agent.TrajectoryStatus `json:"stored_terminal_status"`
	RecordedTypes        []string                `json:"recorded_types"`
	LiveOutputChars      *int                    `json:"live_output_chars"`
	StoredOutputChars    *int                    `json:"stored_output_chars"`
	RequestCount         int                     `json:"request_count"`
	RequestHasFullResult bool                    `json:"request_has_full_result"`
	EphemeralRecorded    bool                    `json:"ephemeral_recorded"`
}

func TestManagedTrajectoryMatchesActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Managed []managedContract }
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Managed {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			store, err := New(Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
			if err != nil {
				t.Fatal(err)
			}
			var writer agent.TrajectoryWriter = store
			if row.Name == "disabled" {
				writer = nil
			} else if strings.HasSuffix(row.Name, "failure") {
				writer = failingStore{store, row.Name}
			}
			executor, err := shell.New(shell.Config{Workspace: filepath.Join(root, "workspace")})
			if err != nil {
				t.Fatal(err)
			}
			delay := time.Duration(0)
			if row.Name == "cancelled" {
				delay = time.Hour
			}
			provider := agent.NewFakeProvider(agent.FakeProviderConfig{Responder: responder{fail: row.Name == "provider-error"}, Delay: delay})
			session, err := agent.NewManagedSession(agent.RuntimeConfig{ID: "session-a", Owner: "alice", Workspace: executor.Workspace(), Bash: executor, Provider: provider, Mode: agent.ModeAuto, MaxRounds: 10, Model: "fixture-model", Trajectories: writer, Build: "fixture-build"})
			if err != nil {
				t.Fatal(err)
			}
			var final *string
			if row.Name == "cancelled" {
				done := make(chan error, 1)
				go func() { _, err := session.Run(context.Background(), "record this"); done <- err }()
				deadline := time.Now().Add(5 * time.Second)
				for {
					found := false
					for _, event := range session.Events() {
						if event.Event.Kind() == agent.EventModelStart {
							found = true
						}
					}
					if found {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("model not started")
					}
					time.Sleep(time.Millisecond)
				}
				if ok, err := session.Cancel(context.Background(), "stop now"); err != nil || !ok {
					t.Fatal(ok, err)
				}
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				value, err := session.Run(context.Background(), "record this")
				if err != nil {
					t.Fatal(err)
				}
				value = strings.ReplaceAll(value, "*errors.errorString: fixture provider failure", "RuntimeError: fixture provider failure")
				final = &value
			}
			info := session.Info()
			actual := managedContract{Name: row.Name, Final: final, Count: info.TrajectoryCount, Active: info.ActiveTrajectoryID != nil, RecordingError: info.TrajectoryRecordingError != nil, RecordedTypes: []string{}}
			live := session.Events()
			for _, record := range live {
				if result, ok := record.Event.ToolResult(); ok {
					n := len([]rune(result.Output))
					actual.LiveOutputChars = &n
				}
				if record.Terminal != nil {
					actual.TerminalStatus = record.Terminal.Status
					actual.TerminalPersisted = record.Terminal.Persisted
				}
			}
			rows, err := store.List(agent.TrajectoryQuery{Limit: 100})
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) > 0 {
				summary := rows[0]
				actual.Status = &summary.Status
				actual.Owner = summary.Owner
				encoded, err := store.JSON(summary.ID, 8*1024*1024)
				if err != nil {
					t.Fatal(err)
				}
				var document struct {
					Events []struct {
						Type      string
						Output    *string
						Status    *agent.TrajectoryStatus `json:"trajectory_status"`
						Ephemeral bool
						Input     *struct {
							Messages []struct{ Content json.RawMessage }
						} `json:"model_input"`
					}
				}
				if err = json.Unmarshal(encoded, &document); err != nil {
					t.Fatal(err)
				}
				for _, record := range document.Events {
					actual.RecordedTypes = append(actual.RecordedTypes, record.Type)
					actual.EphemeralRecorded = actual.EphemeralRecorded || record.Ephemeral
					if record.Status != nil {
						actual.StoredTerminalStatus = record.Status
					}
					if record.Type == "tool_result" && record.Output != nil {
						n := len([]rune(*record.Output))
						actual.StoredOutputChars = &n
					}
					if record.Input != nil {
						actual.RequestCount++
						for _, message := range record.Input.Messages {
							var blocks []struct {
								Type    string
								Content string
							}
							if len(message.Content) > 0 && message.Content[0] == '[' {
								if err := json.Unmarshal(message.Content, &blocks); err != nil {
									t.Fatal(err)
								}
							}
							for _, block := range blocks {
								if block.Type == "tool_result" && len(block.Content) == 6000 {
									actual.RequestHasFullResult = true
								}
							}
						}
					}
				}
			}
			if !reflect.DeepEqual(actual, row) {
				a, _ := json.MarshalIndent(actual, "", "  ")
				b, _ := json.MarshalIndent(row, "", "  ")
				t.Fatalf("actual %s\nsource %s", a, b)
			}
		})
	}
}
func TestRecordingMasksFullFieldsWithoutChangingRequestsAndExcludesProgress(t *testing.T) {
	root := t.TempDir()
	store, err := New(Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("DEMO", "fixture-secret")
	executor, err := shell.New(shell.Config{Workspace: filepath.Join(root, "ws")})
	if err != nil {
		t.Fatal(err)
	}
	provider := agent.NewFakeProvider(agent.FakeProviderConfig{})
	session, err := agent.NewManagedSession(agent.RuntimeConfig{ID: "s", Owner: "alice", Bash: executor, Workspace: executor.Workspace(), Provider: provider.Streaming(), Mode: agent.ModeAuto, MaxRounds: 10, Secrets: registry, Trajectories: store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "fixture-secret"); err != nil {
		t.Fatal(err)
	}
	if text, _ := session.Messages()[0].Content.Plain(); text != "fixture-secret" {
		t.Fatal("recording changed live arguments")
	}
	rows, _ := store.List(agent.TrajectoryQuery{Limit: 100})
	encoded, err := store.JSON(rows[0].ID, 8*1024*1024)
	if err != nil || strings.Contains(string(encoded), "fixture-secret") {
		t.Fatal("raw recording credential", err)
	}
	var document struct {
		Input  string
		Events []struct {
			Type      string
			Ephemeral bool
		}
	}
	if err = json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document.Input != secrets.Mask {
		t.Fatal("input mask missing", document.Input)
	}
	for _, event := range document.Events {
		if event.Ephemeral || event.Type == "assistant_delta" || event.Type == "stream_start" {
			t.Fatal("persisted ephemeral progress")
		}
	}
}

type blockingFinish struct {
	*Store
	entered, release chan struct{}
}

func (s blockingFinish) Finish(id agent.TrajectoryID, finish agent.TrajectoryFinish) error {
	close(s.entered)
	<-s.release
	return s.Store.Finish(id, finish)
}
func TestTerminalReceiptIsPublishedAfterFileFinish(t *testing.T) {
	root := t.TempDir()
	store, err := New(Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	writer := blockingFinish{store, make(chan struct{}), make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-writer.release:
		default:
			close(writer.release)
		}
	})
	executor, err := shell.New(shell.Config{Workspace: filepath.Join(root, "ws")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := agent.NewManagedSession(agent.RuntimeConfig{ID: "s", Owner: "alice", Workspace: executor.Workspace(), Bash: executor, Provider: agent.NewFakeProvider(agent.FakeProviderConfig{}), Mode: agent.ModeAuto, MaxRounds: 10, Trajectories: writer})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "hello"); done <- err }()
	select {
	case <-writer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("finish not reached")
	}
	subscription := session.Subscribe(true)
	for _, record := range session.Events() {
		if record.Terminal != nil {
			t.Fatal("terminal receipt published before finish")
		}
	}
	rows, err := store.List(agent.TrajectoryQuery{Limit: 10})
	if err != nil || len(rows) != 1 || rows[0].Status != agent.TrajectoryRunning {
		t.Fatal(rows, err)
	}
	if session.Info().ActiveTrajectoryID == nil {
		t.Fatal("active recording cleared before finish")
	}
	close(writer.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publication blocked")
	}
	found := false
	for _, record := range session.Events() {
		if record.Terminal != nil {
			found = true
			if record.Terminal.Persisted == nil || !*record.Terminal.Persisted {
				t.Fatal("missing persisted receipt")
			}
		}
	}
	if !found {
		t.Fatal("missing terminal")
	}
	subscription.Close()
	rows, err = store.List(agent.TrajectoryQuery{Limit: 10})
	if err != nil || rows[0].Status != agent.TrajectoryCompleted {
		t.Fatal(rows, err)
	}
}
