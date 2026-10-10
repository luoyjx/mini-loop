package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestFederationExchangeMatchesSourceRecipes(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-provider-auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Spec struct {
				Name           string
				IdentityFile   *string `json:"identity_file"`
				RotateIdentity string  `json:"rotate_identity"`
				AdvanceClock   int64   `json:"advance_clock"`
				TokenStatus    *int    `json:"token_status"`
				ModelStatuses  []int   `json:"model_statuses"`
			}
			Constructed bool
			Environment map[string]string
			Requests    []struct {
				URL     string
				Headers map[string]string
			}
			Exchanges []struct {
				URL, Beta string
				Body      jsonvalue.Value
			}
			Fault *struct{ Type string }
			Texts []string
		} `json:"discovery_cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{"federation-environment": true, "federation-file-before-literal": true, "federation-refuses-http": true, "federation-exchange-failure": true, "federation-model-401": true, "federation-file-rotation": true}
	tested := 0
	for _, row := range fixture.Cases {
		if !selected[row.Spec.Name] {
			continue
		}
		tested++
		t.Run(row.Spec.Name, func(t *testing.T) {
			// Replay exchange behavior from Source; this explicit test harness does not
			// stand in for the still-pending Launcher discovery chain.
			var identity IdentityTokenProvider
			path := ""
			if row.Spec.IdentityFile != nil {
				path = filepath.Join(t.TempDir(), "identity.jwt")
				if err := os.WriteFile(path, []byte(*row.Spec.IdentityFile+"\n"), 0644); err != nil {
					t.Fatal(err)
				}
				identity, err = NewIdentityTokenFile(path)
			} else {
				identity = NewLiteralIdentityToken(row.Environment["ANTHROPIC_IDENTITY_TOKEN"])
			}
			if err != nil {
				t.Fatal(err)
			}
			exchanges, requests := 0, 0
			httpClient := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				status := 200
				body := strings.ReplaceAll(goodReply, "done", "ok")
				if strings.HasSuffix(r.URL.Path, "/oauth/token") {
					if exchanges >= len(row.Exchanges) {
						t.Fatal("unexpected token exchange")
					}
					expected := row.Exchanges[exchanges]
					exchanges++
					wire, _ := io.ReadAll(r.Body)
					want, _ := expected.Body.MarshalJSON()
					var actual, source map[string]jsonvalue.Value
					if err := json.Unmarshal(wire, &actual); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(want, &source); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(actual, source) || r.URL.String() != expected.URL || r.Header.Get("Anthropic-Beta") != expected.Beta {
						t.Fatal("Source exchange differs")
					}
					body = `{"access_token":"exchanged-token","expires_in":3600,"token_type":"Bearer"}`
					if row.Spec.TokenStatus != nil {
						status = *row.Spec.TokenStatus
						body = `{"error":"invalid_grant","access_token":"must-redact"}`
					}
				} else {
					if requests >= len(row.Requests) {
						t.Fatal("unexpected model request")
					}
					expected := row.Requests[requests]
					requests++
					headers := map[string]string{}
					for _, key := range []string{"authorization", "anthropic-beta", "anthropic-workspace-id", "x-api-key"} {
						if value := r.Header.Get(key); value != "" {
							headers[key] = value
						}
					}
					if !reflect.DeepEqual(headers, expected.Headers) || r.URL.String() != expected.URL {
						t.Fatal("Source model authentication differs")
					}
					if len(row.Spec.ModelStatuses) > 0 {
						status = row.Spec.ModelStatuses[requests-1]
					}
					if status != 200 {
						body = `{"error":{"type":"authentication_error","message":"exchanged-token"}}`
					}
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			now := time.Unix(1700000000, 0)
			clock := func() time.Time { return now }
			driver, err := NewFederation(FederationConfig{Identity: identity, BaseURL: row.Environment["ANTHROPIC_BASE_URL"], FederationRuleID: row.Environment["ANTHROPIC_FEDERATION_RULE_ID"], OrganizationID: row.Environment["ANTHROPIC_ORGANIZATION_ID"], HTTPClient: httpClient, Now: clock})
			if (err == nil) != row.Constructed {
				t.Fatal("Source construction phase differs", err)
			}
			if err != nil {
				return
			}
			client, err := New(Config{BaseURL: driver.ModelBaseURL(), Credentials: driver, HTTPClient: httpClient, Now: clock})
			if err != nil {
				t.Fatal(err)
			}
			var texts []string
			var failure error
			for turn := 0; turn < 2; turn++ {
				if turn == 1 && row.Spec.RotateIdentity != "" {
					if err := os.WriteFile(path, []byte(row.Spec.RotateIdentity+"\n"), 0644); err != nil {
						t.Fatal(err)
					}
					now = now.Add(time.Duration(row.Spec.AdvanceClock) * time.Second)
				}
				reply, callErr := client.Complete(context.Background(), minimalRequest())
				failure = callErr
				if failure != nil {
					break
				}
				block, _ := reply.Content[0].Text()
				texts = append(texts, block.Text)
			}
			if row.Fault != nil {
				var typed *Failure
				if !errors.As(failure, &typed) || string(typed.Class()) != row.Fault.Type || strings.Contains(failure.Error(), "must-redact") || strings.Contains(failure.Error(), "exchanged-token") {
					t.Fatal("Source error class/redaction differs", failure)
				}
			} else if failure != nil {
				t.Fatal(failure)
			}
			if requests != len(row.Requests) || exchanges != len(row.Exchanges) || !reflect.DeepEqual(texts, nullableTexts(row.Texts)) {
				t.Fatal("Source cache/call/text evidence differs", requests, exchanges, texts)
			}
		})
	}
	if tested != len(selected) {
		t.Fatal("Source federation cases missing")
	}
}
func nullableTexts(texts []string) []string {
	if len(texts) == 0 {
		return nil
	}
	return texts
}

func TestFederationOptionalFieldsCaptureAndFreshIdentity(t *testing.T) {
	var calls atomic.Int32
	service, workspace, scope := "", "workspace", "informational"
	assertion := NewLiteralIdentityToken("jwt-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["workspace_id"] != "workspace" || body["service_account_id"] != "" || body["assertion"] != "jwt-secret" || r.Header.Get("Anthropic-Beta") != OAuthAPIBeta+","+FederationAPIBeta {
			t.Error("captured fields differ")
		}
		if _, exists := body["service_account_id"]; !exists {
			t.Error("explicit empty optional field lost")
		}
		if _, exists := body["scope"]; exists {
			t.Error("informational scope sent")
		}
		w.WriteHeader(201)
		io.WriteString(w, `{"access_token":"native-token","expires_in":"3_600","token_type":"bEaReR"}`)
	}))
	defer server.Close()
	driver, err := NewFederation(FederationConfig{Identity: assertion, BaseURL: server.URL + "/", FederationRuleID: "rule", OrganizationID: "org", ServiceAccountID: &service, WorkspaceID: &workspace, Scope: &scope, Now: func() time.Time { return time.Unix(1700000000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	service, workspace, scope = "changed", "changed", "changed"
	returnedScope := driver.Scope()
	*returnedScope = "changed-again"
	for _, force := range []bool{false, true} {
		token, err := driver.FetchToken(context.Background(), force)
		if err != nil || token.value != "native-token" || token.expiry.Unix() != 1700003600 {
			t.Fatal("fresh exchange failed", err)
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", driver, token), "native-token") {
			t.Fatal("formatting leaked token")
		}
	}
	if calls.Load() != 2 || *driver.Scope() != "informational" {
		t.Fatal("driver unexpectedly cached/retained caller pointers")
	}
}

func TestIdentityFileProjectedSymlinksAndBounds(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "token")
	first := filepath.Join(root, "one")
	if err := os.WriteFile(first, []byte("\x1c jwt.one \n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	file, err := NewIdentityTokenFile(link)
	if err != nil {
		t.Fatal(err)
	}
	token, err := file.IdentityToken(context.Background())
	if err != nil || token.value != "jwt.one" {
		t.Fatal("projected symlink failed", err)
	}
	replacement := filepath.Join(root, "two")
	if err := os.WriteFile(replacement, []byte("jwt.two"), 0644); err != nil {
		t.Fatal(err)
	}
	rotated := filepath.Join(root, "next")
	if err := os.Symlink(replacement, rotated); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(rotated, link); err != nil {
		t.Fatal(err)
	}
	token, err = file.IdentityToken(context.Background())
	if err != nil || token.value != "jwt.two" {
		t.Fatal("projected file rotation not reread", err)
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", file, token), "jwt.two") {
		t.Fatal("identity formatting leaked")
	}
	for _, value := range []string{"   ", string([]byte{255}), strings.Repeat("a", MaxIdentityAssertionBytes+1)} {
		if err := os.WriteFile(replacement, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := file.IdentityToken(context.Background()); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	if _, err := NewIdentityAssertion(strings.Repeat("é", MaxIdentityAssertionBytes/2)); err != nil {
		t.Fatal("UTF-8 byte boundary rejected", err)
	}
	if _, err := NewIdentityAssertion(strings.Repeat("é", MaxIdentityAssertionBytes/2+1)); err == nil {
		t.Fatal("UTF-8 byte bound ignored")
	}
}

func TestFederationMalformedResponsesAndWireLimits(t *testing.T) {
	assertion := NewLiteralIdentityToken("jwt-secret")
	for _, body := range []string{`{`, `[]`, `{}`, `{"access_token":"secret"}`, `{"access_token":"secret","expires_in":null}`, `{"access_token":"secret","expires_in":1e100}`, `{"access_token":"secret","expires_in":1,"token_type":"MAC"}`, `{"access_token":"bad\nheader","expires_in":1}`, strings.Repeat(" ", MaxFederationResponseBytes+1)} {
		t.Run(fmt.Sprint(len(body))+body[:min(len(body), 24)], func(t *testing.T) {
			driver, err := NewFederation(FederationConfig{Identity: assertion, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = driver.FetchToken(context.Background(), false)
			var typed *CredentialError
			if !errors.As(err, &typed) || typed.Kind != CredentialExchange || typed.Status != 200 || strings.Contains(err.Error(), "secret") {
				t.Fatal("bad exchange not contained", err)
			}
		})
	}
	if _, err := NewFederation(FederationConfig{Identity: assertion, BaseURL: "http://localhost.evil"}); err == nil {
		t.Fatal("remote HTTP accepted")
	}
}

type identityFunc func(context.Context) (IdentityAssertion, error)

func (f identityFunc) IdentityToken(ctx context.Context) (IdentityAssertion, error) { return f(ctx) }

func TestFederationIdentityCallbackAndTransportCancellation(t *testing.T) {
	for _, identity := range []IdentityTokenProvider{
		identityFunc(func(context.Context) (IdentityAssertion, error) {
			return IdentityAssertion{}, errors.New("identity-secret")
		}),
		identityFunc(func(context.Context) (IdentityAssertion, error) { panic("identity-secret") }),
	} {
		driver, err := NewFederation(FederationConfig{Identity: identity})
		if err != nil {
			t.Fatal(err)
		}
		_, err = driver.FetchToken(context.Background(), false)
		var typed *CredentialError
		if !errors.As(err, &typed) || typed.Kind != CredentialCallback || strings.Contains(err.Error(), "identity-secret") {
			t.Fatal("identity callback leaked", err)
		}
	}
	assertion := NewLiteralIdentityToken("assertion")
	driver, err := NewFederation(FederationConfig{Identity: assertion, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := driver.FetchToken(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("exchange ignored cancellation", err)
	}
}

func TestFederationStreamingModel401KeepsCachedToken(t *testing.T) {
	var exchanges, models atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth/token") {
			exchanges.Add(1)
			io.WriteString(w, `{"access_token":"stream-token","expires_in":3600}`)
			return
		}
		models.Add(1)
		if r.Header.Get("Authorization") != "Bearer stream-token" || r.Header.Get("Anthropic-Beta") != OAuthAPIBeta {
			t.Error("stream auth differs")
		}
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"stream-token"}}`)
	}))
	defer server.Close()
	assertion := NewLiteralIdentityToken("assertion")
	driver, err := NewFederation(FederationConfig{Identity: assertion, BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewStreaming(Config{BaseURL: driver.ModelBaseURL(), Credentials: driver})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err := client.CompleteStream(context.Background(), minimalRequest(), nil)
		var failure *Failure
		if !errors.As(err, &failure) || failure.Status != 401 || strings.Contains(failure.Message, "stream-token") {
			t.Fatal("stream credential failure escaped", err)
		}
	}
	if exchanges.Load() != 1 || models.Load() != 2 {
		t.Fatal("model 401 refreshed/replayed federation", exchanges.Load(), models.Load())
	}
}

func TestFederationLiteralAdmissionPhaseAndRedirectIsolation(t *testing.T) {
	var destination atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/destination" {
			destination.Add(1)
			return
		}
		w.Header().Set("Location", "/destination")
		w.WriteHeader(307)
	}))
	defer server.Close()
	supplied := &http.Client{}
	driver, err := NewFederation(FederationConfig{Identity: NewLiteralIdentityToken("jwt-secret"), BaseURL: server.URL, HTTPClient: supplied})
	if err != nil {
		t.Fatal(err)
	}
	_, err = driver.FetchToken(context.Background(), false)
	var failure *CredentialError
	if !errors.As(err, &failure) || failure.Status != 307 || destination.Load() != 0 || supplied.CheckRedirect != nil {
		t.Fatal("redirect leaked/changed caller policy", err)
	}
	driver, err = NewFederation(FederationConfig{Identity: NewLiteralIdentityToken(strings.Repeat("a", MaxIdentityAssertionBytes+1)), BaseURL: server.URL})
	if err != nil {
		t.Fatal("oversize assertion must fail on request", err)
	}
	if _, err := driver.FetchToken(context.Background(), false); err == nil {
		t.Fatal("oversize literal exchanged")
	}
}
