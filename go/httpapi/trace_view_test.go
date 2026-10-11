package httpapi

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/trajectory"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActualPythonTrajectoryHTMLRoute(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trace-view.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		HTTP struct {
			ID, Seed string
			Cases    []struct {
				Name, Path, Token, Body, CSP string
				Status                       int
				ContentType                  string `json:"content_type"`
			}
		}
	}
	if json.Unmarshal(data, &fixture) != nil {
		t.Fatal("bad fixture")
	}
	root := t.TempDir()
	store, err := trajectory.New(trajectory.Config{Root: filepath.Join(root, "traces"), CaptureContent: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root(), fixture.HTTP.ID+".jsonl")
	os.WriteFile(path, []byte(fixture.HTTP.Seed), 0600)
	guarded := &guardedReader{TrajectoryStore: store}
	manager := recordingManager(t, filepath.Join(root, "ws"), guarded)
	server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t), Now: func() time.Time { return time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC) }})
	for _, row := range fixture.HTTP.Cases {
		t.Run(row.Name, func(t *testing.T) {
			if row.Name == "oversized" {
				file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				file.WriteString(strings.Repeat("x", 8388608) + "\n")
				file.Close()
			}
			beforeJSON, beforeSize := guarded.JSONCalls, guarded.SizeCalls
			response := request(server, "GET", row.Path, "", row.Token)
			if response.Code != row.Status || response.Header().Get("Content-Type") != row.ContentType || response.Header().Get("Content-Security-Policy") != row.CSP {
				t.Fatal(response.Code, response.Header(), response.Body.String())
			}
			if row.Status == 200 {
				if response.Body.String() != row.Body {
					t.Fatal("HTML differs from actual source HTTP page")
				}
			} else {
				compareTrajectoryJSON(t, response.Body.Bytes(), []byte(row.Body))
			}
			if row.Name == "foreign" && (beforeJSON != guarded.JSONCalls || beforeSize != guarded.SizeCalls) {
				t.Fatal("foreign view read full body")
			}
			if row.Name == "oversized" && beforeJSON != guarded.JSONCalls {
				t.Fatal("oversized view materialized JSON")
			}
		})
	}
	disabled := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t)})
	if response := request(disabled, "GET", "/trajectories/"+fixture.HTTP.ID+"/view", "", "token-a"); response.Code != 503 {
		t.Fatal("disabled view fabricated page")
	}
}
