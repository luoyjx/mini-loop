package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestUsageMetadataObservationFailureDoesNotReplaceDecodedUsage(t *testing.T) {
	var usage TokenUsage
	initial := []byte(`{"input_tokens":13,"output_tokens":5,"cache_creation":[{"marker":9223372036854775808}],"service_tier":{"marker":1e100}}`)
	if err := json.Unmarshal(initial, &usage); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"cache_creation", "output_tokens_details", "server_tool_use", "service_tier", "inference_geo"} {
		t.Run(field, func(t *testing.T) {
			bad := fmt.Sprintf(`{"input_tokens":99,"output_tokens":99,%q:[{"marker":NaN}]}`, field)
			// Call the codec directly: encoding/json rejects NaN before a custom codec.
			if err := usage.UnmarshalJSON([]byte(bad)); err == nil {
				t.Fatal("accepted nonfinite known observation")
			}
			after, err := json.Marshal(usage)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatal("failed decode replaced caller state")
			}
		})
	}
	observation := *usage.CacheCreation.observation
	observedBefore, err := observation.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`[{"marker":NaN}]`, `[{"marker":Infinity}]`, `[{"marker":-Infinity}]`} {
		if err := observation.UnmarshalJSON([]byte(bad)); err == nil {
			t.Fatal("accepted nonfinite observation")
		}
		after, err := observation.MarshalJSON()
		if err != nil || !bytes.Equal(after, observedBefore) {
			t.Fatal("failed observation decode replaced state")
		}
	}
	if err := usage.UnmarshalJSON([]byte(strings.Repeat(" ", MaxWireBytes+1))); err == nil {
		t.Fatal("accepted oversized usage frame")
	}
	after, err := json.Marshal(usage)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("oversized decode replaced caller state")
	}
}
