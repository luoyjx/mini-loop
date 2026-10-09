package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

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
