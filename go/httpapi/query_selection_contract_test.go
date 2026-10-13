package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/sqlitestate"
	"github.com/luoyjx/mini-loop/go/trajectory"
)

type querySelectionPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type querySelectionHeaders struct {
	ContentType        *string `json:"content-type"`
	Allow              *string `json:"allow"`
	Authenticate       *string `json:"www-authenticate"`
	Location           *string `json:"location"`
	Disposition        *string `json:"content-disposition"`
	CacheControl       *string `json:"cache-control"`
	AccelBuffering     *string `json:"x-accel-buffering"`
	CSP                *string `json:"content-security-policy"`
	ContentTypeOptions *string `json:"x-content-type-options"`
	FrameOptions       *string `json:"x-frame-options"`
	ReferrerPolicy     *string `json:"referrer-policy"`
}

type querySelectionFrame struct {
	ID    string          `json:"id"`
	Event string          `json:"event"`
	Data  jsonvalue.Value `json:"data"`
}

type querySelectionEffects struct {
	Sessions        int `json:"sessions"`
	Events          int `json:"events"`
	Subscribers     int `json:"subscribers"`
	TrajectoryFiles int `json:"trajectory_files"`
}

type querySelectionEffectPair struct {
	Before querySelectionEffects `json:"before"`
	After  querySelectionEffects `json:"after"`
}

type querySelectionCase struct {
	Name            string                    `json:"name"`
	Group           string                    `json:"group"`
	Method          string                    `json:"method"`
	Path            string                    `json:"path"`
	Query           []querySelectionPair      `json:"query"`
	RawQueryHex     *string                   `json:"raw_query_hex"`
	SourceFault     *queryContractSourceFault `json:"source_fault"`
	Authorization   *string                   `json:"authorization"`
	RequestHeaders  []querySelectionPair      `json:"request_headers"`
	Status          int                       `json:"status"`
	ResponseHeaders querySelectionHeaders     `json:"response_headers"`
	BodyKind        string                    `json:"body_kind"`
	BodyJSON        jsonvalue.Value           `json:"body_json"`
	BodyText        *string                   `json:"body_text"`
	Events          []querySelectionFrame     `json:"events"`
	Effects         querySelectionEffectPair  `json:"effects"`
}

type querySelectionSessionSeed struct {
	Key       string        `json:"key"`
	ID        string        `json:"id"`
	Owner     agent.OwnerID `json:"owner"`
	CreatedAt float64       `json:"created_at"`
}

type querySelectionTrajectorySeed struct {
	Key      string        `json:"key"`
	ID       string        `json:"id"`
	Session  string        `json:"session"`
	Owner    agent.OwnerID `json:"owner"`
	RunIndex int           `json:"run_index"`
	RawJSONL string        `json:"raw_jsonl"`
}

type querySelectionEpochSeed struct {
	Epoch    agent.TranscriptEpoch `json:"epoch"`
	Messages []protocol.Message    `json:"messages"`
}

type querySelectionEventSeed struct {
	Session string          `json:"session"`
	Event   jsonvalue.Value `json:"event"`
}

type querySelectionSeeds struct {
	Sessions         []querySelectionSessionSeed    `json:"sessions"`
	Trajectories     []querySelectionTrajectorySeed `json:"trajectories"`
	TranscriptEpochs []querySelectionEpochSeed      `json:"transcript_epochs"`
	Events           []querySelectionEventSeed      `json:"events"`
}

type querySelectionFixture struct {
	Seeds querySelectionSeeds  `json:"seeds"`
	Cases []querySelectionCase `json:"cases"`
}

type querySelectionIdentity struct {
	Key       string
	Actual    string
	Workspace string
	CreatedAt float64
}

type querySelectionTrajectoryReads struct {
	JSON, Size, Stream int64
}

type querySelectionTrajectoryStore struct {
	*trajectory.Store
	jsonCalls, sizeCalls, streamCalls atomic.Int64
}

func (s *querySelectionTrajectoryStore) JSON(id agent.TrajectoryID, limit int64) ([]byte, error) {
	s.jsonCalls.Add(1)
	return s.Store.JSON(id, limit)
}

func (s *querySelectionTrajectoryStore) ByteSize(id agent.TrajectoryID) (int64, error) {
	s.sizeCalls.Add(1)
	return s.Store.ByteSize(id)
}

