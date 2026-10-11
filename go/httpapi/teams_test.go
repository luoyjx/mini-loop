package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/teams"
)

func TestActualPythonTeamHTTP(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Recipe struct {
				Name       string  `json:"name"`
				Target     string  `json:"target"`
				Token      *string `json:"token"`
				Rows       *int    `json:"rows"`
				Preload    *string `json:"preload"`
				PreloadHex *string `json:"preload_hex"`
				Teamless   bool    `json:"teamless"`
				Method     string  `json:"method"`
			} `json:"recipe"`
			Status   int             `json:"status"`
			Response json.RawMessage `json:"response"`
			Exists   bool            `json:"exists"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("../testdata/python-team-http.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	provider := &countedProvider{}
	manager := testManager(t, provider)
	ids := map[string]agent.SessionID{}
	for _, owner := range []agent.OwnerID{"alice", "bob"} {
		session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: owner})
		if err != nil {
			t.Fatal(err)
		}
		ids[string(owner)] = session.ID()
	}
	server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t), RateLimitPerMinute: 1})
	path := filepath.Join(manager.WorkspaceRoot(), ".teams", string(ids["alice"]), "inboxes", "lead.jsonl")
	for _, test := range fixture.Cases {
		t.Run(test.Recipe.Name, func(t *testing.T) {
			recipe := test.Recipe
			if recipe.Rows != nil {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				var payload strings.Builder
				for i := 0; i < *recipe.Rows; i++ {
					fmt.Fprintf(&payload, "{\"content\":\"m%03d\",\"metadata\":{},\"extra\":[1,true]}\n", i)
				}
				if err := os.WriteFile(path, []byte(payload.String()), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.Preload != nil || recipe.PreloadHex != nil {
				var bytes []byte
				if recipe.Preload != nil {
					bytes = []byte(*recipe.Preload)
				}
				if recipe.PreloadHex != nil {
					var err error
					bytes, err = hex.DecodeString(*recipe.PreloadHex)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, bytes, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.Teamless {
				// Source's custom-agent no-team variant is a projection test. Default
				// native managed sessions always have an established lead identity.
				payload, err := json.Marshal(TeamResponse{Session: ids["alice"]})
				if err != nil {
					t.Fatal(err)
				}
				compareTrajectoryJSON(t, normalizeJSON(t, payload, map[string]string{string(ids["alice"]): "<alice>"}, nil), test.Response)
				return
			}
			token := "token-a"
			if recipe.Token != nil {
				token = *recipe.Token
			}
			target := recipe.Target
			if target == "" {
				target = "alice"
			}
			id := ids[target]
			if target == "missing" {
				id = "missing"
			}
			method := recipe.Method
			if method == "" {
				method = "GET"
			}
			before, readErr := os.ReadFile(path)
			response := request(server, method, "/sessions/"+string(id)+"/team", "", token)
			if response.Code != test.Status {
				t.Fatalf("status %d %s", response.Code, response.Body.String())
			}
			if response.Code == 500 {
				var expected string
				if err := json.Unmarshal(test.Response, &expected); err != nil {
					t.Fatal(err)
				}
				if response.Body.String() != expected {
					t.Fatalf("private failure %q want %q", response.Body.String(), expected)
				}
			} else {
				compareTrajectoryJSON(t, normalizeJSON(t, response.Body.Bytes(), map[string]string{string(ids["alice"]): "<alice>", string(ids["bob"]): "<bob>"}, nil), test.Response)
			}
			after, afterErr := os.ReadFile(path)
			if (readErr == nil) != (afterErr == nil) || !reflect.DeepEqual(before, after) {
				t.Fatal("viewer changed mailbox")
			}
			if (afterErr == nil) != test.Exists {
				t.Fatalf("file existence: %v want %v", afterErr, test.Exists)
			}
		})
	}
	if provider.calls.Load() != 0 {
		t.Fatal("read reached provider")
	}
	response := request(server, "POST", "/sessions/"+string(ids["alice"])+"/messages", `{"message":"fresh"}`, "token-a")
	if response.Code != 200 {
		t.Fatalf("GET spent message budget: %d %s", response.Code, response.Body.String())
	}
}
func TestActualTCPTeamPeekLeavesDeliveryAndRejectsForeignUnreadableInbox(t *testing.T) {
	manager := testManager(t, doneProvider{})
	session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(manager.WorkspaceRoot(), ".teams")
	bus := teams.New(teams.Config{Root: &root})
	key := teams.Key(teams.TeamID(session.ID()), "lead")
	for i := 0; i < 75; i++ {
		if _, err := bus.Send(context.Background(), teams.SendRequest{From: "trusted/operator", To: key, Content: fmt.Sprintf("m%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(testServer(t, Config{Manager: manager, Auth: tokenAuth(t)}))
	defer server.Close()
	for _, token := range []string{"token-a", "token-a", "token-b"} {
		r, err := http.NewRequest("GET", server.URL+"/sessions/"+string(session.ID())+"/team", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if token == "token-b" {
			if response.StatusCode != 404 {
				t.Fatal(response.StatusCode)
			}
			continue
		}
		var body struct {
			Team  teams.TeamID
			Name  teams.MemberName
			Inbox []struct{ Content string }
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 || body.Team != teams.TeamID(session.ID()) || body.Name != "lead" || len(body.Inbox) != 50 || body.Inbox[0].Content != "m025" {
			t.Fatal(response.StatusCode, body)
		}
	}
	rows, err := bus.Read(context.Background(), key)
	if err != nil || len(rows) != 75 {
		t.Fatalf("delivery after viewing %d %v", len(rows), err)
	}
	path := filepath.Join(root, string(session.ID()), "inboxes", "lead.jsonl")
	if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	protected := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	if response := request(protected, "GET", "/sessions/"+string(session.ID())+"/team", "", "token-b"); response.Code != 404 {
		t.Fatal(response.Code, response.Body.String())
	}
	if response := request(protected, "GET", "/sessions/"+string(session.ID())+"/team", "", "token-a"); response.Code != 500 {
		t.Fatal(response.Code, response.Body.String())
	}
}
