package provider

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type surrogateRequest struct {
	Method, Path string
	BodyJSON     string `json:"body_json"`
}

type surrogateCase struct {
	Name, Mode          string
	ResponseJSON        string  `json:"response_json"`
	SSE                 string  `json:"sse"`
	IngressSucceeded    bool    `json:"ingress_succeeded"`
	SDKReplyObtained    bool    `json:"sdk_reply_obtained"`
	IngressErrorClass   *string `json:"ingress_error_class"`
	IngressErrorMessage *string `json:"ingress_error_message"`
	ContentJSON         *string `json:"content_json"`
	RetainedContentJSON *string `json:"retained_content_json"`
	ForwardMessagesJSON *string `json:"forward_messages_json"`
	ForwardSucceeded    *bool   `json:"forward_succeeded"`
	ForwardErrorClass   *string `json:"forward_error_class"`
	ForwardErrorMessage *string `json:"forward_error_message"`
	FailurePhase        *string `json:"failure_phase"`
	TransportRequests   int     `json:"transport_requests"`
	Requests            []surrogateRequest
	ProgressHex         []string `json:"progress_hex"`
}

type surrogateNegativePhase struct {
	phase, class  string
	replyObtained bool
}

var surrogateNegativePhases = map[string]surrogateNegativePhase{
	"stream/bash-command-high":        {"reply_ingress", "ValueError", false},
	"stream/bash-extra-value-low":     {"reply_ingress", "ValueError", false},
	"stream/bash-extra-key-high":      {"reply_ingress", "ValueError", false},
	"stream/server-input-low":         {"reply_ingress", "ValueError", false},
	"direct/caller-extension-key-low": {"reply_snapshot", "UnicodeEncodeError", true},
	"stream/caller-extension-key-low": {"reply_ingress", "UnicodeEncodeError", false},
}

// Inner JSON documents are ASCII escape strings. Go's encoding/json decodes
// only the outer fixture; the closed value decoder owns Unicode code points.
func surrogateValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	v, err := jsonvalue.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func surrogateEqual(t *testing.T, label string, actual, expected jsonvalue.Value) {
	t.Helper()
	if !actual.PythonEqual(expected) {
		a, _ := jsonvalue.AppendLegacy(nil, actual)
		b, _ := jsonvalue.AppendLegacy(nil, expected)
		t.Fatalf("%s: %s != %s", label, a, b)
	}
}

func surrogateField(t *testing.T, value jsonvalue.Value, key string) jsonvalue.Value {
	t.Helper()
	field, present := value.Lookup(key)
	if !present {
		t.Fatal("missing Source field", key)
	}
	return field
}

// Hex mapping makes a detached inspection projection UTF-8 safe without
// replacing or normalizing any actual live string, key, input or caller.
func surrogateHex(text string) string { return hex.EncodeToString([]byte(text)) }

