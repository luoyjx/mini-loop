package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

type skillHTTPProvider struct{}

func (skillHTTPProvider) Complete(ctx context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
	reply, err := (doneProvider{}).Complete(ctx, req)
	if req.Purpose == protocol.PurposePersonalSkillPreview {
		reply.Content = []protocol.Block{protocol.NewTextBlock(`{"schema":"mini-loop.personal-skill-draft/v1","decision":"create","description":"recipe","body":"procedure","evidence_indexes":[0]}`)}
	}
	return reply, err
}

func TestPersonalSkillHTTPValidationBeforeOwnershipAndMethodAdmission(t *testing.T) {
	s := testServer(t, Config{Manager: testManager(t, skillHTTPProvider{}), Auth: tokenAuth(t)})
	first := create(t, s, "token-a")
	base := "/sessions/" + string(first.ID) + "/personal-skills/"
	for _, path := range []string{base + "preview", base + "unknown/commit"} {
		for _, body := range []string{`null`, `{}`, `[]`, `{"name":"recipe","owner":"bob"}`, `{"digest":"bad"}`, `{"digest":"` + strings.Repeat("a", 64) + `","body":"override"}`} {
			w := request(s, "POST", path, body, "token-b")
			if w.Code != 422 {
				t.Fatal("invalid request reached ownership/service", path, body, w.Code)
			}
		}
		w := request(s, "GET", path, "", "token-a")
		if w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Fatal("method admission", w.Code, w.Header())
		}
	}
	w := request(s, "POST", base+"preview", `{"name":"recipe"}`, "token-a")
	if w.Code != 404 || decode[PersonalSkillErrorResponse](t, w.Body.Bytes()).Detail.Message != "personal skill publication is not enabled" {
		t.Fatal("disabled publication", w.Code, w.Body.String())
	}
}

func TestActualPythonPersonalSkillHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-personal-skill-http.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string
			Status   int
			Response json.RawMessage
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 11 {
		t.Fatal(err, len(fixture.Cases))
	}
	root := t.TempDir()
	resolver, err := userresources.NewResolver(context.Background(), filepath.Join(root, "users"), skills.EmptyCatalog(), nil)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "workspaces"), Services: agent.ManagerServices{Provider: skillHTTPProvider{}, UserResources: resolver}})
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
	first, second := create(t, s, "token-a"), create(t, s, "token-a")
	base := "/sessions/" + string(first.ID)
	var draft userresources.DraftPreview
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			path, body, token := base+"/personal-skills/preview", `{"name":"recipe"}`, "token-a"
			commit := base + "/personal-skills/" + string(draft.ID) + "/commit"
			reviewed := `{"digest":"` + string(draft.Digest) + `"}`
			switch row.Name {
			case "unauthenticated":
				token = ""
			case "query-token-refused":
				token = ""
				path += "?access_token=token-a"
			case "foreign-preview":
				token = "token-b"
			case "empty-evidence":
			case "preview":
				if w := request(s, "POST", base+"/messages", `{"message":"human evidence"}`, token); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			case "readonly":
				if w := request(s, "POST", base+"/mode", `{"mode":"readonly"}`, token); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				path, body = commit, reviewed
			case "wrong-digest":
				if w := request(s, "POST", base+"/mode", `{"mode":"interactive"}`, token); w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				path, body = commit, `{"digest":"`+strings.Repeat("0", 64)+`"}`
			case "cross-session":
				path = "/sessions/" + string(second.ID) + "/personal-skills/" + string(draft.ID) + "/commit"
				body = reviewed
			case "foreign-commit":
				path, body, token = commit, reviewed, "token-b"
			case "commit", "consumed":
				path, body = commit, reviewed
			default:
				t.Fatal(row.Name)
			}
			w := request(s, "POST", path, body, token)
			if w.Code != row.Status {
				t.Fatal(w.Code, w.Body.String(), row.Status)
			}
			if row.Name == "preview" {
				draft = decode[userresources.DraftPreview](t, w.Body.Bytes())
				if draft.ID == "" || draft.ExpiresAt-draft.CreatedAt != 900 {
					t.Fatal("invalid draft lifetime", draft)
				}
			}
			if row.Name == "commit" {
				receipt := decode[agent.PersonalSkillCommitReceipt](t, w.Body.Bytes())
				if receipt.DraftID != draft.ID || receipt.Session != first.ID {
					t.Fatal("receipt binding", receipt)
				}
			}
			var response map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code == 200 {
				for _, key := range []string{"draft_id", "session", "created_at", "expires_at"} {
					delete(response, key)
				}
			}
			raw, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			got := normalizeJSON(t, raw, map[string]string{string(first.ID): "<session>"}, nil)
			want := normalizeJSON(t, row.Response, nil, nil)
			if !bytes.Equal(got, want) {
				t.Fatalf("HTTP response differs\ngot  %s\nwant %s", got, want)
			}
		})
	}
}
