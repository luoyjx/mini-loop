package httpapi

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/tasks"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskBoardHTTPMatchesActualPythonAndChecksOwnerBeforeFiles(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		HTTP struct {
			Seed  []tasks.Task
			Cases []struct {
				Name, Session, Token string
				Status               int
				Body                 json.RawMessage
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	manager := testManager(t, doneProvider{})
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	if response := request(server, "GET", "/sessions/"+string(session.ID())+"/tasks", "", "token-b"); response.Code != 404 {
		t.Fatal(response.Code)
	}
	if _, err = os.Stat(filepath.Join(session.Info().Workspace, ".tasks")); !os.IsNotExist(err) {
		t.Fatal("foreign read touched task directory")
	}
	var board *tasks.Store
	for _, row := range fixture.HTTP.Cases {
		t.Run(row.Name, func(t *testing.T) {
			if row.Name == "owned" {
				board, err = tasks.New(tasks.Config{Workspace: session.Info().Workspace})
				if err != nil {
					t.Fatal(err)
				}
				for _, task := range fixture.HTTP.Seed {
					if err = board.Save(task); err != nil {
						t.Fatal(err)
					}
				}
			}
			id := row.Session
			if id == "session-fixture" {
				id = string(session.ID())
			}
			response := request(server, "GET", "/sessions/"+id+"/tasks", "", row.Token)
			if response.Code != row.Status {
				t.Fatal(response.Code, response.Body.String())
			}
			compareTrajectoryJSON(t, []byte(replaceSession(response.Body.String(), session.ID())), row.Body)
		})
	}
	// Structured polling retains the claimed record; no claim marker is consumed.
	record, err := board.Load("task_first")
	if err != nil || record == nil || record.Status != tasks.InProgress || record.Owner == nil || *record.Owner != "alice" {
		t.Fatal(record, err)
	}
}
func replaceSession(text string, id agent.SessionID) string {
	return strings.ReplaceAll(text, string(id), "session-fixture")
}
