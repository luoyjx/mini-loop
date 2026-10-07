package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func TestActualPythonMemoryHTTP(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-http.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Scenarios []struct {
			Mode  string
			Cases []struct {
				Name, Target, Suffix, Token string
				Update                      bool
				Status                      int
				Response                    json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Scenarios) != 3 {
		t.Fatal(err)
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Mode, func(t *testing.T) {
			if len(scenario.Cases) != 15 {
				t.Fatal(len(scenario.Cases))
			}
			ctx, root := context.Background(), t.TempDir()
			shared, err := memory.NewStore(ctx, filepath.Join(root, "workspaces", ".memory"), nil)
			if err != nil {
				t.Fatal(err)
			}
			services := agent.ManagerServices{Provider: doneProvider{}}
			if scenario.Mode == "local" {
				services.UserResources, err = userresources.NewResolver(ctx, filepath.Join(root, "users"), skills.EmptyCatalog(), nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "workspaces"), Services: services})
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
			var auth Authenticator = tokenAuth(t)
			actor, other := "token-a", "token-b"
			if scenario.Mode == "anonymous" {
				auth, actor, other = NullAuth{}, "", ""
			}
			s := testServer(t, Config{Manager: manager, Auth: auth})
			ids := map[string]agent.SessionID{"alice": create(t, s, actor).ID, "second": create(t, s, actor).ID, "bob": create(t, s, other).ID, "missing": "missing"}
			write := func(owner string, input memory.Input) {
				t.Helper()
				bound, err := memory.Bind(shared, memory.OwnerID(owner))
				if err != nil {
					t.Fatal(err)
				}
				if services.UserResources != nil {
					bundle, err := services.UserResources.ForOwner(ctx, userresources.OwnerID(owner))
					if err != nil {
						t.Fatal(err)
					}
					bound = bundle.Memory()
				}
				if _, err := bound.Write(ctx, input); err != nil {
					t.Fatal(err)
				}
			}
			for index, row := range scenario.Cases {
				if index == 1 {
					for _, owner := range []string{"alice", "bob"} {
						if scenario.Mode == "anonymous" {
							owner = "anonymous"
						}
						write(owner, memory.Input{Name: "界", Type: memory.Project, Description: owner + " description", Body: owner + " private body", Origin: memory.Imported})
					}
					if services.UserResources != nil {
						if _, err := shared.Write(ctx, "alice", memory.Input{Name: "fallback", Description: "shared only", Body: "must not leak"}); err != nil {
							t.Fatal(err)
						}
					}
				}
				if row.Update {
					owner := "alice"
					if scenario.Mode == "anonymous" {
						owner = "anonymous"
					}
					write(owner, memory.Input{Name: "界", Type: memory.Feedback, Description: "updated", Body: "latest body"})
				}
				t.Run(row.Name, func(t *testing.T) {
					w := request(s, "GET", "/sessions/"+string(ids[row.Target])+"/memory"+row.Suffix, "", row.Token)
					if w.Code != row.Status {
						t.Fatal(w.Code, row.Status, w.Body.String())
					}
					aliases := map[string]string{}
					for label, id := range ids {
						if label != "missing" {
							aliases[string(id)] = "<" + label + ">"
						}
					}
					if !bytes.Equal(normalizeJSON(t, w.Body.Bytes(), aliases, nil), normalizeJSON(t, row.Response, nil, nil)) {
						t.Fatalf("memory differs: %s, want %s", w.Body.String(), row.Response)
					}
				})
			}
		})
	}
}

func TestMemoryHTTPMasksAndContainsStoreFailure(t *testing.T) {
	ctx := context.Background()
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("TOKEN", "private-test-credential")
	root := t.TempDir()
	store, err := memory.NewStore(ctx, filepath.Join(root, "memory"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Write(ctx, "alice", memory.Input{Name: "fact", Description: "private-test-credential", Body: "private-test-credential"}); err != nil {
		t.Fatal(err)
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "workspaces"), Services: agent.ManagerServices{Provider: doneProvider{}, Memory: store, Secrets: registry}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	id := create(t, s, "token-a").ID
	for _, suffix := range []string{"", "/fact"} {
		w := request(s, "GET", "/sessions/"+string(id)+"/memory"+suffix, "", "token-a")
		if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("private-test-credential")) || !bytes.Contains(w.Body.Bytes(), []byte("secret-hidden")) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// Break the backing root after binding: denied owners still receive only 404,
	// while an admitted read contains filesystem diagnostics in a safe plain 500.
	if err := os.Rename(filepath.Join(root, "memory"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/fact"} {
		if w := request(s, "GET", "/sessions/"+string(id)+"/memory"+suffix, "", "token-b"); w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
		w := request(s, "GET", "/sessions/"+string(id)+"/memory"+suffix, "", "token-a")
		if w.Code != 500 || w.Body.String() != "Internal Server Error" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
