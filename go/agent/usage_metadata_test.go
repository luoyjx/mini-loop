package agent

import (
	"encoding/json"
	"strings"
	"testing"

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
	if !ok || event.Usage == nil || *event.Usage.CacheCreation.Ephemeral1hInputTokens != 7 || *event.Usage.OutputTokensDetails.ThinkingTokens != 3 || *event.Usage.ServerToolUse.WebFetchRequests != 2 {
		t.Fatal("archival usage metadata lost")
	}
	if usage.InferenceGeo == nil || *usage.InferenceGeo != runtimeCanary {
		t.Fatal("mask changed caller metadata")
	}
	*event.Usage.CacheCreation.Ephemeral1hInputTokens = 999
	untouched, _ := records[0].Event.ModelEnd()
	if *untouched.Usage.CacheCreation.Ephemeral1hInputTokens != 7 {
		t.Fatal("event accessor aliases metadata")
	}
	usageWire, _ := json.Marshal(event.Usage)
	if !strings.Contains(string(usageWire), `"cache_read_input_tokens":null`) {
		t.Fatal("explicit usage null was lost")
	}
}
