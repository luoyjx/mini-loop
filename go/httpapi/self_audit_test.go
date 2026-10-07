package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/selfaudit"
	"github.com/luoyjx/mini-loop/go/skills"
	"github.com/luoyjx/mini-loop/go/userresources"
)

type auditSkillDiagnostics struct {
	log    problems.Log
	broken bool
}

func (*auditSkillDiagnostics) Descriptions() string { return "" }
func (*auditSkillDiagnostics) Load(context.Context, protocol.LoadSkillInput) (string, error) {
	return "", nil
}
func (source *auditSkillDiagnostics) SelfAuditProblems() selfaudit.Ledger {
	if source.broken {
		panic("private audit source")
	}
	return selfaudit.FromProblems(source.log.Snapshot())
}

func normalizeAuditSource(value string, ids map[string]agent.SessionID) string {
	for label, id := range ids {
		value = strings.ReplaceAll(value, string(id), "<"+label+">")
	}
	return value
}
func TestActualPythonSelfAuditHTTP(t *testing.T) {
	var fixture struct {
		Scenarios []struct {
			Mode          string
			GlobalEntries []string `json:"global_entries"`
			Cases         []struct {
				Name, Method, Path, Token, Body string
				Status                          int
				ContentType                     string `json:"content_type"`
				Challenge, Allow                *string
				Response                        json.RawMessage
			}
		}
	}
	data, err := os.ReadFile("../testdata/python-self-audit-http.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Scenarios) != 3 {
		t.Fatal(len(fixture.Scenarios))
	}
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Mode, func(t *testing.T) {
			ctx := context.Background()
			global := &auditSkillDiagnostics{}
			if err := global.log.Extend(scenario.GlobalEntries); err != nil {
				t.Fatal(err)
			}
			services := agent.ManagerServices{Provider: doneProvider{}, Skills: global}
			if scenario.Mode == "local" {
				services.UserResources, err = userresources.NewResolver(ctx, filepath.Join(t.TempDir(), "users"), skills.EmptyCatalog(), nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: services})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := manager.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			var auth Authenticator = tokenAuth(t)
			if scenario.Mode == "open" {
				auth = NullAuth{}
			}
			server := testServer(t, Config{Manager: manager, Auth: auth, RateLimitPerMinute: 1})
			ids := map[string]agent.SessionID{}
			for _, input := range []struct {
				label string
				owner agent.OwnerID
			}{{"alice", "alice"}, {"second", "alice"}, {"bob", "bob"}} {
				session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: input.owner, PermissionMode: agent.ModeAuto})
				if err != nil {
					t.Fatal(err)
				}
				ids[input.label] = session.ID()
			}
			if scenario.Mode == "local" {
				for _, owner := range []string{"alice", "bob"} {
					resources, err := services.UserResources.ForOwner(ctx, userresources.OwnerID(owner))
					if err != nil {
						t.Fatal(err)
					}
					for i := 0; i < 2; i++ {
						if _, err := resources.Memory().Write(ctx, memory.Input{Name: owner + "-friction", Type: memory.Project, Body: strings.Repeat("界", 32001)}); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if len(scenario.Cases) != 24 {
				t.Fatal(len(scenario.Cases))
			}
			for _, row := range scenario.Cases {
				t.Run(row.Name, func(t *testing.T) {
					response := request(server, row.Method, row.Path, row.Body, row.Token)
					if response.Code != row.Status || response.Header().Get("Content-Type") != row.ContentType {
						t.Fatalf("%d %s %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
					}
					for key, want := range map[string]*string{"WWW-Authenticate": row.Challenge, "Allow": row.Allow} {
						expected := ""
						if want != nil {
							expected = *want
						}
						if response.Header().Get(key) != expected {
							t.Fatalf("header %s: %s want %s", key, response.Header().Get(key), expected)
						}
					}
					if row.Status != 200 {
						got := decode[ErrorResponse](t, response.Body.Bytes())
						want := decode[ErrorResponse](t, row.Response)
						if got != want {
							t.Fatalf("error %+v want %+v", got, want)
						}
						return
					}
					if row.ContentType == "text/plain; charset=utf-8" {
						want := decode[string](t, row.Response)
						got := normalizeAuditSource(response.Body.String(), ids)
						if got != want {
							t.Fatalf("report\ngot %s\nwant %s", got, want)
						}
						return
					}
					if strings.Contains(row.Path, "/suggestions") {
						got := decode[SelfAuditSuggestionsResponse](t, response.Body.Bytes())
						for i := range got.Suggestions {
							got.Suggestions[i].Source = normalizeAuditSource(got.Suggestions[i].Source, ids)
						}
						want := decode[SelfAuditSuggestionsResponse](t, row.Response)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("suggestions %+v want %+v", got, want)
						}
					} else {
						got := decode[SelfAuditDraftsResponse](t, response.Body.Bytes())
						for i := range got.Drafts {
							got.Drafts[i].Source = normalizeAuditSource(got.Drafts[i].Source, ids)
						}
						want := decode[SelfAuditDraftsResponse](t, row.Response)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("drafts %+v want %+v", got, want)
						}
					}
				})
			}
		})
	}
}

