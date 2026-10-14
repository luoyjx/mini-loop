package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type retainedBatchTransport struct{ calls int }

func (transport *retainedBatchTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: 400, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"invalid_request_error","message":"fixture\\credential ` + strings.Repeat("界", 600) + `"}}`)), Request: request}, nil
}

func TestRetainedBatchHTTPDispatchKeepsOrdinaryValidationAndScrubsDiagnostics(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		name := "direct"
		if streaming {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			request := minimalRequest()
			request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("retained", "pwd"))})
			batch, err := protocol.ObserveUnpublishedToolBatch(request.Messages)
			if err != nil {
				t.Fatal(err)
			}
			request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("follow-up")})
			transport := &retainedBatchTransport{}
			zero := 0
			config := Config{APIKey: `fixture\credential`, BaseURL: "https://fixture.invalid", HTTPClient: &http.Client{Transport: transport}, MaxRetries: &zero}
			client, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			complete := client.Complete
			if streaming {
				stream := &StreamingClient{client: client}
				complete = stream.Complete
			}
			if _, err := complete(context.Background(), request); err == nil || transport.calls != 0 {
				t.Fatal("ordinary malformed request reached transport")
			}
			observed, err := request.WithUnpublishedToolBatch(batch)
			if err != nil {
				t.Fatal(err)
			}
			if observed.Validate() == nil {
				t.Fatal("public validation accepted the pairing gap")
			}
			_, err = complete(context.Background(), observed)
			var failure *Failure
			if !errors.As(err, &failure) || failure.Status != 400 || transport.calls != 1 {
				t.Fatalf("registered request failed to reach HTTP: %v; calls %d", err, transport.calls)
			}
			detail, ok := failure.ProviderErrorDetail()
			if !ok || !strings.HasPrefix(detail, "BadRequestError: Error code: 400 - ") || strings.Contains(detail, `fixture\credential`) || strings.Contains(detail, `fixture\\credential`) || !strings.Contains(detail, "[REDACTED]") || utf8.RuneCountInString(detail) > 500 {
				t.Fatalf("unsafe/unbounded SDK diagnostic: %q", detail)
			}
			observed.Messages[0] = protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("tampered prefix")}
			if _, err := complete(context.Background(), observed); err == nil || transport.calls != 1 {
				t.Fatal("tampered observed history reached HTTP")
			}
		})
	}
}
