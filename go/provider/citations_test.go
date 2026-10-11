package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type citationFixture struct {
	Completed []struct {
		Name                         string
		Input, Normalized, Forwarded json.RawMessage
	}
	Streams []struct {
		Name      string
		Events    []json.RawMessage
		Content   json.RawMessage
		Forwarded json.RawMessage
		Requests  int
		Observed  []struct{ Type, Text string }
	}
}

func citationContracts(t *testing.T) citationFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-provider-citations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture citationFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Completed) != 35 || len(fixture.Streams) != 2 {
		t.Fatal("incomplete Source capture")
	}
	return fixture
}
func equalCitationJSON(t *testing.T, actual, expected []byte) {
	t.Helper()
	decode := func(data []byte) any {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(actual), decode(expected)) {
		t.Fatalf("citation payload differs:\n%s\nwant:\n%s", actual, expected)
	}
}
func citationReply(block json.RawMessage) string {
	return fmt.Sprintf(`{"id":"source-citation","type":"message","role":"assistant","model":"citation-model","content":[%s],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":2}}`, block)
}
func TestCitationHTTPNormalizationAndForwardingMatchesSDK(t *testing.T) {
	for _, spec := range citationContracts(t).Completed {
		t.Run(spec.Name, func(t *testing.T) {
			calls := 0
			client, err := New(Config{APIKey: "fixture-key", BaseURL: "https://provider.invalid", HTTPClient: &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				data, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				if calls == 2 {
					var wire struct {
						Messages []struct{ Content []json.RawMessage }
					}
					// First user rows use plain strings; inspect only the second assistant row.
					var envelope struct{ Messages []json.RawMessage }
					if err := json.Unmarshal(data, &envelope); err != nil {
						t.Fatal(err)
					}
					var assistant struct{ Content []json.RawMessage }
					if err := json.Unmarshal(envelope.Messages[1], &assistant); err != nil {
						t.Fatal(err)
					}
					equalCitationJSON(t, assistant.Content[0], spec.Forwarded)
					_ = wire
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(citationReply(spec.Input)))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			request := minimalRequest()
			reply, err := client.Complete(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(reply.Content[0])
			if err != nil {
				t.Fatal(err)
			}
			equalCitationJSON(t, encoded, spec.Normalized)
			request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("next")})
			if _, err := client.Complete(context.Background(), request.Clone()); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatal("unexpected retries")
			}
		})
	}
}
func citationSSE(t *testing.T, events []json.RawMessage) string {
	t.Helper()
	var wire strings.Builder
	for _, raw := range events {
		var event struct{ Type string }
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&wire, "event: %s\ndata: %s\n\n", event.Type, compact.Bytes())
	}
	return wire.String()
}
func TestCitationStreamRetainsInitialAndDeltaMetadataWithoutTextProgress(t *testing.T) {
	for _, spec := range citationContracts(t).Streams {
		t.Run(spec.Name, func(t *testing.T) {
			calls := 0
			client, err := NewStreaming(Config{APIKey: "fixture-key", BaseURL: "https://provider.invalid", HTTPClient: &http.Client{Transport: transportFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 2 {
					data, err := io.ReadAll(request.Body)
					if err != nil {
						t.Fatal(err)
					}
					var envelope struct{ Messages []json.RawMessage }
					if err := json.Unmarshal(data, &envelope); err != nil {
						t.Fatal(err)
					}
					var assistant struct{ Content json.RawMessage }
					if err := json.Unmarshal(envelope.Messages[1], &assistant); err != nil {
						t.Fatal(err)
					}
					equalCitationJSON(t, assistant.Content, spec.Forwarded)
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(citationReply(json.RawMessage(`{"type":"text","text":"next"}`))))}, nil
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(citationSSE(t, spec.Events)))}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			var progress []protocol.StreamDelta
			request := minimalRequest()
			reply, err := client.CompleteStream(context.Background(), request, func(delta protocol.StreamDelta) error { progress = append(progress, delta); return nil })
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(reply.Content)
			if err != nil {
				t.Fatal(err)
			}
			equalCitationJSON(t, encoded, spec.Content)
			request.Messages = append(request.Messages, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("next")})
			if _, err := client.client.Complete(context.Background(), request.Clone()); err != nil {
				t.Fatal(err)
			}
			if calls != spec.Requests {
				t.Fatal("request count differs", calls, spec.Requests)
			}
			var expected []protocol.StreamDelta
			for _, event := range spec.Observed {
				if event.Type == "text" {
					expected = append(expected, protocol.StreamDelta{Kind: protocol.DeltaText, Text: event.Text})
				}
			}
			if !reflect.DeepEqual(progress, expected) {
				t.Fatalf("citation created text progress: %#v / %#v", progress, expected)
			}
		})
	}
}
func TestCitationStreamRefusesInvalidOwnershipAndBoundsAccumulatedMetadata(t *testing.T) {
	client, _ := New(Config{APIKey: "fixture-key"})
	seed := citationContracts(t).Streams[1]
	var stream streamAssembly
	for _, raw := range seed.Events[:2] {
		var event struct{ Type string }
		json.Unmarshal(raw, &event)
		if err := client.consumeStream(&stream, event.Type, raw, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, data := range []string{
		`{"type":"content_block_delta","index":0,"delta":{"type":"citations_delta"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"citations_delta","citation":null}}`,
	} {
		if err := client.consumeStream(&stream, "content_block_delta", []byte(data), nil); err == nil {
			t.Fatal("invalid citation delta accepted")
		}
	}
	stream.blocks[0].kind = protocol.BlockThinking
	if err := client.consumeStream(&stream, "content_block_delta", seed.Events[3], nil); err == nil {
		t.Fatal("thinking accepted citation delta")
	}
	stream.blocks[0].kind = protocol.BlockText
	opaque, _ := protocol.DecodeCitations([]byte(`"opaque"`))
	stream.blocks[0].citations = opaque
	if err := client.consumeStream(&stream, "content_block_delta", seed.Events[3], nil); err == nil {
		t.Fatal("non-array citation append accepted")
	}
	empty, _ := protocol.DecodeCitations([]byte("null"))
	stream.blocks[0].citations = empty
	stream.blocks[0].citationBytes = protocol.MaxWireBytes
	err := client.consumeStream(&stream, "content_block_delta", seed.Events[3], nil)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Kind != FailureLimit {
		t.Fatal("citation aggregate limit bypassed", err)
	}
}
