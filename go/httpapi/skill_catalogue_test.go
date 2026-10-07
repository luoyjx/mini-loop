package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

func TestActualPythonSkillCatalogueHTTP(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-skill-catalogue-http.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Scenarios []struct {
			Mode  string
			Cases []struct {
				Name, Target, Token, Query string
				Status                     int
				Response                   json.RawMessage
			}
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Scenarios) != 4 {
		t.Fatal(err, len(fixture.Scenarios))
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Mode, func(t *testing.T) {
			if len(scenario.Cases) != 12 {
				t.Fatal(len(scenario.Cases))
			}
			ctx, root := context.Background(), t.TempDir()
			directory := filepath.Join(root, "agent")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if scenario.Mode != "empty" {
				canonical, err := userresources.NewCanonicalSkill(userresources.SkillFields{Name: "first", Description: "Agent first 界", Body: "private agent body"})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(directory, "first", "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(canonical.Text()), 0600); err != nil {
					t.Fatal(err)
				}
			}
			catalogue, err := skills.NewCatalog(ctx, directory)
			if err != nil {
				t.Fatal(err)
			}
			services := agent.ManagerServices{Provider: doneProvider{}, Skills: catalogue}
			if scenario.Mode == "layered" {
				services.UserResources, err = userresources.NewResolver(ctx, filepath.Join(root, "users"), catalogue, nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, owner := range []string{"alice", "bob"} {
					if _, err := services.UserResources.PublishSkill(ctx, userresources.OwnerID(owner), userresources.SkillFields{Name: "first", Description: owner + " private catalogue", Body: owner + " private body"}); err != nil {
						t.Fatal(err)
					}
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
			for _, row := range scenario.Cases {
				t.Run(row.Name, func(t *testing.T) {
					if row.Name == "alice-after" && services.UserResources != nil {
						if _, err := services.UserResources.PublishSkill(ctx, "alice", userresources.SkillFields{Name: "later", Description: "Later publication", Body: "not a live replacement"}); err != nil {
							t.Fatal(err)
						}
					}
					if row.Target == "fresh" {
						ids["fresh"] = create(t, s, actor).ID
					}
					if row.Target == "fork" {
						w := request(s, "POST", "/sessions/"+string(ids["alice"])+"/fork", "{}", actor)
						if w.Code != 200 {
							t.Fatal(w.Code, w.Body.String())
						}
						ids["fork"] = decode[SessionInfo](t, w.Body.Bytes()).ID
					}
					w := request(s, "GET", "/sessions/"+string(ids[row.Target])+"/skills"+row.Query, "", row.Token)
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
						t.Fatalf("catalogue differs: %s, want %s", w.Body.String(), row.Response)
					}
				})
			}
		})
	}
}

type catalogueHTTPSource struct {
	reads, loads atomic.Int32
	broken       atomic.Bool
}

func (source *catalogueHTTPSource) Descriptions() string {
	source.reads.Add(1)
	if source.broken.Load() {
		panic("private host path and catalogue-secret-12345")
	}
	return "one: description catalogue-secret-12345"
}
func (source *catalogueHTTPSource) Load(context.Context, protocol.LoadSkillInput) (string, error) {
	source.loads.Add(1)
	return "private skill body", nil
}

func TestSkillCatalogueScreensOwnerMasksDescriptionAndRefusesHostFailure(t *testing.T) {
	source := &catalogueHTTPSource{}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("API_TOKEN", "catalogue-secret-12345")
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Skills: source, Secrets: registry}})
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
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	path := "/sessions/" + string(create(t, s, "token-a").ID) + "/skills"
	source.reads.Store(0)
	for _, row := range []struct {
		path, token string
		status      int
	}{{path, "", 401}, {path + "?access_token=token-a", "", 401}, {path, "token-b", 404}, {"/sessions/missing/skills", "token-a", 404}} {
		if w := request(s, "GET", row.path, "", row.token); w.Code != row.status {
			t.Fatal(w.Code, row.status)
		}
	}
	if source.reads.Load() != 0 {
		t.Fatal("catalogue read before owner admission")
	}
	w := request(s, "GET", path, "", "token-a")
	if w.Code != 200 || strings.Contains(w.Body.String(), "catalogue-secret-12345") || source.reads.Load() != 1 || source.loads.Load() != 0 {
		t.Fatal(w.Code, w.Body.String(), source.reads.Load(), source.loads.Load())
	}
	value := decode[SkillCatalogueResponse](t, w.Body.Bytes())
	if value.Catalogue != registry.MaskText("one: description catalogue-secret-12345") {
		t.Fatal(value)
	}
	source.broken.Store(true)
	w = request(s, "GET", path, "", "token-a")
	if w.Code != 500 || w.Body.String() != "Internal Server Error" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatal(w.Code, w.Body.String())
	}
}