func assertSurrogateContent(t *testing.T, blocks []protocol.Block, raw string) {
	t.Helper()
	expected, ok := surrogateValue(t, raw).Array()
	if !ok || len(expected) != len(blocks) {
		t.Fatal("SDK content count differs")
	}
	for i, block := range blocks {
		want := expected[i]
		ascii, err := block.MarshalObservationASCIIJSON()
		if err != nil {
			t.Fatal("escaped SDK snapshot", err)
		}
		surrogateEqual(t, "complete escaped SDK content", surrogateValue(t, string(ascii)), want)
		surrogateEqual(t, "kind", jsonvalue.TextValue(string(block.Kind())), surrogateField(t, want, "type"))
		textField := func(key, actual string) {
			t.Helper()
			surrogateEqual(t, key, jsonvalue.TextValue(actual), surrogateField(t, want, key))
		}
		switch block.Kind() {
		case protocol.BlockText:
			text, _ := block.Text()
			textField("text", text.Text)
			if _, present := want.Lookup("parsed_output"); present != text.ParsedOutputNull() {
				t.Fatal("SDK stream parsed-output default differs")
			}
		case protocol.BlockThinking:
			value, _ := block.Thinking()
			textField("thinking", value.Thinking)
			textField("signature", value.Signature)
		case protocol.BlockRedactedThinking:
			value, _ := block.RedactedThinking()
			textField("data", value.Data)
		case protocol.BlockContainerUpload:
			value, _ := block.ContainerUpload()
			textField("file_id", value.FileID)
		case protocol.BlockToolUse, protocol.BlockServerToolUse:
			var input protocol.InputObservation
			if value, ok := block.ToolUse(); ok {
				textField("id", value.ID)
				textField("name", string(value.Name))
				var err error
				input, err = value.Input.Observation()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				value, _ := block.ServerToolUse()
				textField("id", value.ID)
				textField("name", string(value.Name))
				input = value.Input.Observation()
			}
			mapped, err := input.MapStrings(surrogateHex).LegacyJSON()
			if err != nil {
				t.Fatal("detached input observation", err)
			}
			surrogateEqual(t, "input code points", surrogateValue(t, string(mapped)),
				surrogateField(t, want, "input").MapStrings(surrogateHex))
			caller := callerContractBlockCaller(block)
			wantCaller, present := want.Lookup("caller")
			if present && wantCaller.Kind() != jsonvalue.Null {
				if caller == nil {
					t.Fatal("missing SDK caller")
				}
				observed, ok := caller.SDKObservation()
				if !ok {
					t.Fatal("caller lost SDK observation")
				}
				surrogateEqual(t, "caller code points", observed.Value(), wantCaller)
			} else if caller != nil {
				t.Fatal("unexpected SDK caller")
			}
		default:
			t.Fatal("unmapped finite surrogate recipe", block.Kind())
		}
	}
}

