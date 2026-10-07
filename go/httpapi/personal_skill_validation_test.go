package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/secrets"
)

func TestActualPythonPersonalSkillHTTPValidation(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-personal-skill-http-validation.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name, Raw string
			Status          int
			Response        json.RawMessage
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) != 41 {
		t.Fatal(err, len(fixture.Cases))
	}
	s := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t)})
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			suffix := "preview"
			if row.Kind == "commit" {
				suffix = "draft/commit"
			}
			w := request(s, "POST", "/sessions/missing/personal-skills/"+suffix, row.Raw, "token-a")
			if w.Code != row.Status {
				t.Fatal(w.Code, w.Body.String())
			}
			got, want := normalizeJSON(t, w.Body.Bytes(), nil, nil), normalizeJSON(t, row.Response, nil, nil)
			if !bytes.Equal(got, want) {
				t.Fatalf("validation response differs\ngot  %s\nwant %s", got, want)
			}
		})
	}
}

func TestPersonalSkillValidationMasksEscapedInputAndExtraKeys(t *testing.T) {
	const value = "clé-\"secret\"-界"
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("KEY", value)
	// Use the manager's concrete recording masker, as every new route does.
	// A separately configured manager avoids mutating any live session.
	config := agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Secrets: registry}}
	secured, err := agent.NewSessionManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := secured.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	s := testServer(t, Config{Manager: secured, Auth: tokenAuth(t)})
	body, err := json.Marshal(map[string]string{"name": "", value: value})
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", "/sessions/missing/personal-skills/preview", string(body), "token-a")
	if w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
	encoded, _ := json.Marshal(value)
	if bytes.Contains(w.Body.Bytes(), encoded) || strings.Contains(w.Body.String(), value) || !strings.Contains(w.Body.String(), string(secrets.Mask)) {
		t.Fatal("validation input/key leaked", w.Body.String())
	}
}
