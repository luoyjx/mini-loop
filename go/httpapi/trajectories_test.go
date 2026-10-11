package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/trajectory"
)

func recordingManager(t *testing.T, root string, store agent.TrajectoryStore) *agent.SessionManager {
	t.Helper()
	m, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: root, Services: agent.ManagerServices{Provider: doneProvider{}, Trajectories: store}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Stop(context.Background()) })
	return m
}
func compareTrajectoryJSON(t *testing.T, actual, source []byte) {
	t.Helper()
	var a, b interface{}
	if json.Unmarshal(actual, &a) != nil || json.Unmarshal(source, &b) != nil {
		t.Fatal("invalid JSON")
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("actual %s\nsource %s", actual, source)
	}
}
func TestActualPythonTrajectoryHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		HTTP struct {
			Seed   string
			Routes []struct {
				Name, Path, Token string
				Status            int
				Body              json.RawMessage
				ContentType       string `json:"content_type"`
				Disposition       *string
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.HTTP.Routes) != 16 {
		t.Fatal("source route inventory changed")
	}
	root := t.TempDir()
	store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	manager := recordingManager(t, filepath.Join(root, "workspaces"), store)
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	id := agent.TrajectoryID("traj_" + strings.Repeat("c", 24))
	sourceID := string(session.ID())
	seed := strings.NewReplacer("<session>", sourceID, "<trajectory>", string(id)).Replace(fixture.HTTP.Seed)
	if err = os.WriteFile(filepath.Join(store.Root(), string(id)+".jsonl"), []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	normalize := func(data []byte) []byte {
		return bytes.ReplaceAll(bytes.ReplaceAll(data, []byte(sourceID), []byte("<session>")), []byte(id), []byte("<trajectory>"))
	}
	for _, row := range fixture.HTTP.Routes {
		t.Run(row.Name, func(t *testing.T) {
			if row.Name == "retained-after-delete" {
				if _, err = manager.Delete("alice", session.ID(), agent.DeleteSessionOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if row.Name == "retained-after-restart" {
				reopened, err := trajectory.New(trajectory.Config{Root: store.Root(), CaptureContent: true})
				if err != nil {
					t.Fatal(err)
				}
				manager = recordingManager(t, filepath.Join(root, "restart-workspaces"), reopened)
				s = testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
			}
			path := strings.NewReplacer("<session>", sourceID, "<trajectory>", string(id)).Replace(row.Path)
			response := request(s, "GET", path, "", row.Token)
			if response.Code != row.Status || response.Header().Get("Content-Type") != row.ContentType {
				t.Fatal(response.Code, response.Header(), response.Body.String())
			}
			disposition := strings.ReplaceAll(response.Header().Get("Content-Disposition"), string(id), "<trajectory>")
			if row.Disposition == nil {
				if disposition != "" {
					t.Fatal(disposition)
				}
			} else if disposition != *row.Disposition {
				t.Fatal(disposition, *row.Disposition)
			}
			body := response.Body.Bytes()
			if strings.HasPrefix(row.ContentType, "application/x-ndjson") {
				records := []json.RawMessage{}
				for _, line := range bytes.Split(bytes.TrimSpace(body), []byte("\n")) {
					records = append(records, line)
				}
				body, _ = json.Marshal(records)
			}
			compareTrajectoryJSON(t, normalize(body), row.Body)
		})
	}
}

type guardedReader struct {
	agent.TrajectoryStore
	JSONCalls, SizeCalls, StreamCalls int
	oversized                         bool
	streamError                       error
}

func (r *guardedReader) JSON(id agent.TrajectoryID, limit int64) ([]byte, error) {
	r.JSONCalls++
	return r.TrajectoryStore.JSON(id, limit)
}
func (r *guardedReader) ByteSize(id agent.TrajectoryID) (int64, error) {
	r.SizeCalls++
	if r.oversized {
		return MaxTrajectoryJSONBytes + 1, nil
	}
	return r.TrajectoryStore.ByteSize(id)
}
func (r *guardedReader) Stream(ctx context.Context, id agent.TrajectoryID, out io.Writer) error {
	r.StreamCalls++
	if r.streamError != nil {
		out.Write([]byte("partial"))
		return r.streamError
	}
	return r.TrajectoryStore.Stream(ctx, id, out)
}
func TestTrajectoryOwnershipPrecedesBodyAndJSONBound(t *testing.T) {
	root := t.TempDir()
	store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Start(agent.TrajectoryStart{Session: "s", Owner: "alice", Input: "private"})
	if err != nil {
		t.Fatal(err)
	}
	guarded := &guardedReader{TrajectoryStore: store, oversized: true}
	manager := recordingManager(t, filepath.Join(root, "ws"), guarded)
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	for _, path := range []string{"/trajectories/" + string(id), "/trajectories/" + string(id) + "/export", "/trajectories/" + string(id) + "/export?format=jsonl"} {
		if response := request(s, "GET", path, "", "token-b"); response.Code != 404 {
			t.Fatal(response.Code)
		}
	}
	if guarded.JSONCalls != 0 || guarded.SizeCalls != 0 || guarded.StreamCalls != 0 {
		t.Fatal("foreign owner touched full body")
	}
	for _, suffix := range []string{"", "/export"} {
		if response := request(s, "GET", "/trajectories/"+string(id)+suffix, "", "token-a"); response.Code != 413 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	if guarded.JSONCalls != 0 {
		t.Fatal("oversized JSON loaded")
	}
	response := request(s, "GET", "/trajectories/"+string(id)+"/export?format=jsonl", "", "token-a")
	if response.Code != 200 || guarded.StreamCalls != 1 {
		t.Fatal("streaming export refused", response.Code)
	}
	if health := decode[HealthResponse](t, request(s, "GET", "/healthz", "", "token-a").Body.Bytes()); !health.Trajectories {
		t.Fatal("health hides configured recording")
	}
	disabled := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t)})
	if response := request(disabled, "GET", "/trajectories", "", "token-a"); response.Code != 503 {
		t.Fatal("disabled store fabricated recordings")
	}
}
func TestExplicitPurgeRemovesIdleRecordings(t *testing.T) {
	root := t.TempDir()
	store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	manager := recordingManager(t, filepath.Join(root, "ws"), store)
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "record"); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.List(agent.TrajectoryQuery{Limit: 100})
	if len(rows) != 1 {
		t.Fatal("missing recording")
	}
	if _, err = manager.Delete("alice", session.ID(), agent.DeleteSessionOptions{RemoveTrajectories: true}); err != nil {
		t.Fatal(err)
	}
	rows, _ = store.List(agent.TrajectoryQuery{Limit: 100})
	if len(rows) != 0 {
		t.Fatal("explicit purge left recording")
	}
	if _, err = manager.Delete("bob", session.ID(), agent.DeleteSessionOptions{RemoveTrajectories: true}); err != agent.ErrSessionNotFound {
		t.Fatal("foreign purge was accepted")
	}
}

