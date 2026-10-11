package protocol

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestUsageMetadataMatchesSourceEventShapes(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-usage.json")
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
	if len(fixture.Cases) != 9 {
		t.Fatal("Source usage inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			wire, _ := row.Usage.MarshalJSON()
			var usage TokenUsage
			err := json.Unmarshal(wire, &usage)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := json.Marshal(usage)
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonvalue.Decode(string(actual))
			if err != nil || !value.PythonEqual(row.Usage) {
				t.Fatal("SDK usage event shape lost", string(actual))
			}
			cloned := (ModelReply{Usage: usage}).Clone().Usage
			if usage.CacheCreation != nil && usage.CacheCreation.Ephemeral1hInputTokens != nil {
				*cloned.CacheCreation.Ephemeral1hInputTokens = IntegerUsageCount(999)
				if usage.CacheCreation.Ephemeral1hInputTokens.Metered() == 999 {
					t.Fatal("cache metadata aliases clone")
				}
			}
			if usage.InferenceGeo != nil {
				*cloned.InferenceGeo = "changed"
				if *usage.InferenceGeo == "changed" {
					t.Fatal("geo aliases clone")
				}
			}
			if usage.OutputTokensDetails != nil && usage.OutputTokensDetails.ThinkingTokens != nil {
				*cloned.OutputTokensDetails.ThinkingTokens = IntegerUsageCount(999)
				if usage.OutputTokensDetails.ThinkingTokens.Metered() == 999 {
					t.Fatal("output metadata aliases clone")
				}
			}
			if usage.ServerToolUse != nil && usage.ServerToolUse.WebFetchRequests != nil {
				*cloned.ServerToolUse.WebFetchRequests = IntegerUsageCount(999)
				if usage.ServerToolUse.WebFetchRequests.Metered() == 999 {
					t.Fatal("server metadata aliases clone")
				}
			}
		})
	}
}

func TestUsageSparseNullDefaultsAndNestedExtensions(t *testing.T) {
	sparse := TokenUsage{InputTokens: IntegerUsageCount(13), OutputTokens: IntegerUsageCount(5)}
	wire, _ := json.Marshal(sparse)
	if string(wire) != `{"input_tokens":13,"output_tokens":5}` {
		t.Fatal("native sparse usage changed", string(wire))
	}
	normalized := sparse.WithSDKDefaults()
	wire, _ = json.Marshal(normalized)
	value, _ := jsonvalue.Decode(string(wire))
	if len(value.Keys()) != 9 {
		t.Fatal("SDK nullable fields missing")
	}
	for _, key := range []string{"cache_creation", "inference_geo", "output_tokens_details", "server_tool_use"} {
		v, ok := value.Lookup(key)
		if !ok || v.Kind() != jsonvalue.Null {
			t.Fatal("SDK null missing", key)
		}
	}
	raw := `{"input_tokens":1,"output_tokens":2,"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":1,"extra":{"generation":7}},"output_tokens_details":{"thinking_tokens":0,"future":"detail"},"server_tool_use":{"web_fetch_requests":0,"web_search_requests":1,"future":true},"future":[1,"value"]}`
	var usage TokenUsage
	if err := json.Unmarshal([]byte(raw), &usage); err != nil {
		t.Fatal(err)
	}
	wire, _ = json.Marshal((ModelReply{Usage: usage}).Clone().Usage)
	actual, _ := jsonvalue.Decode(string(wire))
	expected, _ := jsonvalue.Decode(raw)
	if !actual.PythonEqual(expected) {
		t.Fatal("nested closed extensions lost", string(wire))
	}
}

// Non-text geo is accepted as observed SDK metadata; the required Source test
// covers the exact former inference_geo:5 rejection case. Genuine malformed
// frames, missing required counts and nonfinite counts remain atomic failures.
func TestUsageMetadataFailureIsAtomicAndBounded(t *testing.T) {
	initial := TokenUsage{InputTokens: IntegerUsageCount(7), OutputTokens: IntegerUsageCount(8)}
	for _, raw := range []string{`{"input_tokens":1}`, `{"input_tokens":1,"output_tokens":2,"cache_creation":{"ephemeral_1h_input_tokens":NaN,"ephemeral_5m_input_tokens":0}}`, `{"input_tokens":1,"output_tokens":2,"output_tokens_details":{"thinking_tokens":NaN}}`, strings.Repeat(" ", MaxWireBytes+1)} {
		target := initial
		if err := json.Unmarshal([]byte(raw), &target); err == nil {
			t.Fatal("invalid usage accepted")
		}
		if target.InputTokens.Metered() != 7 || target.OutputTokens.Metered() != 8 {
			t.Fatal("failed decode changed target")
		}
	}
}
