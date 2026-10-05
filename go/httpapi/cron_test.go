package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/cron"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCronHTTPMatchesActualPythonOwnershipAndValidation(t *testing.T) {
	data, e := os.ReadFile("../testdata/python-cron-surfaces.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		HTTP []struct {
			Name, Method, Path, Token string
			Input                     json.RawMessage
			Status                    int
			Body                      json.RawMessage
		}
	}
	if e = json.Unmarshal(data, &fixture); e != nil || len(fixture.HTTP) != 29 {
		t.Fatal(e)
	}
	m := testManager(t, doneProvider{})
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	a := create(t, s, "token-a")
	b := create(t, s, "token-b")
	sessions := map[string]string{"session-fixture": string(a.ID), "other-session": string(b.ID)}
	aliases := map[cron.ID]string{}
	type body struct {
		Session string
		Jobs    []agent.CronJobView
		Result  string
		Job     string
		Armed   bool
		Detail  string
	}
	for _, row := range fixture.HTTP {
		t.Run(row.Name, func(t *testing.T) {
			path := row.Path
			for alias, id := range sessions {
				path = strings.ReplaceAll(path, alias, id)
			}
			for id, alias := range aliases {
				path = strings.ReplaceAll(path, alias, string(id))
			}
			value := string(row.Input)
			if value == "null" {
				value = ""
			}
			w := request(s, row.Method, path, value, row.Token)
			if w.Code != row.Status {
				t.Fatalf("status %d / %d: %s", w.Code, row.Status, w.Body.String())
			}
			for _, job := range m.CronScheduler().Jobs() {
				if _, ok := aliases[job.ID]; !ok {
					aliases[job.ID] = fmt.Sprintf("<job%d>", len(aliases)+1)
				}
			}
			if w.Code == 422 {
				return
			} // Structured Pydantic detail parity is an existing open HTTP boundary.
			out := w.Body.String()
			for alias, id := range sessions {
				out = strings.ReplaceAll(out, id, alias)
			}
			for id, alias := range aliases {
				out = strings.ReplaceAll(out, string(id), alias)
			}
			var got, want body
			if e = json.Unmarshal([]byte(out), &got); e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(row.Body, &want); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal(out, string(row.Body))
			}
		})
	}
}

func TestHTTPArmRestoredJobUsesOwnerAndNeverPersistsActivation(t *testing.T) {
	root := t.TempDir()
	sid := agent.SessionID("000000000000")
	data, e := json.Marshal([]cron.Job{{ID: "restored", Cron: "0 0 31 2 *", Prompt: "restored", Session: cron.SessionID(sid), Recurring: true, Durable: true}})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, ".cron.json")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	m, e := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: root, Services: agent.ManagerServices{Provider: doneProvider{}}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Stop(context.Background()) })
	// This test has no parallel peers. Pin only the six random identity bytes;
	// creation is still the real public manager operation, without restore authority.
	original := rand.Reader
	rand.Reader = bytes.NewReader(make([]byte, 6))
	session, e := m.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	rand.Reader = original
	if e != nil || session.ID() != sid {
		t.Fatal(e)
	}
	s := testServer(t, Config{Manager: m, Auth: tokenAuth(t)})
	base := "/sessions/" + string(sid) + "/cron/restored/arm"
	if m.CronScheduler().Armed("restored") {
		t.Fatal("restored job armed")
	}
	if w := request(s, "POST", base, "", "token-b"); w.Code != 404 || m.CronScheduler().Armed("restored") {
		t.Fatal(w.Code)
	}
	if w := request(s, "POST", base, "", "token-a"); w.Code != 200 || !m.CronScheduler().Armed("restored") {
		t.Fatal(w.Code, w.Body.String())
	}
	if after, e := os.ReadFile(path); e != nil || !bytes.Equal(data, after) {
		t.Fatal("persisted activation", e)
	}
	if _, e := m.ScheduleCron("alice", sid, agent.ScheduleCronRequest{Cron: "0 0 31 2 *", Prompt: "save"}); e != nil {
		t.Fatal(e)
	}
	reloaded, e := cron.New(cron.Config{DurablePath: path, Resolver: cronUnboundResolver{}})
	if e != nil || reloaded.Armed("restored") {
		t.Fatal("activation survived reload", e)
	}
}

type cronUnboundResolver struct{}

func (cronUnboundResolver) ResolveScheduled(cron.SessionID) (cron.Runner, error) { return nil, nil }