func (s *querySelectionTrajectoryStore) Stream(ctx context.Context, id agent.TrajectoryID, writer io.Writer) error {
	s.streamCalls.Add(1)
	return s.Store.Stream(ctx, id, writer)
}

func (s *querySelectionTrajectoryStore) reads() querySelectionTrajectoryReads {
	return querySelectionTrajectoryReads{s.jsonCalls.Load(), s.sizeCalls.Load(), s.streamCalls.Load()}
}

func querySelectionResolve(text string, identities []querySelectionIdentity) string {
	for _, identity := range identities {
		text = strings.ReplaceAll(text, "{"+identity.Key+"}", identity.Actual)
	}
	return text
}

func querySelectionNormalizeText(text string, identities []querySelectionIdentity) string {
	for _, identity := range identities {
		if identity.Workspace != "" {
			text = strings.ReplaceAll(text, identity.Workspace, "<workspace>/sessions/"+identity.Key)
		}
		text = strings.ReplaceAll(text, identity.Actual, identity.Key)
	}
	return text
}

func querySelectionNormalize(value jsonvalue.Value, identities []querySelectionIdentity) jsonvalue.Value {
	value = value.MapStrings(func(text string) string { return querySelectionNormalizeText(text, identities) })
	var normalizeClock func(jsonvalue.Value) jsonvalue.Value
	normalizeClock = func(value jsonvalue.Value) jsonvalue.Value {
		if items, ok := value.Array(); ok {
			for index := range items {
				items[index] = normalizeClock(items[index])
			}
			return jsonvalue.ArrayValue(items)
		}
		if value.Kind() != jsonvalue.Object {
			return value
		}
		id, _ := value.Lookup("id")
		key, _ := id.Text()
		fields := make([]jsonvalue.Field, 0, len(value.Keys()))
		for _, name := range value.Keys() {
			field, _ := value.Lookup(name)
			if name == "created_at" {
				for _, identity := range identities {
					if identity.Key == key && identity.Workspace != "" {
						field = jsonvalue.FloatValue(identity.CreatedAt)
					}
				}
			}
			fields = append(fields, jsonvalue.Field{Name: name, Value: normalizeClock(field)})
		}
		return jsonvalue.ObjectValue(fields)
	}
	return normalizeClock(value)
}

// Numeric JSON spelling may differ (1100 versus 1100.0), but booleans, text,
// null, object membership and array order remain distinct assertions.
func querySelectionEqual(left, right jsonvalue.Value) bool {
	leftNumber := left.Kind() == jsonvalue.Integer || left.Kind() == jsonvalue.Float
	rightNumber := right.Kind() == jsonvalue.Integer || right.Kind() == jsonvalue.Float
	if leftNumber && rightNumber {
		comparison, ordered, err := left.PythonNumberCompare(right)
		return err == nil && ordered && comparison == 0
	}
	if left.Kind() != right.Kind() {
		return false
	}
	switch left.Kind() {
	case jsonvalue.Object:
		if len(left.Keys()) != len(right.Keys()) {
			return false
		}
		for _, key := range left.Keys() {
			value, _ := left.Lookup(key)
			other, present := right.Lookup(key)
			if !present || !querySelectionEqual(value, other) {
				return false
			}
		}
		return true
	case jsonvalue.Array:
		items, _ := left.Array()
		others, _ := right.Array()
		if len(items) != len(others) {
			return false
		}
		for index := range items {
			if !querySelectionEqual(items[index], others[index]) {
				return false
			}
		}
		return true
	default:
		return left.PythonEqual(right)
	}
}

func querySelectionAssertJSON(t *testing.T, got, want jsonvalue.Value) {
	t.Helper()
	if !querySelectionEqual(got, want) {
		actual, _ := got.MarshalUTF8()
		expected, _ := want.MarshalUTF8()
		t.Fatalf("response JSON = %s; Source = %s", actual, expected)
	}
}

func querySelectionDecode(t *testing.T, data []byte) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func querySelectionReadHeaders(headers http.Header, identities []querySelectionIdentity) querySelectionHeaders {
	read := func(name string) *string {
		if len(headers.Values(name)) == 0 {
			return nil
		}
		value := querySelectionNormalizeText(headers.Get(name), identities)
		return &value
	}
	return querySelectionHeaders{
		ContentType: read("Content-Type"), Allow: read("Allow"), Authenticate: read("WWW-Authenticate"),
		Location: read("Location"), Disposition: read("Content-Disposition"), CacheControl: read("Cache-Control"),
		AccelBuffering: read("X-Accel-Buffering"), CSP: read("Content-Security-Policy"),
		ContentTypeOptions: read("X-Content-Type-Options"), FrameOptions: read("X-Frame-Options"), ReferrerPolicy: read("Referrer-Policy"),
	}
}