const surrogateDoneJSON = `{"id":"surrogate-reply","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":2}}`
const surrogateDoneSSE = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"surrogate-reply\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"fixture-model\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"done\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func TestEscapedSurrogateIngressAndNextRequestMatchActualSDK(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-provider-surrogates.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SDKVersion      string   `json:"sdk_version"`
		PydanticVersion string   `json:"pydantic_version"`
		ProfileNames    []string `json:"profile_names"`
		Cases           []surrogateCase
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SDKVersion != "0.107.1" || fixture.PydanticVersion != "2.13.4" || len(fixture.ProfileNames) != 17 || len(fixture.Cases) != 34 {
		t.Fatal("fixed SDK surrogate inventory drift")
	}
	seen := make(map[string]bool)
	for _, row := range fixture.Cases {
		key := row.Mode + "/" + row.Name
		if seen[key] {
			t.Fatal("duplicate Source recipe", key)
		}
		seen[key] = true
		t.Run(key, func(t *testing.T) {
			requests := []surrogateRequest{}
			progress := []string{}
			transport := transportFunc(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				requests = append(requests, surrogateRequest{Method: request.Method, Path: request.URL.EscapedPath(), BodyJSON: string(body)})
				content, contentType := row.ResponseJSON, "application/json"
				if row.Mode == "stream" {
					content, contentType = row.SSE, "text/event-stream"
				}
				if len(requests) > 1 {
					content = surrogateDoneJSON
					if row.Mode == "stream" {
						content = surrogateDoneSSE
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(content)), Request: request}, nil
			})
			credentials := &encodingCredentials{}
			config := Config{BaseURL: "https://fixture.invalid", Credentials: credentials, HTTPClient: &http.Client{Transport: transport}}
			complete := func(request protocol.ModelRequest) (protocol.ModelReply, error) {
				if row.Mode == "stream" {
					client, err := NewStreaming(config)
					if err != nil {
						t.Fatal(err)
					}
					return client.CompleteStream(context.Background(), request, func(delta protocol.StreamDelta) error {
						if delta.Text != "" {
							progress = append(progress, surrogateHex(delta.Text))
						}
						return nil
					})
				}
				client, err := New(config)
				if err != nil {
					t.Fatal(err)
				}
				return client.Complete(context.Background(), request)
			}
			initial := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn,
				Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("capture")}}}
			reply, ingressError := complete(initial)
			if !row.IngressSucceeded {
				expected, present := surrogateNegativePhases[key]
				if !present || row.SDKReplyObtained != expected.replyObtained || row.FailurePhase == nil || *row.FailurePhase != expected.phase || row.IngressErrorClass == nil || *row.IngressErrorClass != expected.class {
					t.Fatal("actual fixed SDK failure phase/class/reply-obtained recipe changed")
				}
				var failure *Failure
				// Native Complete returns a closed typed reply or a protocol
				// failure; it does not expose the intermediate SDK model. The
				// Source send/snapshot distinction above is retained explicitly.
				if !errors.As(ingressError, &failure) || failure.Kind != FailureProtocol || failure.Class() != ClassProtocol {
					t.Fatal("captured ingress/snapshot refusal differs", ingressError)
				}
				if row.Mode == "direct" && (failure.Status != 200 || !strings.HasPrefix(failure.Message, "invalid provider response: ")) || row.Mode == "stream" && (failure.Status != 0 || !strings.HasPrefix(failure.Message, "invalid provider stream: ")) {
					t.Fatal("closed native protocol-adapter failure changed", failure)
				}
			} else {
				if _, negative := surrogateNegativePhases[key]; negative || !row.SDKReplyObtained {
					t.Fatal("SDK successful reply-obtained recipe changed")
				}
				if ingressError != nil || row.ContentJSON == nil || row.ForwardMessagesJSON == nil || row.ForwardSucceeded == nil {
					t.Fatal("actual SDK preserved reply but native ingress failed", ingressError)
				}
				assertSurrogateContent(t, reply.Content, *row.ContentJSON)
				cloned := reply.Clone()
				assertSurrogateContent(t, cloned.Content, *row.ContentJSON)
				continuation := protocol.PlainContent("continue")
				if use, ok := reply.Content[0].ToolUse(); ok {
					continuation = protocol.BlockContent(protocol.NewToolResult(use.ID, "safe fixture result", false))
				}
				next := initial
				next.Messages = append(append([]protocol.Message(nil), initial.Messages...),
					protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)},
					protocol.Message{Role: protocol.RoleUser, Content: continuation})
				_, nextError := complete(next)
				if *row.ForwardSucceeded {
					if nextError != nil {
						t.Fatal("scalar Unicode control lost next dispatch", nextError)
					}
				} else {
					var failure *Failure
					if row.ForwardErrorClass == nil || *row.ForwardErrorClass != "UnicodeEncodeError" || row.FailurePhase == nil || *row.FailurePhase != "request_encode" || !errors.As(nextError, &failure) || failure.Kind != FailureProtocol {
						t.Fatal("strict UTF-8 next-request refusal differs", nextError)
					}
				}
				assertSurrogateContent(t, reply.Content, *row.RetainedContentJSON)
			}
			if len(requests) != row.TransportRequests || int(credentials.calls.Load()) != row.TransportRequests {
				t.Fatal("request crossed encoding/credential/transport boundary", len(requests), credentials.calls.Load(), row.TransportRequests)
			}
			if !reflect.DeepEqual(progress, row.ProgressHex) {
				t.Fatal("stream progress code points differ", progress, row.ProgressHex)
			}
			for i, request := range requests {
				if request.Method != row.Requests[i].Method || request.Path != row.Requests[i].Path {
					t.Fatal("actual SDK HTTP request target differs")
				}
				surrogateEqual(t, "actual request JSON", surrogateValue(t, request.BodyJSON), surrogateValue(t, row.Requests[i].BodyJSON))
			}
		})
	}
}
