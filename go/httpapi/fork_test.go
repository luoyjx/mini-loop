package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type forkHTTPFixture struct {
	HTTP []struct {
		Name, Token string
		Status      int
		Response    json.RawMessage
	}
}
type forkHTTPProvider struct {
	entered, release chan struct{}
	once             sync.Once
}

func (p *forkHTTPProvider) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-ctx.Done():
		return protocol.ModelReply{}, ctx.Err()
	case <-p.release:
	}
	reply, err := (doneProvider{}).Complete(ctx, r)
	reply.Content = []protocol.Block{protocol.NewTextBlock("noted")}
	return reply, err
}
func TestForkHTTPMatchesActualPythonAndExposesLineage(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-forks.json")
	if err != nil {
		t.Fatal(err)
	}
	expected := decode[forkHTTPFixture](t, data)
	p := &forkHTTPProvider{entered: make(chan struct{}), release: make(chan struct{})}
	m := testManager(t, p)
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	source := create(t, s, "token-a")
	session, err := m.Get("alice", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	released := false
	defer func() {
		if !released {
			close(p.release)
		}
	}()
	children := []agent.SessionID{}
	for _, c := range expected.HTTP {
		id := source.ID
		if c.Name == "missing" {
			id = "missing"
		}
		if c.Name == "busy" {
			go func() { _, err := session.Run(context.Background(), "go"); done <- err }()
			receive(t, p.entered)
		}
		if c.Name == "completed" {
			close(p.release)
			released = true
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		w := request(s, "POST", "/sessions/"+string(id)+"/fork", "", c.Token)
		if w.Code != c.Status {
			t.Fatal(c.Name, w.Code, w.Body.String())
		}
		replacements := map[string]string{string(source.ID): "source"}
		paths := map[string]string{}
		if w.Code == 200 {
			child := decode[SessionInfo](t, w.Body.Bytes())
			children = append(children, child.ID)
			replacements[string(child.ID)] = "child"
			paths[child.Workspace] = "child-workspace"
			// The fixture uses an explicit manager model. Replace the currently known
			// deployment default only for this boundary comparison, checked below.
			paths[child.Model] = "default-model"
			if child.Model != m.Summary().Model {
				t.Fatal("fork model not manager default")
			}
			for _, path := range []string{"", "/messages", "/events", "/fork"} {
				method := "GET"
				body := ""
				if path == "/messages" || path == "/fork" {
					method = "POST"
					body = `{"message":"foreign"}`
				}
				foreign := request(s, method, "/sessions/"+string(child.ID)+path, body, "token-b")
				if foreign.Code != 404 {
					t.Fatal("child not owner scoped", path, foreign.Code)
				}
			}
			listed := request(s, "GET", "/sessions", "", "token-a")
			list := decode[[]SessionInfo](t, listed.Body.Bytes())
			found := false
			for _, v := range list {
				if v.ID == child.ID {
					found = v.ForkedFrom != nil && v.ForkedFrom.Session == source.ID && v.ForkedFrom.MessageCount == child.MessageCount
				}
			}
			if !found {
				t.Fatal("lineage absent in listing")
			}
		}
		got := normalizeJSON(t, w.Body.Bytes(), replacements, paths)
		want := normalizeJSON(t, c.Response, nil, nil)
		if !bytes.Equal(got, want) {
			t.Fatalf("%s %s != %s", c.Name, got, want)
		}
	}
	observer := session.Subscribe(true)
	defer observer.Close()
	count := 0
	for _, record := range session.Events() {
		if event, ok := record.Event.SessionForked(); ok {
			data, _ := json.Marshal(record)
			if !strings.Contains(string(data), `"child":"`+string(event.Child)+`"`) {
				t.Fatal("event JSON dropped child")
			}
			count++
		}
	}
	if count != len(children) {
		t.Fatal("fork events missing")
	}
}
func TestForkRateBudgetAndMissingLookupOrder(t *testing.T) {
	m := testManager(t, doneProvider{})
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t), RateLimitPerMinute: 1})
	v := create(t, s, "token-a")
	path := "/sessions/" + string(v.ID) + "/fork"
	if w := request(s, "POST", path, "", "token-b"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request(s, "POST", path, "", "token-a"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(s, "POST", path, "", "token-a"); w.Code != 429 {
		t.Fatal(w.Code)
	}
	if len(m.List("alice")) != 2 {
		t.Fatal("rate rejection allocated child")
	}
}
