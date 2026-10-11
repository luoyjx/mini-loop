package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestUsageMetadataEventCloneMaskAndArchivalRoundTrip(t *testing.T) {
	raw := `{"input_tokens":13,"output_tokens":5,"cache_creation_input_tokens":null,"cache_read_input_tokens":null,"service_tier":"CANARY","inference_geo":"CANARY","cache_creation":{"ephemeral_1h_input_tokens":7,"ephemeral_5m_input_tokens":10,"CANARY":{"value":"CANARY"}},"output_tokens_details":{"thinking_tokens":3},"server_tool_use":{"web_fetch_requests":2,"web_search_requests":4},"future":{"value":"CANARY"}}`
	raw = strings.ReplaceAll(raw, "CANARY", runtimeCanary)
	// Marshal the canary as JSON so escaped/non-ASCII values exercise structure masking.
	var usage protocol.TokenUsage
	safe, _ := json.Marshal(runtimeCanary)
	raw = strings.ReplaceAll(raw, runtimeCanary, string(safe[1:len(safe)-1]))
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		t.Fatal(err)
	}
	events := &sessionEvents{secrets: runtimeSecrets(), sessionID: "session", epoch: 1}
	events.append(SessionEvent{kind: EventModelEnd, modelEnd: ModelEndEvent{SpanID: "span", Purpose: protocol.PurposeAgentTurn, Status: ModelCompleted, DurationMS: 1, Usage: &usage}})
	records := events.snapshot()
	if len(records) != 1 {
		t.Fatal("event missing")
	}
	wire, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), string(safe[1:len(safe)-1])) {
		t.Fatal("usage metadata leaked registered secret")
	}
	restored := assertStoredEventRoundTrip(t, wire)
	event, ok := restored.Event.ModelEnd()
	if !ok || event.Usage == nil || event.Usage.CacheCreation.Ephemeral1hInputTokens.Metered() != 7 || event.Usage.OutputTokensDetails.ThinkingTokens.Metered() != 3 || event.Usage.ServerToolUse.WebFetchRequests.Metered() != 2 {
		t.Fatal("archival usage metadata lost")
	}
	if usage.InferenceGeo == nil || *usage.InferenceGeo != runtimeCanary {
		t.Fatal("mask changed caller metadata")
	}
	*event.Usage.CacheCreation.Ephemeral1hInputTokens = protocol.IntegerUsageCount(999)
	untouched, _ := records[0].Event.ModelEnd()
	if untouched.Usage.CacheCreation.Ephemeral1hInputTokens.Metered() != 7 {
		t.Fatal("event accessor aliases metadata")
	}
	usageWire, _ := json.Marshal(event.Usage)
	if !strings.Contains(string(usageWire), `"cache_read_input_tokens":null`) {
		t.Fatal("explicit usage null was lost")
	}
}

// Source final usage is the independent expected shape. Registered escaped
// canaries exercise the actual event masker, accessors and historical codec.
func TestSDKUsageMetadataShapesEventMaskCloneAndArchive(t *testing.T) {
	assertSDKUsageMaskArchive(t, "../testdata/python-provider-usage-metadata.json", 191)
}

func assertSDKUsageMaskArchive(t *testing.T, path string, expected int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Usage      jsonvalue.Value `json:"event_usage"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != expected {
		t.Fatal("Source inventory changed")
	}
	canonical := func(data []byte) []byte {
		value, err := jsonvalue.Decode(string(data))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := value.Sorted().MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			source := row.Usage.MapStrings(func(s string) string { return strings.ReplaceAll(s, "secret-marker", runtimeCanary) })
			wire, err := source.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var usage protocol.TokenUsage
			if err := json.Unmarshal(wire, &usage); err != nil {
				t.Fatal(err)
			}
			original, err := json.Marshal(usage)
			if err != nil {
				t.Fatal(err)
			}
			registry := runtimeSecrets()
			want, err := source.MapStrings(registry.MaskText).MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			check := func(value protocol.TokenUsage) {
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(canonical(encoded), canonical(want)) {
					t.Fatalf("masked usage observation differs: %s want %s", encoded, want)
				}
			}
			reply := (protocol.ModelReply{Usage: usage}).Clone()
			check(reply.Usage.MapStrings(registry.MaskText))
			events := &sessionEvents{secrets: registry, sessionID: "metadata", epoch: 1}
			events.append(SessionEvent{kind: EventModelEnd, modelEnd: ModelEndEvent{SpanID: "span", Purpose: protocol.PurposeAgentTurn, Status: ModelCompleted, Usage: &usage}})
			records := events.snapshot()
			if len(records) != 1 {
				t.Fatal("event missing")
			}
			encoded, err := json.Marshal(records[0])
			if err != nil {
				t.Fatal(err)
			}
			restored := assertStoredEventRoundTrip(t, encoded)
			if restored.Scope.RunContext.Authority() != AuthorityUntrusted {
				t.Fatal("metadata restored authority")
			}
			end, ok := restored.Event.ModelEnd()
			if !ok || end.Usage == nil {
				t.Fatal("archived metadata missing")
			}
			check(*end.Usage)
			after, err := json.Marshal(usage)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatal("clone/mask mutated caller usage")
			}
		})
	}
}
