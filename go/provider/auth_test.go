package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestExplicitAuthHeadersMatchSourceRecipes(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SourceCases []struct {
			Name        string
			Key         *string
			Environment map[string]string
			Calls       []struct{ Headers map[string]string }
		} `json:"source_cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.SourceCases) != 10 {
		t.Fatal("Source auth inventory changed")
	}
	for _, row := range fixture.SourceCases {
		for _, stream := range []bool{false, true} {
			t.Run(row.Name+fmt.Sprint(stream), func(t *testing.T) {
				key, token := row.Environment["ANTHROPIC_API_KEY"], row.Environment["ANTHROPIC_AUTH_TOKEN"]
				if row.Key != nil && *row.Key != "" {
					key = *row.Key
					token = ""
				}
				var headers []Header
				for _, line := range strings.Split(row.Environment["ANTHROPIC_CUSTOM_HEADERS"], "\n") {
					name, value, ok := strings.Cut(line, ":")
					if ok {
						headers = append(headers, Header{strings.TrimSpace(name), strings.TrimSpace(value)})
					}
				}
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					got := make(map[string]string)
					for _, field := range []string{"x-api-key", "authorization", "x-fixture"} {
						if values, ok := r.Header[http.CanonicalHeaderKey(field)]; ok {
							got[field] = values[0]
						}
					}
					if !reflect.DeepEqual(got, row.Calls[0].Headers) {
						t.Errorf("headers differ: %v", got)
					}
					w.Header().Set("request-id", "env-token custom-token custom-key explicit-key env-key")
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"env-token custom-token custom-key explicit-key env-key"}}`)
				}))
				defer server.Close()
				cfg := Config{BaseURL: server.URL, APIKey: key, AuthToken: token, Headers: headers}
				request := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("hello")}}}
				client, err := New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				// Caller edits after construction must not alter the captured authority.
				for i := range headers {
					headers[i].Value = "changed"
				}
				var failure error
				if stream {
					sc := &StreamingClient{client: client}
					_, failure = sc.CompleteStream(context.Background(), request, func(protocol.StreamDelta) error { return nil })
				} else {
					_, failure = client.Complete(context.Background(), request)
				}
				var f *Failure
				if calls != 1 || !errors.As(failure, &f) || f.Kind != FailureStatus {
					t.Fatal("auth did not reach transport", calls, failure)
				}
				for _, credential := range []string{key, token} {
					if credential != "" && strings.Contains(f.Message+f.RequestID, credential) {
						t.Fatal("static credential leaked")
					}
				}
				for _, header := range client.headers {
					if strings.EqualFold(header.Name, "Authorization") || strings.EqualFold(header.Name, "X-Api-Key") {
						value := strings.TrimPrefix(header.Value, "Bearer ")
						if value != "" && strings.Contains(f.Message+f.RequestID, value) {
							t.Fatal("header credential leaked")
						}
					}
				}
			})
		}
	}
}

func TestAuthHeaderAdmission(t *testing.T) {
	for _, cfg := range []Config{{AuthToken: "bad\r\ntoken"}, {Headers: []Header{{Name: "bad:name", Value: "value"}}}, {Headers: []Header{{Name: "Authorization", Value: "bad\nvalue"}}}} {
		if _, err := New(cfg); err == nil {
			t.Fatal("invalid authentication accepted")
		}
	}
	client, err := New(Config{Headers: []Header{{Name: "Authorization", Value: ""}}})
	if err != nil {
		t.Fatal(err)
	}
	if client.requireCredential() == nil {
		t.Fatal("empty header granted authentication")
	}
}

type authRetrySentinel struct{ t *testing.T }

func (s authRetrySentinel) Wait(context.Context, time.Duration) error {
	s.t.Fatal("missing credentials entered retry waiting")
	return nil
}

func TestMissingCredentialMatchesPinnedSDKPhasesWithoutHTTP(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-auth.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SDKVersion string `json:"sdk_version"`
		Cases      []struct {
			Name      string
			Key       *string
			Stream    bool
			MaxTokens int `json:"max_tokens"`
			Fault     struct{ Type, Message string }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SDKVersion != PythonSDKBaseline || len(fixture.Cases) != 4 {
		t.Fatal("SDK corpus changed", fixture.SDKVersion)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			cfg := Config{BaseURL: server.URL, Waiter: authRetrySentinel{t}}
			if row.Key != nil {
				cfg.APIKey = *row.Key
			}
			client, err := New(cfg)
			if err != nil {
				t.Fatal("constructor rejected absent credentials", err)
			}
			if client.Describe().Credential != "<absent>" || client.Describe().Endpoint != server.URL || strings.Contains(client.String(), "[REDACTED]") {
				t.Fatal("absence corrupts description", client.Describe())
			}
			if client.clean("unchanged diagnostic", 500) != "unchanged diagnostic" {
				t.Fatal("empty key corrupts diagnostics")
			}
			request := minimalRequest()
			request.Model = "claude-sonnet-4-6"
			request.MaxTokens = row.MaxTokens
			if row.Stream {
				stream, makeErr := NewStreaming(cfg)
				if makeErr != nil {
					t.Fatal(makeErr)
				}
				_, err = stream.CompleteStream(context.Background(), request, func(protocol.StreamDelta) error { t.Fatal("missing credentials emitted stream output"); return nil })
			} else {
				_, err = client.Complete(context.Background(), request)
			}
			var failure *Failure
			if !errors.As(err, &failure) || string(failure.Class()) != row.Fault.Type || failure.Message != row.Fault.Message || calls.Load() != 0 {
				t.Fatal("SDK phase mismatch", err, calls.Load(), row.Fault)
			}
			if row.Name != "ceiling-first" {
				view := failure.RecoveryFailure()
				if view.Kind != protocol.ModelFailureOther || view.Class != "TypeError" || view.Status != 0 || view.RetryAfterSeconds != nil {
					t.Fatal("credential refusal became retryable transport/status failure", view)
				}
			}
		})
	}
}
