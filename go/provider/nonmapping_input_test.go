package provider

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestDirectNonMappingInputsMatchSDKAndForwardedHistory(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonmapping-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name string
			Response   string `json:"response_json"`
			Expected   jsonvalue.Value
			Forwarded  jsonvalue.Value
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 12 {
		t.Fatal("SDK direct inventory changed")
	}
	for _, row := range fixture.Cases {
		t.Run(row.Kind+"/"+row.Name, func(t *testing.T) {
			reply, err := decodeReply([]byte(row.Response))
			if err != nil {
				t.Fatal(err)
			}
			content, err := json.Marshal(reply.Clone().Content)
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonvalue.Decode(string(content))
			if err != nil || !sameUsageShape(value, row.Expected) {
				t.Fatal("SDK value or kind changed", string(content))
			}
			message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}
			wire, err := json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			forwarded, err := jsonvalue.Decode(string(wire))
			if err != nil || !sameUsageShape(forwarded, row.Forwarded) {
				t.Fatal("SDK forwarded input changed", string(wire))
			}
			stored, err := protocol.DecodeStoredMessage(wire)
			if err != nil {
				t.Fatal(err)
			}
			blocks, _ := stored.Content.Blocks()
			if row.Kind == "tool_use" {
				use, ok := blocks[0].ToolUse()
				if !ok {
					t.Fatal("client discriminator lost")
				}
				failure, ok := use.Input.BashBindingFailure()
				if !ok || failure.Kind() != protocol.BashNonMappingArguments {
					t.Fatal("non-mapping input became executable arguments")
				}
			} else if _, ok := blocks[0].ToolUse(); ok {
				t.Fatal("server observation entered client dispatch")
			}
		})
	}
}