func querySelectionReadFrame(t *testing.T, reader io.Reader) querySelectionFrame {
	t.Helper()
	scanner := bufio.NewScanner(reader)
	var frame querySelectionFrame
	dataSeen := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" && dataSeen {
			return frame
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "id":
			frame.ID = value
		case "event":
			frame.Event = value
		case "data":
			if dataSeen {
				t.Fatal("unexpected multiline SSE data")
			}
			frame.Data = querySelectionDecode(t, []byte(value))
			dataSeen = true
		}
	}
	t.Fatalf("SSE ended before a complete frame: %v", scanner.Err())
	return frame
}

// Only wire identity fields are rebound; fixed fixture text such as the trace's
// input prompt is retained even when it happens to equal a stable identity key.
func querySelectionBindSeed(value jsonvalue.Value, identities []querySelectionIdentity) jsonvalue.Value {
	if items, ok := value.Array(); ok {
		for index := range items {
			items[index] = querySelectionBindSeed(items[index], identities)
		}
		return jsonvalue.ArrayValue(items)
	}
	if value.Kind() != jsonvalue.Object {
		return value
	}
	fields := make([]jsonvalue.Field, 0, len(value.Keys()))
	for _, name := range value.Keys() {
		field, _ := value.Lookup(name)
		switch name {
		case "session", "group_id", "trajectory_id", "trace_id":
			text, ok := field.Text()
			if ok {
				for _, identity := range identities {
					if text == identity.Key {
						field = jsonvalue.TextValue(identity.Actual)
					}
				}
			}
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: querySelectionBindSeed(field, identities)})
	}
	return jsonvalue.ObjectValue(fields)
}

