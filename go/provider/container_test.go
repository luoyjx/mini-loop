package provider

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"strings"
	"testing"
)

func TestContainerUploadMatchesSDKDirectAndStream(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-container-upload.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Mode, SSE string
			FileID    string `json:"file_id"`
			Message   json.RawMessage
			Expected  jsonvalue.Value `json:"expected_content"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 6 {
		t.Fatal("Source inventory changed")
	}
	client, err := New(Config{APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Mode+"/"+row.FileID, func(t *testing.T) {
			var reply protocol.ModelReply
			var err error
			if row.Mode == "stream" {
				reply, err = client.readStream(context.Background(), strings.NewReader(row.SSE), nil)
			} else {
				reply, err = decodeReply(row.Message)
			}
			if err != nil {
				t.Fatal(err)
			}
			value, ok := reply.Content[0].ContainerUpload()
			if !ok || value.FileID != row.FileID {
				t.Fatal("file identity lost")
			}
			encoded, err := json.Marshal(reply.Content)
			if err != nil {
				t.Fatal(err)
			}
			got, err := jsonvalue.Decode(string(encoded))
			if err != nil || !got.PythonEqual(row.Expected) {
				t.Fatal("SDK content projection differs", string(encoded))
			}
			message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(reply.Content...)}
			encoded, err = json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := protocol.DecodeStoredMessage(encoded)
			if err != nil {
				t.Fatal(err)
			}
			blocks, _ := restored.Content.Blocks()
			encoded, err = json.Marshal(blocks)
			if err != nil {
				t.Fatal(err)
			}
			got, err = jsonvalue.Decode(string(encoded))
			if err != nil || !got.PythonEqual(row.Expected) {
				t.Fatal("forwarded/stored container changed", string(encoded))
			}
			if _, tool := blocks[0].ToolUse(); tool {
				t.Fatal("container metadata became executable")
			}
		})
	}
	for _, raw := range []string{`{"type":"container_upload"}`, `{"type":"container_upload","file_id":null}`, `{"type":"container_upload","file_id":3}`} {
		if _, err := decodeBlock([]byte(raw)); err == nil {
			t.Fatal("invalid file ID admitted", raw)
		}
	}
}
