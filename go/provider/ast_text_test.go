package provider

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestASTResultTextSourceSDKEncodingBoundary(t *testing.T) {
	var fixture struct {
		Invocations []struct {
			Name          string
			TextHex       string `json:"text_hex"`
			UTF8          bool
			ProviderError string `json:"provider_error"`
			ProviderCalls int    `json:"provider_calls"`
		}
	}
	raw, err := os.ReadFile("../testdata/python-ast-text.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Invocations {
		t.Run(row.Name, func(t *testing.T) {
			data, err := hex.DecodeString(row.TextHex)
			if err != nil {
				t.Fatal(err)
			}
			request := minimalRequest()
			request.Messages = []protocol.Message{
				{Role: protocol.RoleUser, Content: protocol.PlainContent("inspect")},
				{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewBashUse("ast", "true"))},
				{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("ast", string(data), false))},
			}
			calls := 0
			httpClient := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var sent struct{ Messages []protocol.Message }
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Fatal(err)
				}
				blocks, _ := sent.Messages[len(sent.Messages)-1].Content.Blocks()
				result, ok := blocks[0].ToolResult()
				if !ok || result.Content != string(data) {
					t.Fatal("provider wire changed Source tool text")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(goodReply)), Request: r}, nil
			})}
			client, err := New(Config{APIKey: "fixture-key", HTTPClient: httpClient})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Complete(context.Background(), request)
			if row.UTF8 {
				if err != nil || row.ProviderError != "" {
					t.Fatal(err, row.ProviderError)
				}
			} else {
				var failure *Failure
				if row.ProviderError != "UnicodeEncodeError" || !errors.As(err, &failure) || failure.Kind != FailureProtocol {
					t.Fatal("encoding refusal differs", err, row.ProviderError)
				}
				block := protocol.NewToolResult("ast", string(data), false)
				if _, err = json.Marshal(block); !errors.Is(err, protocol.ErrResultUTF8) {
					t.Fatal("wire replaced surrogate", err)
				}
				stream, _ := NewStreaming(Config{APIKey: "fixture-key", HTTPClient: httpClient})
				_, err = stream.CompleteStream(context.Background(), request, nil)
				if !errors.As(err, &failure) || failure.Kind != FailureProtocol {
					t.Fatal("stream encoding sent surrogate", err)
				}
			}
			if calls != row.ProviderCalls {
				t.Fatal("HTTP requests differ", calls, row.ProviderCalls)
			}
		})
	}
}