// A ledger-only read must never fall through to recording IO. Embedded unused
// store methods fail if called; diagnostic state is the only installed seam.
type noAuditRecordingIO struct {
	agent.TrajectoryStore
	lists, visits int
}

func (*noAuditRecordingIO) Count(agent.SessionID) (int, error) { return 0, nil }
func (s *noAuditRecordingIO) List(agent.TrajectoryQuery) ([]agent.TrajectorySummary, error) {
	s.lists++
	panic("recording IO forbidden")
}
func (s *noAuditRecordingIO) VisitRecords(context.Context, agent.TrajectoryID, agent.TrajectoryEventQuery, func([]byte) error) error {
	s.visits++
	panic("recording IO forbidden")
}
func (*noAuditRecordingIO) SelfAuditProblems() selfaudit.Ledger {
	return selfaudit.Ledger{Entries: []string{"recorded friction"}}
}

func TestSelfAuditCurationDoesNotReadRecordingsOrLaunchWork(t *testing.T) {
	store := &noAuditRecordingIO{}
	provider := &countedProvider{}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: provider, Trajectories: store}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(context.Background()) })
	server := testServer(t, Config{Manager: manager})
	create(t, server, "")
	for _, path := range []string{"/self-audit/suggestions", "/self-audit/bench-task-drafts"} {
		w := request(server, "GET", path+"?limit=0&owner=foreign", `{"launch":true}`, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "recorded friction") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if store.lists != 0 || store.visits != 0 || provider.calls.Load() != 0 || len(manager.CronScheduler().Jobs()) != 0 {
		t.Fatal("curation performed work or read recordings")
	}
}