func querySelectionSnapshot(t *testing.T, manager *agent.SessionManager, sessions []*agent.ManagedSession, state *sqlitestate.Store, store *querySelectionTrajectoryStore) querySelectionEffects {
	t.Helper()
	effects := querySelectionEffects{Sessions: len(manager.List("alice")) + len(manager.List("bob"))}
	for _, session := range sessions {
		cursor, err := state.EventCursor(context.Background(), session.ID())
		if err != nil {
			t.Fatal(err)
		}
		effects.Events += int(cursor)
		effects.Subscribers += session.Info().Subscribers
	}
	files, err := filepath.Glob(filepath.Join(store.Root(), "traj_*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	effects.TrajectoryFiles = len(files)
	return effects
}

type querySelectionGroupCount struct {
	Group string
	Count int
}

func TestHTTPScalarQuerySelectionMatchesActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-http-query-selection.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture querySelectionFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 99 || len(fixture.Seeds.Sessions) != 3 || len(fixture.Seeds.Trajectories) != 3 || len(fixture.Seeds.TranscriptEpochs) != 2 || len(fixture.Seeds.Events) != 6 {
		t.Fatal("Source query selection inventory changed")
	}
	for _, row := range fixture.Cases {
		if row.RawQueryHex != nil || row.SourceFault != nil {
			t.Fatal("encoded selection corpus unexpectedly contains raw query or fault metadata", row.Name)
		}
	}
	groups := []querySelectionGroupCount{{"auth", 15}, {"sessions", 18}, {"session-trajectories", 12}, {"trajectories", 11}, {"envelope", 17}, {"trajectory-session", 8}, {"format", 9}, {"epoch", 9}}
	for _, group := range groups {
		count := 0
		for index, row := range fixture.Cases {
			if row.Group == group.Group {
				count++
			}
			for _, previous := range fixture.Cases[:index] {
				if previous.Name == row.Name {
					t.Fatalf("duplicate Source case %q", row.Name)
				}
			}
		}
		if count != group.Count {
			t.Fatalf("Source group %s has %d cases, want %d", group.Group, count, group.Count)
		}
	}
	runHTTPQueryContracts(t, fixture)
}

// Both corpora use the same actual manager, durable records and HTTP server.
// Their entry tests separately pin the encoded-value and raw-byte inventories.
func runHTTPQueryContracts(t *testing.T, fixture querySelectionFixture) {
	t.Helper()
	if len(fixture.Seeds.Sessions) != 3 || len(fixture.Seeds.Trajectories) != 3 || len(fixture.Seeds.TranscriptEpochs) != 2 || len(fixture.Seeds.Events) != 6 {
		t.Fatal("Source query seed inventory changed")
	}

	root := t.TempDir()
	state, err := sqlitestate.Open(context.Background(), filepath.Join(root, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := state.Close(); err != nil {
			t.Error(err)
		}
	})
	traces, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "trajectories"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	store := &querySelectionTrajectoryStore{Store: traces}
	provider := &countedProvider{}
	bash := &countedBash{}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{
		WorkspaceRoot: filepath.Join(root, "sessions"),
		Defaults:      agent.SessionDefaults{Model: "fixture-model", PermissionMode: agent.ModeReadonly},
		Services:      agent.ManagerServices{Provider: provider, BashFactory: fixedBashFactory{bash}, StateStore: state, Trajectories: store},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	identities := make([]querySelectionIdentity, 0, 6)
	sessions := make([]*agent.ManagedSession, 0, 3)
	for _, seed := range fixture.Seeds.Sessions {
		session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: seed.Owner, PermissionMode: agent.ModeReadonly})
		if err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, session)
		identities = append(identities, querySelectionIdentity{Key: seed.Key, Actual: string(session.ID()), Workspace: session.Info().Workspace, CreatedAt: seed.CreatedAt})
	}
	for index, seed := range fixture.Seeds.Trajectories {
		id := "traj_" + strings.Repeat(strconv.Itoa(index+1), 24)
		identities = append(identities, querySelectionIdentity{Key: seed.Key, Actual: id})
	}
	resolveID := func(key string) string {
		t.Helper()
		for _, identity := range identities {
			if identity.Key == key {
				return identity.Actual
			}
		}
		t.Fatalf("Source seed uses unknown identity %q", key)
		return ""
	}
	for _, seed := range fixture.Seeds.Trajectories {
		if seed.RawJSONL == "" {
			t.Fatal("Source trajectory seed lacks captured JSONL", seed.Key)
		}
		var contents strings.Builder
		for _, line := range strings.Split(strings.TrimSpace(seed.RawJSONL), "\n") {
			value := querySelectionBindSeed(querySelectionDecode(t, []byte(line)), identities)
			encoded, err := value.MarshalUTF8()
			if err != nil {
				t.Fatal(err)
			}
			contents.Write(encoded)
			contents.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(store.Root(), resolveID(seed.Key)+".jsonl"), []byte(contents.String()), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, seed := range fixture.Seeds.Events {
		value := querySelectionBindSeed(seed.Event, identities)
		encoded, err := value.MarshalUTF8()
		if err != nil {
			t.Fatal(err)
		}
		event, err := agent.DecodeStoredEvent(encoded)
		if err != nil {
			t.Fatal(err)
		}
		id := agent.SessionID(resolveID(seed.Session))
		if event.SessionID != id || event.Event.Kind() != agent.EventStatus {
			t.Fatal("Source event seed has incorrect session or kind", seed.Session)
		}
		if _, err := state.AppendEvent(context.Background(), id, event); err != nil {
			t.Fatal(err)
		}
	}
	for _, seed := range fixture.Seeds.TranscriptEpochs {
		if _, err := state.AppendMessages(context.Background(), agent.SessionID(resolveID("alice_old")), seed.Messages, seed.Epoch); err != nil {
			t.Fatal(err)
		}
	}

	handler := testServer(t, Config{Manager: manager, Auth: tokenAuth(t), PingInterval: time.Hour})
	requests := make(chan queryContractWireRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- queryContractWireRequest{RawQuery: r.URL.RawQuery, RequestURI: r.RequestURI}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer client.CloseIdleConnections()
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			before := querySelectionSnapshot(t, manager, sessions, state, store)
			if before != row.Effects.Before {
				t.Fatalf("effects before = %+v; Source = %+v", before, row.Effects.Before)
			}
			reads := store.reads()
			path := querySelectionResolve(row.Path, identities)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, row.Method, server.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.URL.RawQuery = queryContractRawQuery(t, row, identities)
			req.Host = "fixture"
			if row.Authorization != nil {
				req.Header.Set("Authorization", *row.Authorization)
			}
			for _, pair := range row.RequestHeaders {
				req.Header.Add(pair.Name, pair.Value)
			}
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cancel()
				response.Body.Close()
				wait(t, func() bool {
					for _, session := range sessions {
						if session.Info().Subscribers != 0 {
							return false
						}
					}
					return true
				})
			}()
			select {
			case observed := <-requests:
				if observed.RawQuery != req.URL.RawQuery || observed.RequestURI != req.URL.RequestURI() {
					t.Fatalf("HTTP transport changed query bytes: got %x / %q; want %x / %q", observed.RawQuery, observed.RequestURI, req.URL.RawQuery, req.URL.RequestURI())
				}
			default:
				t.Fatal("request did not reach the actual HTTP handler")
			}
			if response.StatusCode != row.Status {
				if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
					t.Fatalf("status %d with live SSE; Source %d", response.StatusCode, row.Status)
				}
				body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
				t.Fatalf("status %d; Source %d; body %s", response.StatusCode, row.Status, body)
			}
			if headers := querySelectionReadHeaders(response.Header, identities); !reflect.DeepEqual(headers, row.ResponseHeaders) {
				got, _ := json.Marshal(headers)
				want, _ := json.Marshal(row.ResponseHeaders)
				t.Fatalf("headers = %s; Source = %s", got, want)
			}
			switch row.BodyKind {
			case "sse":
				if len(row.Events) != 1 || req.Header.Get("Last-Event-ID") != "1" {
					t.Fatal("Source SSE corpus must use one durable catch-up frame after cursor 1")
				}
				frame := querySelectionReadFrame(t, response.Body)
				cancel()
				response.Body.Close()
				wait(t, func() bool {
					for _, session := range sessions {
						if session.Info().Subscribers != 0 {
							return false
						}
					}
					return true
				})
				if frame.ID != row.Events[0].ID || frame.Event != row.Events[0].Event {
					t.Fatalf("SSE id/event = %q/%q; Source = %q/%q", frame.ID, frame.Event, row.Events[0].ID, row.Events[0].Event)
				}
				querySelectionAssertJSON(t, querySelectionNormalize(frame.Data, identities), row.Events[0].Data)
			case "json", "jsonl", "text":
				body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
				if err != nil || len(body) > 1<<20 {
					t.Fatal("bounded response read failed", err, len(body))
				}
				response.Body.Close()
				if row.BodyKind == "text" {
					if row.BodyText == nil || querySelectionNormalizeText(string(body), identities) != *row.BodyText {
						t.Fatalf("response text = %q; Source = %v", body, row.BodyText)
					}
				} else {
					var value jsonvalue.Value
					if row.BodyKind == "jsonl" {
						records := []jsonvalue.Value{}
						for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
							records = append(records, querySelectionDecode(t, []byte(line)))
						}
						value = jsonvalue.ArrayValue(records)
					} else {
						value = querySelectionDecode(t, body)
					}
					querySelectionAssertJSON(t, querySelectionNormalize(value, identities), row.BodyJSON)
				}
			default:
				t.Fatal("unsupported Source response kind", row.BodyKind)
			}
			if after := querySelectionSnapshot(t, manager, sessions, state, store); after != row.Effects.After || after != before {
				t.Fatalf("effects after = %+v; Source = %+v; before = %+v", after, row.Effects.After, before)
			}
			if row.Status >= 400 && store.reads() != reads {
				t.Fatalf("rejected request performed a bulk trajectory read: before %+v, after %+v", reads, store.reads())
			}
			if provider.calls.Load() != 0 {
				t.Fatal("read-only query invoked model", provider.calls.Load())
			}
			if bash.calls.Load() != 0 {
				t.Fatal("read-only query executed a tool", bash.calls.Load())
			}
		})
	}
	for _, seed := range fixture.Seeds.TranscriptEpochs {
		messages, err := state.LoadMessages(context.Background(), agent.SessionID(resolveID("alice_old")), &seed.Epoch)
		if err != nil || !reflect.DeepEqual(messages, seed.Messages) {
			t.Fatal("query altered native stored transcript", seed.Epoch, err)
		}
	}
	for _, session := range sessions {
		if session.Info().RunCount != 0 || len(session.Messages()) != 0 {
			t.Fatal("query changed live session", session.ID())
		}
	}
}