var _ http.Handler = (*Server)(nil)

func TestStreamFailureAbortsResponse(t *testing.T) {
	root := t.TempDir()
	store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Start(agent.TrajectoryStart{Session: "s", Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	guarded := &guardedReader{TrajectoryStore: store, streamError: errors.New("read failure")}
	server := testServer(t, Config{Manager: recordingManager(t, filepath.Join(root, "ws"), guarded), Auth: tokenAuth(t)})
	defer func() {
		if value := recover(); value != http.ErrAbortHandler {
			t.Fatalf("stream did not abort: %v", value)
		}
	}()
	request(server, "GET", "/trajectories/"+string(id)+"/export?format=jsonl", "", "token-a")
}
func TestExplicitPurgeJoinsActiveWriter(t *testing.T) {
	root := t.TempDir()
	store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	provider := agent.NewFakeProvider(agent.FakeProviderConfig{Delay: time.Hour})
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "ws"), DeleteGrace: time.Millisecond, Services: agent.ManagerServices{Provider: provider, Trajectories: store}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(context.Background()) })
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "record"); done <- err }()
	deadline := time.After(5 * time.Second)
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
		select {
		case <-deadline:
			t.Fatal("model did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := manager.Delete("bob", session.ID(), agent.DeleteSessionOptions{RemoveTrajectories: true}); err != agent.ErrSessionNotFound {
		t.Fatal("foreign purge accepted")
	}
	if _, err := manager.Delete("alice", session.ID(), agent.DeleteSessionOptions{RemoveTrajectories: true}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("active writer not joined")
	}
	rows, err := store.List(agent.TrajectoryQuery{Limit: 100})
	if err != nil || len(rows) != 0 {
		t.Fatal("recording recreated after purge", rows, err)
	}
}