func TestSelfAuditAuthenticationBodyAdmissionAndNoRateSpending(t *testing.T) {
	server := testServer(t, Config{Manager: testManager(t, doneProvider{}), Auth: tokenAuth(t), RateLimitPerMinute: 1})
	id := create(t, server, "token-a").ID
	for _, path := range []string{"/self-audit", "/self-audit/suggestions", "/self-audit/bench-task-drafts"} {
		for i := 0; i < 2; i++ {
			if w := request(server, "GET", path, "", "token-a"); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		reader := &countReader{Data: strings.NewReader("do not read")}
		req := httptest.NewRequest("GET", path, reader)
		req.ContentLength = MaxRequestBytes + 1
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		if w.Code != 413 || reader.Count != 0 {
			t.Fatalf("body/auth admission %d/%d", w.Code, reader.Count)
		}
	}
	path := "/sessions/" + string(id) + "/messages"
	if w := request(server, "POST", path, `{"message":"first"}`, "token-a"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"/self-audit", "/self-audit/suggestions", "/self-audit/bench-task-drafts"} {
		if w := request(server, "GET", path, "", "token-a"); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(server, "POST", path, `{"message":"second"}`, "token-a"); w.Code != 429 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSelfAuditResponseProjectionFailsClosed(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("TOKEN", "audit-secret-12345")
	source := &auditSkillDiagnostics{}
	if err := source.log.Append("audit-secret-12345"); err != nil {
		t.Fatal(err)
	}
	for _, masker := range []agent.ApprovalRedactor{registry, panicMasker{}} {
		manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Skills: source, Secrets: masker}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { manager.Stop(context.Background()) })
		server := testServer(t, Config{Manager: manager})
		for _, path := range []string{"/self-audit", "/self-audit/suggestions", "/self-audit/bench-task-drafts"} {
			response := request(server, "GET", path, "", "")
			if strings.Contains(response.Body.String(), "audit-secret-12345") || strings.Contains(response.Body.String(), "private-credential") {
				t.Fatal("raw projection escaped")
			}
			if masker == registry {
				if response.Code != 200 || !strings.Contains(response.Body.String(), "secret-hidden") {
					t.Fatal(response.Code, response.Body.String())
				}
			} else if response.Code != 500 {
				t.Fatal(response.Code, response.Body.String())
			}
		}
	}
}

func TestSelfAuditFailedLedgerIsNotAnEmptyCurationResult(t *testing.T) {
	source := &auditSkillDiagnostics{broken: true}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Skills: source}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(context.Background()) })
	server := testServer(t, Config{Manager: manager})
	for _, path := range []string{"/self-audit/suggestions", "/self-audit/bench-task-drafts"} {
		w := request(server, "GET", path, "", "")
		if w.Code != 500 || w.Body.String() != "Internal Server Error" || strings.Contains(w.Body.String(), "private audit source") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(server, "GET", "/self-audit", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "unreadable (RuntimeError)") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSelfAuditReportCapThroughHTTP(t *testing.T) {
	source := &auditSkillDiagnostics{}
	for i := 0; i < 50; i++ {
		if err := source.log.Append(fmt.Sprintf("%02d-%s", i, strings.Repeat("界", 1000))); err != nil {
			t.Fatal(err)
		}
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Skills: source}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(context.Background()) })
	server := testServer(t, Config{Manager: manager})
	response := request(server, "GET", "/self-audit", "", "")
	if response.Code != 200 || len([]rune(response.Body.String())) != selfaudit.MaxReportCharacters+len([]rune("\n[report truncated at the cap]")) || !strings.HasSuffix(response.Body.String(), "[report truncated at the cap]") {
		t.Fatal(response.Code, len([]rune(response.Body.String())))
	}
}

func TestSelfAuditOwnedHTTPNeverExposesUnattributedSharedMemory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := memory.NewStore(ctx, filepath.Join(root, "memory"), nil)
	if err != nil {
		t.Fatal(err)
	}
	filename := "bob-private-unattributed.md"
	if err := os.WriteFile(filepath.Join(root, "memory", filename), []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(ctx, nil); err != nil {
		t.Fatal(err)
	}
	source := &auditSkillDiagnostics{}
	if err := source.log.Append("fleet-private-diagnostic"); err != nil {
		t.Fatal(err)
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "workspaces"), Services: agent.ManagerServices{Provider: doneProvider{}, Memory: store, Skills: source}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(ctx) })
	server := testServer(t, Config{Manager: manager, Auth: tokenAuth(t)})
	create(t, server, "token-a")
	bob := create(t, server, "token-b")
	for _, path := range []string{"/self-audit", "/self-audit/suggestions", "/self-audit/bench-task-drafts"} {
		w := request(server, "GET", path+"?owner=bob&include_global=true", "", "token-a")
		if w.Code != 200 || strings.Contains(w.Body.String(), filename) || strings.Contains(w.Body.String(), "fleet-private-diagnostic") || strings.Contains(w.Body.String(), string(bob.ID)) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// The fleet operator retains the evidence excluded from tenant output.
	open := testServer(t, Config{Manager: manager})
	if w := request(open, "GET", "/self-audit", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), filename) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSelfAuditProjectedTextKeepsCharacterCap(t *testing.T) {
	minimum := 1
	registry := secrets.New(secrets.Config{MinLength: &minimum})
	registry.RegisterValue("SHORT", "abcd")
	source := &auditSkillDiagnostics{}
	if err := source.log.Append(strings.Repeat("abcd", 2000)); err != nil {
		t.Fatal(err)
	}
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(), Services: agent.ManagerServices{Provider: doneProvider{}, Skills: source, Secrets: registry}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.Stop(context.Background()) })
	server := testServer(t, Config{Manager: manager})
	w := request(server, "GET", "/self-audit", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "abcd") || len([]rune(w.Body.String())) != selfaudit.MaxReportCharacters+len([]rune("\n[report truncated at the cap]")) {
		t.Fatal(w.Code, len([]rune(w.Body.String())))
	}
}
