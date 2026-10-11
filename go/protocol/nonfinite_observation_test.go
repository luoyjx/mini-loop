package protocol

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"os"
	"strings"
	"testing"
)

func TestNonfiniteSourceObservationsSeparateArchiveAndFiniteEncoding(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonfinite-consumer.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name string
			Messages   string `json:"reopened_messages_json"`
			Content    string `json:"content_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 4 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			rows, err := jsonvalue.Decode(row.Messages)
			if err != nil {
				t.Fatal(err)
			}
			items, ok := rows.Array()
			if !ok || len(items) != 1 {
				t.Fatal("Source message inventory")
			}
			encoded, err := items[0].MarshalLegacyUTF8()
			if err != nil {
				t.Fatal(err)
			}
			message, err := DecodeStoredMessage(encoded)
			if err != nil {
				t.Fatal(err)
			}
			archived, err := message.MarshalArchiveJSON()
			if err != nil {
				t.Fatal(err)
			}
			restored, err := jsonvalue.Decode(string(archived))
			if err != nil {
				t.Fatal(err)
			}
			actual, _ := restored.Sorted().MarshalLegacyUTF8()
			expected, _ := items[0].Sorted().MarshalLegacyUTF8()
			if string(actual) != string(expected) {
				t.Fatalf("archive changed: %s != %s", actual, expected)
			}
			if _, err = json.Marshal(message); err == nil {
				t.Fatal("nonfinite SDK snapshot entered finite request encoder")
			}
			blocks, _ := message.Content.Blocks()
			if row.Kind == "tool_use" {
				use, _ := blocks[0].ToolUse()
				failure, ok := use.Input.BashBindingFailure()
				if !ok || failure.Parameter() != "keep" {
					t.Fatal("nonfinite extra lost binding failure")
				}
				canonical, err := use.Input.CanonicalJSON()
				if err != nil || !strings.Contains(canonical, `"keep":`) {
					t.Fatal(canonical, err)
				}
				if _, err = DecodeToolInput(ToolBash, []byte(`{"command":"true","keep":NaN}`)); err == nil {
					t.Fatal("strict explicit input admitted nonfinite metadata")
				}
			} else {
				if _, err = DecodeServerToolInput([]byte(`{"keep":NaN}`)); err == nil {
					t.Fatal("strict server input admitted nonfinite observation")
				}
			}
		})
	}
}
