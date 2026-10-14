package agent_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/httpapi"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type sourceTeamlessHeaders struct {
	ContentType           string  `json:"content-type"`
	ContentSecurityPolicy *string `json:"content-security-policy"`
	ContentTypeOptions    *string `json:"x-content-type-options"`
	FrameOptions          *string `json:"x-frame-options"`
	ReferrerPolicy        *string `json:"referrer-policy"`
}

type sourceTeamlessCase struct {
	Name                string                `json:"name"`
	State               string                `json:"state"`
	Method              string                `json:"method"`
	Path                string                `json:"path"`
	Token               string                `json:"token"`
	Status              int                   `json:"status"`
	Response            jsonvalue.Value       `json:"response"`
	Headers             sourceTeamlessHeaders `json:"headers"`
	ActualPeekCalls     int                   `json:"actual_peek_calls"`
	MailboxBeforeHex    string                `json:"mailbox_before_hex"`
	MailboxBeforeSHA256 string                `json:"mailbox_before_sha256"`
	MailboxAfterSHA256  string                `json:"mailbox_after_sha256"`
	MailboxUnchanged    bool                  `json:"mailbox_unchanged"`
	ModelCalls          int                   `json:"model_calls"`
}

type sourceTeamlessFixture struct {
	Cases           []sourceTeamlessCase `json:"cases"`
	SessionsCreated int                  `json:"sessions_created"`
	ModelCalls      int                  `json:"model_calls"`
	SourceSHA256    map[string]string    `json:"source_sha256"`
	ProducerSHA256  string               `json:"producer_sha256"`
	Scope           []string             `json:"scope"`
}

func teamlessHeaders(header http.Header) sourceTeamlessHeaders {
	optional := func(name string) *string {
		if value := header.Get(name); value != "" {
			return &value
		}
		return nil
	}
	return sourceTeamlessHeaders{ContentType: header.Get("Content-Type"),
		ContentSecurityPolicy: optional("Content-Security-Policy"),
		ContentTypeOptions:    optional("X-Content-Type-Options"), FrameOptions: optional("X-Frame-Options"),
		ReferrerPolicy: optional("Referrer-Policy")}
}

func teamlessSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// The external test imports the real HTTP implementation, whose owner lookup
// and PeekTeam call operate on the actual manager. Only the controlled Source
// state assembly uses an augmented-test adapter; no production seam is added.
func TestActualSourceTeamlessHTTPThroughOwnedHandler(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-teamless-http.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sourceTeamlessFixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 12 || fixture.SessionsCreated != 2 || fixture.ModelCalls != 0 {
		t.Fatal("actual Source teamless recipe inventory changed")
	}
	thinking := false
	provider := agent.NewFakeProvider(agent.FakeProviderConfig{Thinking: &thinking})
	manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: t.TempDir(),
		Defaults: agent.SessionDefaults{MaxRounds: 1}, Services: agent.ManagerServices{Provider: provider.ObjectView()}})
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
	ids := map[string]agent.SessionID{}
	for _, owner := range []agent.OwnerID{"alice", "bob"} {
		session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: owner})
		if err != nil {
			t.Fatal(err)
		}
		ids["session-"+string(owner)] = session.ID()
	}
	auth, err := httpapi.NewTokenAuth([]httpapi.TokenBinding{{Token: "token-a", Principal: "alice"}, {Token: "token-b", Principal: "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	server, err := httpapi.New(httpapi.Config{Manager: manager, Auth: auth, RateLimitPerMinute: 1})
	if err != nil {
		t.Fatal(err)
	}
	mailbox := filepath.Join(manager.WorkspaceRoot(), ".teams", string(ids["session-alice"]), "inboxes", "lead.jsonl")
	if err := os.MkdirAll(filepath.Dir(mailbox), 0700); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			if seen[row.Name] {
				t.Fatal("duplicate Source recipe")
			}
			seen[row.Name] = true
			before, err := hex.DecodeString(row.MailboxBeforeHex)
			if err != nil || teamlessSHA256(before) != row.MailboxBeforeSHA256 || !row.MailboxUnchanged || row.MailboxBeforeSHA256 != row.MailboxAfterSHA256 || row.ModelCalls != 0 {
				t.Fatal("Source mailbox/model evidence is inconsistent", err)
			}
			if err := os.WriteFile(mailbox, before, 0600); err != nil {
				t.Fatal(err)
			}
			path := row.Path
			for symbolic, actual := range ids {
				path = strings.ReplaceAll(path, symbolic, string(actual))
			}
			observe := func() {
				request := httptest.NewRequest(row.Method, path, nil)
				if row.Token != "" {
					request.Header.Set("Authorization", "Bearer "+row.Token)
				}
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				if response.Code != row.Status {
					t.Fatalf("actual handler status %d, Source %d: %s", response.Code, row.Status, response.Body.String())
				}
				body := response.Body.String()
				for symbolic, actual := range ids {
					body = strings.ReplaceAll(body, string(actual), symbolic)
				}
				var actual jsonvalue.Value
				if strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
					actual, err = jsonvalue.Decode(body)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					actual = jsonvalue.TextValue(body)
				}
				got, err := actual.Sorted().MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				want, err := row.Response.Sorted().MarshalJSON()
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("actual full handler body %s, Source %s: %v", got, want, err)
				}
				if headers := teamlessHeaders(response.Header()); !reflect.DeepEqual(headers, row.Headers) {
					t.Fatalf("actual handler headers %+v, Source %+v", headers, row.Headers)
				}
				after, err := os.ReadFile(mailbox)
				if err != nil || !bytes.Equal(before, after) || teamlessSHA256(after) != row.MailboxAfterSHA256 {
					t.Fatal("actual HTTP view changed the unread mailbox", err)
				}
				if provider.Calls() != 0 {
					t.Fatal("team view invoked the model")
				}
			}
			switch row.State {
			case "missing-key", "explicit-none", "detached-agent":
				if row.ActualPeekCalls != 0 {
					t.Fatal("Source no-team branch called the real peek")
				}
				// Missing key, None and no Agent share the typed absent-identity
				// projection. This is not a native public Agent-detach API claim.
				agent.WithTeamlessBindingForTest(t, manager, "alice", ids["session-alice"], observe)
			case "default-managed", "restored-managed":
				if row.ActualPeekCalls != 1 {
					t.Fatal("Source control did not execute the real peek")
				}
				observe()
			default:
				t.Fatalf("unknown Source assembly %q", row.State)
			}
			// The test-only helper restores before the next observation and
			// before cleanup. Default production construction stays a lead team.
			if view, err := manager.PeekTeam(context.Background(), "alice", ids["session-alice"]); row.Name != "teamless-corrupt-mailbox" && row.Name != "restored-corrupt-mailbox" && (err != nil || view.Identity == nil || string(view.Identity.Team) != string(ids["session-alice"])) {
				t.Fatal("controlled no-team test failed to restore default identity", view, err)
			}
		})
	}
	if len(seen) != 12 || manager.Summary().Sessions != fixture.SessionsCreated || provider.Calls() != 0 {
		t.Fatal("read-only handler changed fleet/model state")
	}
}
