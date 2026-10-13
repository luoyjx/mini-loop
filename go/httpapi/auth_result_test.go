package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
)

type authenticationResultSpy struct {
	result      AuthenticationResult
	resultCalls atomic.Int32
	boolCalls   atomic.Int32
}

var _ ResultAuthenticator = (*authenticationResultSpy)(nil)

func (*authenticationResultSpy) Configured() bool { return true }

func (auth *authenticationResultSpy) Authenticate(string) (Principal, bool) {
	auth.boolCalls.Add(1)
	return Principal{ID: "bob"}, true
}

func (auth *authenticationResultSpy) AuthenticateResult(string) AuthenticationResult {
	if auth.resultCalls.Add(1) != 1 {
		return AuthenticationResult{Disposition: AuthenticationAccepted, Principal: Principal{ID: "bob"}}
	}
	return auth.result
}

func TestAuthenticationResultAdmitsOnePrincipalOverTCP(t *testing.T) {
	cases := []struct {
		name   string
		result AuthenticationResult
		status int
	}{
		{"accepted", AuthenticationResult{Disposition: AuthenticationAccepted, Principal: Principal{ID: "alice"}}, http.StatusOK},
		{"failed-with-principal", AuthenticationResult{
			Disposition: AuthenticationFailed,
			Principal:   Principal{ID: "alice"},
			Failure:     &AuthenticationFailure{ExceptionClass: AuthenticationTypeError, Message: "private authentication diagnostic"},
		}, http.StatusInternalServerError},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			provider := &countedProvider{}
			manager := testManager(t, provider)
			session, err := manager.Create(context.Background(), agent.CreateSessionRequest{Owner: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			auth := &authenticationResultSpy{result: item.result}
			server := httptest.NewServer(testServer(t, Config{Manager: manager, Auth: auth}))
			defer server.Close()
			client := server.Client()
			defer client.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sessions/"+string(session.ID()), nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer typed-spy")
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(io.LimitReader(response.Body, (16<<10)+1))
			if err != nil || len(body) > 16<<10 {
				t.Fatal("bounded response read failed", err, len(body))
			}
			if auth.resultCalls.Load() != 1 || auth.boolCalls.Load() != 0 {
				t.Fatalf("authentication calls: typed=%d bool=%d", auth.resultCalls.Load(), auth.boolCalls.Load())
			}
			if response.StatusCode != item.status {
				t.Fatalf("status %d, want %d: %s", response.StatusCode, item.status, body)
			}
			if item.status == http.StatusOK {
				info := decode[SessionInfo](t, body)
				if info.ID != session.ID() {
					t.Fatal("owned GET did not retain the admitted identity", info)
				}
			} else {
				if string(body) != "Internal Server Error" {
					t.Fatalf("fault response exposed a diagnostic or reached the owned handler: %q", body)
				}
				plain := "text/plain; charset=utf-8"
				if headers := querySelectionReadHeaders(response.Header, nil); !reflect.DeepEqual(headers, querySelectionHeaders{ContentType: &plain}) {
					t.Fatalf("authentication fault application headers = %+v", headers)
				}
				if response.Header.Get("Content-Length") != strconv.Itoa(len(body)) {
					t.Fatal("authentication fault length changed", response.Header)
				}
				for name := range response.Header {
					// Date is synthesized by the TCP HTTP server, outside the
					// Source ASGI application's captured response headers.
					if name != "Content-Type" && name != "Content-Length" && name != "Date" {
						t.Fatal("unexpected authentication fault header", name, response.Header.Values(name))
					}
				}
			}
			if provider.calls.Load() != 0 || session.Info().RunCount != 0 || len(session.Messages()) != 0 {
				t.Fatal("authentication/read-only GET performed a model turn")
			}
		})
	}
}

func TestAdmissionCachesQueriesWithoutBearer(t *testing.T) {
	caller := Principal{ID: "alice"}
	query := parseScalarQuery("access_token=private-bearer&limit=2&envelope=&session_id=&format=jsonl&epoch=2")
	ctx := withAdmission(context.Background(), caller, query)
	retained := admittedQueries(ctx)
	if retained.AccessToken.Present || retained.AccessToken.Text != "" {
		t.Fatal("handler admission retained the fallback credential")
	}
	if principal(ctx) != caller || retained.Limit != (scalarQueryValue{Text: "2", Present: true}) || retained.Envelope != (scalarQueryValue{Present: true}) || retained.SessionID != (scalarQueryValue{Present: true}) || retained.Format != (scalarQueryValue{Text: "jsonl", Present: true}) || retained.Epoch != (scalarQueryValue{Text: "2", Present: true}) {
		t.Fatal("credential removal changed admitted identity or selected query presence", principal(ctx), retained)
	}
	withoutQueries := withAdmission(context.Background(), caller, scalarQueryParameters{})
	if principal(withoutQueries) != caller || admittedQueries(withoutQueries) != (scalarQueryParameters{}) {
		t.Fatal("admission introduced absent query values")
	}
}

func TestTokenAuthenticationDoesNotAcceptBeforeLaterUnicodeBinding(t *testing.T) {
	// Native control for Source TokenAuth's full comparison loop. The raw-query
	// Source corpus captures a non-ASCII presented token, not this configured-
	// Unicode binding profile; do not label this an actual Source capture.
	auth, err := NewTokenAuth([]TokenBinding{{Token: "known", Principal: "alice"}, {Token: "中", Principal: "bob"}})
	if err != nil {
		t.Fatal(err)
	}
	result := auth.AuthenticateResult("Bearer known")
	if result.Disposition != AuthenticationFailed || result.Principal != (Principal{}) || result.Failure == nil || result.Failure.ExceptionClass != AuthenticationTypeError || result.Failure.Message != "comparing strings with non-ASCII characters is not supported" {
		t.Fatal("an earlier matching binding hid the later comparison fault", result)
	}
	if caller, ok := auth.Authenticate("Bearer known"); ok || caller != (Principal{}) {
		t.Fatal("legacy bool projection granted a faulted authentication", caller, ok)
	}
	if result := auth.AuthenticateResult("Basic known"); result.Disposition != AuthenticationRejected || result.Failure != nil {
		t.Fatal("invalid authorization scheme reached token comparison", result)
	}
}
