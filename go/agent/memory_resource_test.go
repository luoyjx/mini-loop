package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

type memoryReplyResource struct {
	Kind                 string
	Count                int
	Text, Prefix, Suffix string
	SHA256               string `json:"sha256"`
}

// Large replies stay compact in fixtures. The Source hash proves that the
// generated native bytes are the exact reply consumed by the Python scenario.
func memoryResourceReply(t *testing.T, reply string, resource *memoryReplyResource) string {
	t.Helper()
	if resource == nil {
		return reply
	}
	if resource.Count <= 0 {
		t.Fatal("resource count must be positive")
	}
	var payload string
	switch resource.Kind {
	case "text":
		encoded, err := json.Marshal(strings.Repeat(resource.Text, resource.Count))
		if err != nil {
			t.Fatal(err)
		}
		payload = string(encoded)
	case "array", "malformed-array":
		payload = "[" + strings.Repeat("0,", resource.Count-1) + "0"
		if resource.Kind == "malformed-array" {
			payload += ","
		}
		payload += "]"
	default:
		t.Fatal("unknown resource kind", resource.Kind)
	}
	reply = resource.Prefix + payload + resource.Suffix
	digest := sha256.Sum256([]byte(reply))
	if hex.EncodeToString(digest[:]) != resource.SHA256 {
		t.Fatal("native resource reply differs from actual Source bytes")
	}
	return reply
}
