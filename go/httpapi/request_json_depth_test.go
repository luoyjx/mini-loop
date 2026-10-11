package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestActualPythonLiveHTTPPersonalSkillDepth(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-personal-skill-http-depth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Transport      string
		RecursionLimit int `json:"recursion_limit"`
		Cases          []struct {
			Kind, Name, Raw string
			Status          int
			JSON, Text      *string
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 144 || fixture.RecursionLimit != 1000 || fixture.Transport != "uvicorn-default-http" {
		t.Fatal(err, len(fixture.Cases), fixture.RecursionLimit, fixture.Transport)
	}
	for _, mode := range []string{"unmasked", "masked"} {
		t.Run(mode, func(t *testing.T) {
			registry := secrets.New(secrets.Config{})
			services := agent.ManagerServices{Provider: doneProvider{}}
			if mode == "masked" {
				registry.RegisterValue("CANARY", "depth-canary-12345")
				services.Secrets = registry
			}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(t.TempDir(), "root"), Services: services})
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
			for _, row := range fixture.Cases {
				t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
					suffix := "preview"
					if row.Kind == "commit" {
						suffix = "draft/commit"
					}
					r := httptest.NewRequest("POST", "/sessions/missing/personal-skills/"+suffix, strings.NewReader(row.Raw))
					r.Header.Set("Authorization", "Bearer token-a")
					r.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					s.ServeHTTP(w, r)
					if w.Code != row.Status {
						t.Fatalf("status %d, want %d: %.100s", w.Code, row.Status, w.Body.String())
					}
					if row.Text != nil {
						if w.Body.String() != *row.Text || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
							t.Fatal(w.Body.String(), *row.Text)
						}
						return
					}
					if row.JSON == nil {
						t.Fatal("missing JSON source response")
					}
					want := *row.JSON
					if mode == "masked" {
						want = strings.ReplaceAll(want, "depth-canary-12345", registry.MaskText("depth-canary-12345"))
						if strings.Contains(w.Body.String(), "depth-canary-12345") {
							t.Fatal("deep validation input leaked registered credential")
						}
					}
					if !bytes.Equal(normalizeJSON(t, w.Body.Bytes(), nil, nil), normalizeJSON(t, []byte(want), nil, nil)) {
						t.Fatal("live source depth response differs")
					}
				})
			}
		})
	}
}
