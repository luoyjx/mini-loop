package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type callerObservationFixture struct {
	Cases        []callerObservationCase    `json:"cases"`
	ForwardCases []callerObservationForward `json:"forward_cases"`
}

type callerObservationCase struct {
	Name, Mode, SSE string
	Response        string `json:"response_json"`
	Expected        string `json:"expected_content_json"`
}

type callerObservationForward struct {
	Name, Mode        string
	Content           string  `json:"content_json"`
	Messages          string  `json:"request_messages_json"`
	Succeeded         bool    `json:"succeeded"`
	ErrorClass        *string `json:"error_class"`
	ErrorMessage      *string `json:"error_message"`
	TransportRequests int     `json:"transport_requests"`
	ForwardedRequest  *string `json:"forwarded_request_json"`
}

func loadCallerObservations(t *testing.T) callerObservationFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-provider-caller-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture callerObservationFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 224 || len(fixture.ForwardCases) != 8 {
		t.Fatal("Source caller observation inventory changed", len(fixture.Cases), len(fixture.ForwardCases))
	}
	return fixture
}

// Legacy JSON comparison retains NaN and infinity, where PythonEqual would
// intentionally report NaN unequal to itself. Sorting only normalizes key order.
func callerObservationCanonical(t *testing.T, raw string) string {
	t.Helper()
	value, err := jsonvalue.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := value.Sorted().MarshalLegacyUTF8()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertCallerObservationValue(t *testing.T, label string, got, want jsonvalue.Value) {
	t.Helper()
	a, err := got.Sorted().MarshalLegacyUTF8()
	if err != nil {
		t.Fatal(label, err)
	}
	b, err := want.Sorted().MarshalLegacyUTF8()
	if err != nil || string(a) != string(b) {
		t.Fatalf("%s: %s != %s (%v)", label, a, b, err)
	}
}

func assertCallerObservationContent(t *testing.T, blocks []protocol.Block, expected string) {
	t.Helper()
	data, err := protocol.MarshalObservedBlocks(blocks)
	if err != nil || callerObservationCanonical(t, string(data)) != callerObservationCanonical(t, expected) {
		t.Fatalf("SDK observation differs: %s != %s (%v)", data, expected, err)
	}
	want, err := jsonvalue.Decode(expected)
	if err != nil {
		t.Fatal(err)
	}
	wantBlocks, ok := want.Array()
	if !ok || len(blocks) != len(wantBlocks) {
		t.Fatal("Source content shape changed")
	}
	for i, block := range blocks {
		wantCaller, present := wantBlocks[i].Lookup("caller")
		caller := callerContractBlockCaller(block)
		if !present || wantCaller.Kind() == jsonvalue.Null {
			if caller != nil {
				t.Fatal("unexpected caller", i)
			}
			continue
		}
		if caller == nil {
			t.Fatal("missing observed caller", i)
		}
		if err := caller.Validate(); err != nil {
			t.Fatal("observed caller rejected by validation", err)
		}
		observation, ok := caller.SDKObservation()
		if !ok {
			t.Fatal("SDK root observation unavailable")
		}
		assertCallerObservationValue(t, "root", observation.Value(), wantCaller)
		legacy, err := observation.LegacyJSON()
		if err != nil || callerObservationCanonical(t, string(legacy)) != callerObservationCanonical(t, mustCallerObservationJSON(t, wantCaller)) {
			t.Fatal("SDK root archival projection changed", err)
		}
		for _, encode := range []func() ([]byte, error){observation.MarshalJSON, caller.MarshalJSON} {
			finiteJSON, err := encode()
			if json.Valid(legacy) != (err == nil) {
				t.Fatal("caller observation relaxed finite JSON admission", err)
			}
			if err == nil && callerObservationCanonical(t, string(finiteJSON)) != callerObservationCanonical(t, string(legacy)) {
				t.Fatal("finite caller root changed on JSON encoding")
			}
		}
		wantType, _ := wantCaller.Lookup("type")
		assertCallerObservationValue(t, "type", caller.TypeMetadata().Observation(), wantType)
		kind := protocol.CallerObserved
		if tag, text := wantType.Text(); text {
			switch protocol.CallerKind(tag) {
			case protocol.CallerDirect, protocol.CallerCodeExecution2025, protocol.CallerCodeExecution2026:
				kind = protocol.CallerKind(tag)
			}
		}
		if caller.Kind() != kind {
			t.Fatal("caller kind promoted malformed discriminator", caller.Kind(), kind)
		}
		var wantID jsonvalue.Value
		if kind == protocol.CallerCodeExecution2025 || kind == protocol.CallerCodeExecution2026 {
			wantID, _ = wantCaller.Lookup("tool_id")
		}
		assertCallerObservationValue(t, "tool ID", caller.ToolIDMetadata().Observation(), wantID)
		text, _ := wantID.Text()
		if caller.ToolID() != text {
			t.Fatal("non-text caller ID became an execution string")
		}
	}
}

func mustCallerObservationJSON(t *testing.T, value jsonvalue.Value) string {
	t.Helper()
	data, err := value.MarshalLegacyUTF8()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSourceCallerObservationsDirectAndStream(t *testing.T) {
	fixture := loadCallerObservations(t)
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, row := range fixture.Cases {
		key := row.Mode + "/" + row.Name
		if seen[key] {
			t.Fatal("duplicate Source profile", key)
		}
		seen[key] = true
		t.Run(key, func(t *testing.T) {
			var reply protocol.ModelReply
			var err error
			switch row.Mode {
			case "direct":
				reply, err = decodeReply([]byte(row.Response))
			case "stream":
				reply, err = client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
			default:
				t.Fatal("unknown Source transport mode", row.Mode)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertCallerObservationContent(t, reply.Content, row.Expected)
			clone := reply.Clone()
			content := protocol.BlockContent(reply.Content...)
			contentClone := content.Clone()
			if content.SameStorage(contentClone) {
				t.Fatal("content clone retained mutable storage")
			}
			for _, blocks := range [][]protocol.Block{clone.Content, func() []protocol.Block { b, _ := contentClone.Blocks(); return b }()} {
				assertCallerObservationContent(t, blocks, row.Expected)
			}
			message := protocol.Message{Role: protocol.RoleAssistant, Content: content}
			archive, err := message.MarshalArchiveJSON()
			if err != nil {
				t.Fatal(err)
			}
			restored, err := protocol.DecodeStoredMessage(archive)
			if err != nil {
				t.Fatal("stored observation rejected", err)
			}
			blocks, _ := restored.Content.Blocks()
			assertCallerObservationContent(t, blocks, row.Expected)
			finite := json.Valid(archive)
			requestJSON, encodeErr := json.Marshal(message)
			if finite != (encodeErr == nil) {
				t.Fatal("finite HTTP encoding boundary changed", encodeErr)
			}
			if finite && callerObservationCanonical(t, string(requestJSON)) != callerObservationCanonical(t, string(archive)) {
				t.Fatal("finite request encoding lost SDK metadata")
			}
			mask := func(s string) string { return strings.ReplaceAll(s, "secret", "masked") }
			if finite {
				masked, err := protocol.MaskedPythonJSON(contentClone, mask, false, true)
				if err != nil {
					t.Fatal(err)
				}
				want, _ := jsonvalue.Decode(row.Expected)
				got, err := jsonvalue.Decode(masked)
				if err != nil {
					t.Fatal(err)
				}
				assertCallerObservationValue(t, "masked content", got, want.MapStrings(mask))
			}
			// Accessors expose detached handles, including list-root observations.
			for _, b := range clone.Content {
				if caller := callerContractBlockCaller(b); caller != nil {
					observation, _ := caller.SDKObservation()
					if values, ok := observation.Value().Array(); ok && len(values) > 0 {
						values[0] = jsonvalue.TextValue("replacement")
					}
					maskedRoot, err := observation.Value().MapStrings(mask).MarshalLegacyUTF8()
					if err != nil || strings.Contains(string(maskedRoot), "secret") {
						t.Fatal("detached root mask lost nested observation fields", string(maskedRoot), err)
					}
					*caller = protocol.DirectToolCaller()
				}
			}
			clone.Content[0] = protocol.NewTextBlock("replacement")
			assertCallerObservationContent(t, reply.Content, row.Expected)
			blocks, _ = contentClone.Blocks()
			assertCallerObservationContent(t, blocks, row.Expected)
		})
	}
}

func TestSourceCallerObservationForwarding(t *testing.T) {
	fixture := loadCallerObservations(t)
	for _, row := range fixture.ForwardCases {
		t.Run(row.Mode+"/"+row.Name, func(t *testing.T) {
			var source *callerObservationCase
			for i := range fixture.Cases {
				candidate := &fixture.Cases[i]
				if candidate.Name == row.Name && candidate.Mode == row.Mode {
					source = candidate
					break
				}
			}
			if source == nil {
				t.Fatal("forward profile has no actual SDK capture")
			}
			requests := []string{}
			transport := transportFunc(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				requests = append(requests, string(body))
				response, contentType := source.Response, "application/json"
				if len(requests) == 1 && row.Mode == "stream" {
					response, contentType = source.SSE, "text/event-stream"
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(response)), Request: request}, nil
			})
			credentials := &encodingCredentials{}
			cfg := Config{BaseURL: "https://caller-fixture.invalid", Credentials: credentials, HTTPClient: &http.Client{Transport: transport}}
			initial := protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("capture")}}}
			var reply protocol.ModelReply
			var err error
			if row.Mode == "stream" {
				client, createErr := NewStreaming(cfg)
				if createErr != nil {
					t.Fatal(createErr)
				}
				reply, err = client.CompleteStream(context.Background(), initial, nil)
			} else {
				client, createErr := New(cfg)
				if createErr != nil {
					t.Fatal(createErr)
				}
				reply, err = client.Complete(context.Background(), initial)
			}
			if err != nil {
				t.Fatal("first SDK response", err)
			}
			assertCallerObservationContent(t, reply.Content, row.Content)
			messages := []protocol.Message{
				{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)},
				{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("client", "safe fixture result", false))},
			}
			observed, err := protocol.ObservedMessagesPythonJSON(messages, false, true)
			if err != nil || callerObservationCanonical(t, observed) != callerObservationCanonical(t, row.Messages) {
				t.Fatal("paired second-request transcript differs", observed, err)
			}
			// A fresh cache makes a premature credential fetch observable, even
			// though the first transport already fetched this same credential.
			second, err := New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = second.Complete(context.Background(), protocol.ModelRequest{Model: "fixture-model", MaxTokens: 8, Purpose: protocol.PurposeAgentTurn, Messages: messages})
			if row.Succeeded {
				if err != nil || row.ForwardedRequest == nil || len(requests) != 2 {
					t.Fatal("finite caller was not forwarded", err, len(requests))
				}
				if callerObservationCanonical(t, requests[1]) != callerObservationCanonical(t, *row.ForwardedRequest) {
					t.Fatal("forwarded request lost caller observation", requests[1], *row.ForwardedRequest)
				}
			} else {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Kind != FailureNonfiniteRequest || row.ErrorClass == nil || row.ErrorMessage == nil || string(failure.Class()) != *row.ErrorClass || failure.Message != *row.ErrorMessage {
					t.Fatal("nonfinite request failure changed", err)
				}
				if row.ForwardedRequest != nil {
					t.Fatal("Source unexpectedly transported nonfinite request")
				}
			}
			if len(requests) != row.TransportRequests || int(credentials.calls.Load()) != row.TransportRequests {
				t.Fatal("request crossed credential/transport boundary", len(requests), credentials.calls.Load(), row.TransportRequests)
			}
		})
	}
}
