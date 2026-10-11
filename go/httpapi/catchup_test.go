package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// Test-only memory backing. Unused state seams are embedded; explicit journal
// and approval services prevent their use. No database/restart evidence claimed.
type httpCatchupStore struct {
	agent.StateStore
	mu       sync.Mutex
	rows     map[agent.SessionID]agent.SessionRecord
	messages map[agent.SessionID]map[agent.TranscriptEpoch][]protocol.Message
	events   map[agent.SessionID][]agent.SessionEventRecord
	loads    atomic.Int32
	onRead   func(context.Context) error
}

func newHTTPCatchupStore() *httpCatchupStore {
	return &httpCatchupStore{rows: map[agent.SessionID]agent.SessionRecord{}, messages: map[agent.SessionID]map[agent.TranscriptEpoch][]protocol.Message{}, events: map[agent.SessionID][]agent.SessionEventRecord{}}
}
func (s *httpCatchupStore) UpsertSession(_ context.Context, row agent.SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[row.SessionID] = row.Clone()
	return nil
}
func (s *httpCatchupStore) AppendMessages(_ context.Context, id agent.SessionID, rows []protocol.Message, epoch agent.TranscriptEpoch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.messages[id] == nil {
		s.messages[id] = map[agent.TranscriptEpoch][]protocol.Message{}
	}
	for _, m := range rows {
		s.messages[id][epoch] = append(s.messages[id][epoch], protocol.Message{Role: m.Role, Content: m.Content.Clone()})
	}
	return len(s.messages[id][epoch]), nil
}
func (s *httpCatchupStore) MessageCount(_ context.Context, id agent.SessionID, epoch *agent.TranscriptEpoch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	selected := agent.TranscriptEpoch(1)
	if epoch != nil {
		selected = *epoch
	} else {
		for value := range s.messages[id] {
			if value > selected {
				selected = value
			}
		}
	}
	return len(s.messages[id][selected]), nil
}
func (s *httpCatchupStore) AppendEvent(_ context.Context, id agent.SessionID, row agent.SessionEventRecord) (agent.EventOrdinal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[id] = append(s.events[id], row)
	return agent.EventOrdinal(len(s.events[id])), nil
}
func (s *httpCatchupStore) EventCursor(_ context.Context, id agent.SessionID) (agent.EventOrdinal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return agent.EventOrdinal(len(s.events[id])), nil
}
func (s *httpCatchupStore) LoadEvents(ctx context.Context, id agent.SessionID, after agent.EventOrdinal, limit *int) ([]agent.SessionEventRecord, error) {
	s.loads.Add(1)
	s.mu.Lock()
	hook := s.onRead
	s.mu.Unlock()
	if hook != nil {
		if err := hook(ctx); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []agent.SessionEventRecord{}
	for i, row := range s.events[id] {
		if agent.EventOrdinal(i+1) > after && (limit == nil || len(out) < *limit) {
			out = append(out, row)
		}
	}
	return out, nil
}
func (s *httpCatchupStore) AcquireLease(context.Context, agent.SessionID, agent.LeaseOwner, time.Duration) (bool, error) {
	return true, nil
}
func (s *httpCatchupStore) RenewLease(context.Context, agent.SessionID, agent.LeaseOwner, time.Duration) (bool, error) {
	return true, nil
}
func (s *httpCatchupStore) ReleaseLease(context.Context, agent.SessionID, agent.LeaseOwner) error {
	return nil
}
func catchupHTTP(t *testing.T) (*Server, *agent.ManagedSession, *httpCatchupStore) {
	t.Helper()
	store := newHTTPCatchupStore()
	journal, _ := agent.NewInMemoryActionJournal(agent.DefaultResultsRetained)
	broker, _ := agent.NewApprovalBroker(agent.ApprovalBrokerConfig{})
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(t.TempDir(), "sessions"), Services: agent.ManagerServices{Provider: doneProvider{}, StateStore: store, ActionJournal: journal, Approvals: broker}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	return testServer(t, Config{Manager: manager, Auth: tokenAuth(t), PingInterval: 5 * time.Millisecond}), session, store
}
func catchupFrames(t *testing.T, r io.Reader, end agent.EventSequence, envelope bool) []agent.EventSequence {
	t.Helper()
	scanner := bufio.NewScanner(r)
	got := []agent.EventSequence{}
	var id agent.EventSequence
	var name string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id:") {
			n, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "id:")), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			id = agent.EventSequence(n)
		}
		if strings.HasPrefix(line, "event:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
		if line == "" && id > 0 {
			if envelope && name != "agent_event" {
				t.Fatal(name)
			}
			got = append(got, id)
			if id == end {
				return got
			}
			id = 0
		}
	}
	t.Fatal("stream ended before target", end, got, scanner.Err())
	return nil
}
func TestObserveCatchupActualTCPWindowDedupAndLive(t *testing.T) {
	s, session, store := catchupHTTP(t)
	for i := 0; i < 50; i++ {
		if _, err := session.Run(context.Background(), fmt.Sprintf("turn %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	initial := append([]agent.SessionEventRecord(nil), store.events[session.ID()]...)
	store.mu.Unlock()
	if len(initial) <= 200 {
		t.Fatal("non-vacuity", len(initial))
	}
	var added []agent.SessionEventRecord
	store.onRead = func(ctx context.Context) error {
		_, err := session.Run(ctx, "during catch-up")
		store.mu.Lock()
		added = append([]agent.SessionEventRecord(nil), store.events[session.ID()][len(initial):]...)
		store.mu.Unlock()
		return err
	}
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/sessions/"+string(session.ID())+"/events?envelope=true&access_token=token-a", nil)
	req.Header.Set("Last-Event-ID", "5")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	want := []agent.EventSequence{}
	store.mu.Lock()
	for _, row := range store.events[session.ID()] {
		if row.Sequence > 5 {
			want = append(want, row.Sequence)
		}
	}
	store.mu.Unlock()
	if len(added) == 0 {
		t.Fatal("boundary did not emit")
	}
	got := catchupFrames(t, response.Body, want[len(want)-1], true)
	if !reflect.DeepEqual(got, want) {
		t.Fatal("gap/duplicate", got, want)
	}
	// After the window, live delivery must still work; queued window copies skip.
	if _, err := session.Run(context.Background(), "live after catch-up"); err != nil {
		t.Fatal(err)
	}
	records := session.Events()
	last := records[len(records)-1].Sequence
	live := catchupFrames(t, response.Body, last, true)
	if live[0] <= want[len(want)-1] {
		t.Fatal("queued duplicate", live)
	}
	cancel()
	response.Body.Close()
	wait(t, func() bool { return session.Info().Subscribers == 0 })
}
func TestObserveCatchupAdmissionFaultAndDisconnect(t *testing.T) {
	s, session, store := catchupHTTP(t)
	if _, err := session.Run(context.Background(), "seed"); err != nil {
		t.Fatal(err)
	}
	path := "/sessions/" + string(session.ID()) + "/events"
	for _, token := range []string{"", "token-b"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Last-Event-ID", "1")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 404 {
			t.Fatal(w.Code)
		}
		if store.loads.Load() != 0 {
			t.Fatal("read before owner admission")
		}
	}
	store.onRead = func(context.Context) error { return errors.New("database private credential canary") }
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer token-a")
	r.Header.Set("Last-Event-ID", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "canary") || strings.Contains(w.Header().Get("Content-Type"), "event-stream") {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	if session.Info().Subscribers != 0 || session.PersistenceStatus().Error != nil {
		t.Fatal("reader failure leaked/changed writer")
	}
	entered, finished := make(chan struct{}), make(chan struct{})
	store.onRead = func(ctx context.Context) error { close(entered); <-ctx.Done(); close(finished); return ctx.Err() }
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
	req.Header.Set("Authorization", "Bearer token-a")
	req.Header.Set("Last-Event-ID", "1")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, _ := server.Client().Do(req)
		if response != nil {
			response.Body.Close()
		}
	}()
	receive(t, entered)
	if _, err := session.Run(context.Background(), "writer during blocked read"); err != nil {
		t.Fatal(err)
	}
	cancel()
	receive(t, finished)
	receive(t, done)
	wait(t, func() bool { return session.Info().Subscribers == 0 })
}
func TestEventCursorActualPythonHeaders(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name, Header string
			IDs          []agent.EventSequence
			Count        int
			StreamError  *string `json:"stream_error"`
		}
	}
	data, err := os.ReadFile("../testdata/python-event-catchup.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, recipe := range fixture.Cases {
		if !strings.HasPrefix(recipe.Name, "header-") {
			continue
		}
		t.Run(recipe.Header, func(t *testing.T) {
			cursor := eventCursor(recipe.Header)
			start := agent.EventSequence(1)
			if cursor == 0 {
				start = 51
			}
			want := []agent.EventSequence{}
			for i := start; i <= 250; i++ {
				if i > cursor {
					want = append(want, i)
				}
			}
			if !reflect.DeepEqual(want, recipe.IDs) {
				t.Fatal(cursor, want, recipe.IDs)
			}
			if recipe.StreamError != nil && (*recipe.StreamError != "OverflowError" || cursor != agent.EventSequence(math.MaxUint64)) {
				t.Fatal("source SQL overflow boundary", recipe.StreamError, cursor)
			}
		})
	}
	for _, invalid := range []string{"+_5", "1_", "_1", "1_\u0660_", "184467440737095516160bad"} {
		if eventCursor(invalid) != 0 {
			t.Fatal(invalid)
		}
	}
	if eventCursor("\u2003+\u0661_\u0660\u2003") != 10 {
		t.Fatal("Python Unicode decimal/strip")
	}
}

func TestObserveFreshConnectionKeepsBacklogWithoutStoreRead(t *testing.T) {
	s, session, store := catchupHTTP(t)
	for i := 0; i < 50; i++ {
		if _, err := session.Run(context.Background(), fmt.Sprintf("fresh %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	store.onRead = func(context.Context) error {
		t.Error("fresh stream read durable history")
		return errors.New("unexpected read")
	}
	records := session.Events()
	if len(records) != 200 {
		t.Fatal("backlog bound", len(records))
	}
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/sessions/"+string(session.ID())+"/events", nil)
	req.Header.Set("Authorization", "Bearer token-a")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	got := catchupFrames(t, response.Body, records[len(records)-1].Sequence, false)
	want := []agent.EventSequence{}
	for _, record := range records {
		want = append(want, record.Sequence)
	}
	if !reflect.DeepEqual(got, want) || store.loads.Load() != 0 {
		t.Fatal("fresh backlog", got, want, store.loads.Load())
	}
	cancel()
	response.Body.Close()
	wait(t, func() bool { return session.Info().Subscribers == 0 })
}
