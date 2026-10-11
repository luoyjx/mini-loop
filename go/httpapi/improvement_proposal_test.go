package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/selfimprove"
	"github.com/luoyjx/mini-loop/go/shell"
)

type proposalHTTPBash struct {
	root  string
	fail  bool
	calls int
}

type proposalWriteProvider struct{}

func (proposalWriteProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if len(request.Messages) == 1 {
		return protocol.ModelReply{ID: "write", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewToolUse("write", protocol.WriteFileToolInput(protocol.WriteFileInput{Path: "made.txt", Content: "proof"}))}, StopReason: protocol.StopToolUse}, nil
	}
	return (doneProvider{}).Complete(context.Background(), request)
}

func TestProposalActualTCPGitCommitAndOwnedLineage(t *testing.T) {
	manager := testManager(t, proposalWriteProvider{})
	alice, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	root := alice.Info().Workspace
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatal(string(output), err)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-b", "proposal")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "base.txt")
	git("commit", "-m", "baseline")
	server := httptest.NewServer(testServer(t, Config{Manager: manager, Auth: tokenAuth(t)}))
	defer server.Close()
	r, err := http.NewRequest("POST", server.URL+"/sessions/"+string(alice.ID())+"/propose-improvement", strings.NewReader(`{"objective":"write proof","acceptance_command":"test -f made.txt","parent_id":"imp_parent"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer token-a")
	r.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var proposal struct {
		Verified   bool
		Branch     string
		DiffStat   string `json:"diff_stat"`
		ProposalID string `json:"proposal_id"`
		ParentID   string `json:"parent_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&proposal); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !proposal.Verified || proposal.ProposalID == "" || proposal.ParentID != "imp_parent" || !strings.Contains(proposal.DiffStat, "made.txt") {
		t.Fatal(response.StatusCode, proposal)
	}
	if git("status", "--porcelain") != "" || git("log", "-1", "--pretty=%s") != "self-improvement proposal" {
		t.Fatal("missing branch commit")
	}
	s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	for _, test := range []struct {
		token string
		count int
	}{{"token-a", 1}, {"token-b", 0}} {
		w := request(s, "GET", "/improvements", "", test.token)
		rows := decode[struct{ Proposals []json.RawMessage }](t, w.Body.Bytes())
		if w.Code != 200 || len(rows.Proposals) != test.count {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func (b *proposalHTTPBash) Workspace() string { return b.root }
func (b *proposalHTTPBash) ExecuteBash(ctx context.Context, input protocol.BashInput) (string, error) {
	result, err := b.ExecuteBashResult(ctx, input)
	return result.Render(), err
}
func (b *proposalHTTPBash) ExecuteBashResult(_ context.Context, input protocol.BashInput) (shell.Result, error) {
	b.calls++
	if b.fail {
		panic("credential")
	}
	zero := 0
	result := shell.Result{ExitCode: &zero}
	if input.Command == "git rev-parse --abbrev-ref HEAD" {
		result.Stdout = "proposal\n"
	}
	return result, nil
}

type proposalHTTPBashFactory struct {
	fail      bool
	executors []*proposalHTTPBash
}

func (f *proposalHTTPBashFactory) BashFor(_ context.Context, binding agent.SessionBinding) (agent.BashExecutor, error) {
	b := &proposalHTTPBash{root: binding.Workspace, fail: f.fail}
	f.executors = append(f.executors, b)
	return b, nil
}

func TestActualPythonProposalHTTPAdmission(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-improvement-proposal-http.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := decode[struct {
		Cases []struct {
			Name, Raw, Token, Target, Method, Action string
			ContentType                              *string `json:"content_type"`
			Busy                                     bool
			Status                                   int
			Response                                 json.RawMessage
		}
	}](t, data)
	if len(fixture.Cases) != 41 {
		t.Fatal(len(fixture.Cases))
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			provider := &countedProvider{}
			var configured agent.Provider = provider
			var blocked *blockedProvider
			if recipe.Busy {
				blocked = &blockedProvider{entered: make(chan struct{})}
				configured = blocked
			}
			factory := &proposalHTTPBashFactory{fail: recipe.Action == "error"}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(t.TempDir(), "root"), Services: agent.ManagerServices{Provider: configured, BashFactory: factory}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { manager.Stop(context.Background()) })
			alice, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			bob, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "bob"})
			if err != nil {
				t.Fatal(err)
			}
			if recipe.Action != "real" {
				command := exec.Command("git", "init", "-b", "proposal")
				command.Dir = alice.Info().Workspace
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatal(string(output), err)
				}
			}
			if recipe.Busy {
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() { defer close(done); alice.Run(ctx, "busy") }()
				<-blocked.entered
				defer func() { cancel(); <-done }()
			}
			s := testServer(t, Config{Manager: manager, Auth: tokenAuth(t), RateLimitPerMinute: 1})
			target := alice.ID()
			if recipe.Target == "bob" {
				target = bob.ID()
			} else if recipe.Target == "missing" {
				target = "missing"
			}
			r := httptest.NewRequest(recipe.Method, "/sessions/"+string(target)+"/propose-improvement", strings.NewReader(recipe.Raw))
			if recipe.Token != "" {
				r.Header.Set("Authorization", "Bearer "+recipe.Token)
			}
			if recipe.ContentType != nil {
				r.Header.Set("Content-Type", *recipe.ContentType)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != recipe.Status {
				t.Fatalf("status %d want %d: %s", w.Code, recipe.Status, w.Body.String())
			}
			if w.Code == 200 {
				proposal := decode[selfimprove.Proposal](t, w.Body.Bytes())
				// Flat lineage is a response boundary; read its explicit ID for normalization.
				identity := decode[struct {
					ProposalID string `json:"proposal_id"`
				}](t, w.Body.Bytes())
				got := normalizeJSON(t, w.Body.Bytes(), map[string]string{identity.ProposalID: "<proposal>"}, map[string]string{alice.Info().Workspace: "<workspace>"})
				want := normalizeJSON(t, recipe.Response, nil, nil)
				if string(got) != string(want) {
					t.Fatalf("got %s want %s", got, want)
				}
				if !proposal.Verified || provider.calls.Load() != 1 {
					t.Fatal(proposal, provider.calls.Load())
				}
				// The source proposal route does not consume the common rate budget.
				path := "/sessions/" + string(alice.ID()) + "/messages"
				if v := request(s, "POST", path, `{"message":"next"}`, "token-a"); v.Code != 200 {
					t.Fatal(v.Code, v.Body.String())
				}
				if v := request(s, "POST", path, `{"message":"again"}`, "token-a"); v.Code != 429 {
					t.Fatal(v.Code)
				}
			} else {
				text := strings.ReplaceAll(w.Body.String(), string(alice.ID()), "<alice>")
				text = strings.ReplaceAll(text, string(bob.ID()), "<bob>")
				if w.Code == 500 {
					var expected string
					if err := json.Unmarshal(recipe.Response, &expected); err != nil || text != expected {
						t.Fatal(text, string(recipe.Response), err)
					}
				} else {
					got := normalizeJSON(t, []byte(text), nil, nil)
					want := normalizeJSON(t, recipe.Response, nil, nil)
					if string(got) != string(want) {
						t.Fatalf("got %s want %s", got, want)
					}
				}
				if !recipe.Busy && recipe.Action != "error" && provider.calls.Load() != 0 {
					t.Fatal("rejected request reached provider")
				}
			}
		})
	}
}
