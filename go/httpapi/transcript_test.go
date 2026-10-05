package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func (s *httpCatchupStore) TranscriptEpoch(ctx context.Context, id agent.SessionID) (agent.TranscriptEpoch, error) {
	s.mu.Lock()
	hook := s.onRead
	s.mu.Unlock()
	if hook != nil {
		if err := hook(ctx); err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := agent.TranscriptEpoch(0)
	for epoch := range s.messages[id] {
		if len(s.messages[id][epoch]) > 0 && epoch > current {
			current = epoch
		}
	}
	return current, nil
}
func (s *httpCatchupStore) LoadMessages(ctx context.Context, id agent.SessionID, epoch *agent.TranscriptEpoch) ([]protocol.Message, error) {
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
	target := agent.TranscriptEpoch(0)
	if epoch != nil {
		target = *epoch
	} else {
		for value := range s.messages[id] {
			if value > target {
				target = value
			}
		}
	}
	out := []protocol.Message{}
	for _, row := range s.messages[id][target] {
		out = append(out, protocol.Message{Role: row.Role, Content: row.Content.Clone()})
	}
	return out, nil
}

// Raw response bytes are retained only at this external fixture comparison boundary.
type transcriptFixture struct {
	Seeds []struct {
		Name   string
		Epochs [][]protocol.Message
	}
	Cases []struct {
		Name          string
		Null, Missing bool
		Query         [][2]string
		Token         *string
		Status        int
		Response      json.RawMessage
		Challenge     *string
	}
}

func transcriptFixtureData(t *testing.T) transcriptFixture {
	t.Helper()
	var fixture transcriptFixture
	data, err := os.ReadFile("../testdata/python-transcript.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}
func TestTranscriptActualPythonHTTPReadsAndValidation(t *testing.T) {
	fixture := transcriptFixtureData(t)
	for _, null := range []bool{false, true} {
		t.Run(map[bool]string{false: "sql", true: "null"}[null], func(t *testing.T) {
			var s *Server
			var session *agent.ManagedSession
			var store *httpCatchupStore
			if null {
				m := testManager(t, doneProvider{})
				s = testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
				var err error
				session, err = m.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = session.Run(context.Background(), "live history must not substitute for storage"); err != nil {
					t.Fatal(err)
				}
			} else {
				s, session, store = catchupHTTP(t)
			}
			for _, recipe := range fixture.Cases {
				if recipe.Null != null {
					continue
				}
				t.Run(recipe.Name, func(t *testing.T) {
					if !null {
						switch recipe.Name {
						case "sql-original":
							if _, err := store.AppendMessages(context.Background(), session.ID(), fixture.Seeds[0].Epochs[0], 1); err != nil {
								t.Fatal(err)
							}
						case "sql-latest":
							if _, err := store.AppendMessages(context.Background(), session.ID(), fixture.Seeds[0].Epochs[1], 2); err != nil {
								t.Fatal(err)
							}
						case "sql-crash-tail":
							rows := []protocol.Message{{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewToolUseWithoutCaller("crash", protocol.BashToolInput(protocol.BashInput{Command: "echo partial"})))}}
							if _, err := store.AppendMessages(context.Background(), session.ID(), rows, 4); err != nil {
								t.Fatal(err)
							}
						}
					}
					query := url.Values{}
					for _, pair := range recipe.Query {
						query.Add(pair[0], pair[1])
					}
					id := string(session.ID())
					if recipe.Missing {
						id = "missing"
					}
					path := "/sessions/" + id + "/transcript"
					if len(query) > 0 {
						path += "?" + query.Encode()
					}
					token := ""
					if recipe.Token != nil {
						token = *recipe.Token
					}
					response := request(s, "GET", path, "", token)
					got := normalizeJSON(t, response.Body.Bytes(), map[string]string{string(session.ID()): "fixture"}, nil)
					want := normalizeJSON(t, recipe.Response, nil, nil)
					if response.Code != recipe.Status || string(got) != string(want) {
						t.Fatalf("got %d %s; source %d %s", response.Code, got, recipe.Status, want)
					}
					challenge := response.Header().Get("WWW-Authenticate")
					if (recipe.Challenge == nil && challenge != "") || (recipe.Challenge != nil && challenge != *recipe.Challenge) {
						t.Fatal(challenge, recipe.Challenge)
					}
				})
			}
		})
	}
}
func TestTranscriptHTTPFailureAndCancellationNeverBlocksWriter(t *testing.T) {
	s, session, store := catchupHTTP(t)
	if _, err := session.Run(context.Background(), "stored turn"); err != nil {
		t.Fatal(err)
	}
	for _, panicRead := range []bool{false, true} {
		store.onRead = func(context.Context) error {
			if panicRead {
				panic("private database canary")
			}
			return errors.New("private database canary")
		}
		response := request(s, "GET", "/sessions/"+string(session.ID())+"/transcript", "", "token-a")
		if response.Code != 503 || strings.Contains(response.Body.String(), "canary") || session.PersistenceStatus().Error != nil {
			t.Fatal(response.Code, response.Body.String(), session.PersistenceStatus())
		}
	}
	entered, finished := make(chan struct{}), make(chan struct{})
	store.onRead = func(ctx context.Context) error { close(entered); <-ctx.Done(); close(finished); return ctx.Err() }
	server := httptest.NewServer(s)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/sessions/"+string(session.ID())+"/transcript", nil)
	req.Header.Set("Authorization", "Bearer token-a")
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, _ := server.Client().Do(req)
		if response != nil {
			response.Body.Close()
		}
	}()
	receive(t, entered)
	if _, err := session.Run(context.Background(), "writer runs while read is blocked"); err != nil {
		t.Fatal(err)
	}
	cancel()
	receive(t, finished)
	receive(t, done)
	if session.PersistenceStatus().Error != nil {
		t.Fatal("cancel changed writer state")
	}
}
func TestTranscriptActualTCPReadsStoredEpochWithoutModel(t *testing.T) {
	s, session, store := catchupHTTP(t)
	epochs := transcriptFixtureData(t).Seeds[0].Epochs
	for i, rows := range epochs {
		if _, err := store.AppendMessages(context.Background(), session.ID(), rows, agent.TranscriptEpoch(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(s)
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	req, _ := http.NewRequest("GET", server.URL+"/sessions/"+string(session.ID())+"/transcript?epoch=1", nil)
	req.Header.Set("Authorization", "Bearer token-a")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	result := decode[agent.TranscriptSnapshot](t, data)
	if response.StatusCode != 200 || result.Epoch != 1 || result.Epochs != 2 || !reflect.DeepEqual(result.Messages, epochs[0]) || session.Info().RunCount != 0 {
		t.Fatal(response.StatusCode, result)
	}
}
