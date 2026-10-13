package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type ownedGreetingHandler struct{ owners *[]string }

func (handler ownedGreetingHandler) ExecuteExtensionTool(_ context.Context, authority ToolAuthority, input greetingExtensionInput) (string, error) {
	*handler.owners = append(*handler.owners, string(authority.OwnerID))
	return fmt.Sprintf("%s:Hello, %s!", authority.OwnerID, input.Name), nil
}

type schemaExtensionProvider struct{ requests []protocol.ModelRequest }

func (provider *schemaExtensionProvider) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	provider.requests = append(provider.requests, request.Clone())
	if len(provider.requests)%2 == 0 {
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	}
	input, err := protocol.DecodeModelToolInput("greet", []byte(`{"name":"World"}`))
	return fakeReply([]protocol.Block{protocol.NewToolUse("toolu_x", input)}, protocol.StopToolUse), err
}

func TestExtensionSchemasMatchSourceManagedRequests(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-extension-schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name               string
			SchemaJSON         string `json:"schema_json"`
			Received           []string
			TemplateNames      []string `json:"template_names"`
			SeparateRegistries bool     `json:"separate_registries"`
			Sessions           []struct {
				Owner, Output, Fingerprint string
				SnapshotJSON               string `json:"snapshot_json"`
				RequestToolsJSON           string `json:"request_tools_json"`
				PrivateRegistry            bool   `json:"private_registry"`
				Paired                     bool
			}
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 16 {
		t.Fatal(err, len(fixture.Cases))
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			trace, owners := []string{}, []string{}
			encoded := []byte(row.SchemaJSON)
			schema, err := protocol.ExtensionToolSchema("greet", "Unicode schema 描述", encoded)
			if err != nil {
				t.Fatal(err)
			}
			// The constructor cannot retain a caller-owned byte buffer.
			encoded[0] = ' '
			definition, err := NewTypedExtensionTool(schema, ToolTraits{Risk: RiskRead, Readonly: true}, greetingExtensionCodec{&trace}, ownedGreetingHandler{&owners})
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := NewToolCatalog(definition)
			if err != nil {
				t.Fatal(err)
			}
			provider := &schemaExtensionProvider{}
			config := managerTestConfig(t.TempDir(), provider)
			config.Services.Tools = catalog
			manager := makeManager(t, config)
			var sessions []*ManagedSession
			for _, expected := range row.Sessions {
				sessions = append(sessions, createManaged(t, manager, CreateSessionRequest{Owner: OwnerID(expected.Owner)}))
			}
			if len(sessions) != 2 || sessions[0].core.gate.catalog == sessions[1].core.gate.catalog || !row.SeparateRegistries {
				t.Fatal("catalogue publication is not private")
			}
			for i, expected := range row.Sessions {
				session := sessions[i]
				snapshot, err := session.core.gate.catalog.Snapshot()
				if err != nil {
					t.Fatal(err)
				}
				canonical, err := protocol.PythonJSON(snapshot.Schemas(), false, true)
				if err != nil || canonical != expected.SnapshotJSON || snapshot.Fingerprint() != expected.Fingerprint || !expected.PrivateRegistry {
					t.Fatal("Source schema/fingerprint differs", canonical, expected.SnapshotJSON, err)
				}
				if _, err := session.Run(context.Background(), "greet"); err != nil {
					t.Fatal(err)
				}
				request := provider.requests[i*2]
				wire, err := request.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				value, err := jsonvalue.Decode(string(wire))
				if err != nil {
					t.Fatal(err)
				}
				tools, ok := value.Lookup("tools")
				if !ok {
					t.Fatal("model tools missing")
				}
				actual, err := protocol.PythonJSON(tools.Sorted(), false, true)
				if err != nil || actual != expected.RequestToolsJSON {
					t.Fatal("Source model schema/cache annotation differs", actual, expected.RequestToolsJSON, err)
				}
				messages := session.Messages()
				blocks, _ := messages[2].Content.Blocks()
				result, _ := blocks[0].ToolResult()
				if result.Content != expected.Output || result.ToolUseID != "toolu_x" || result.IsError || !expected.Paired {
					t.Fatal(result, expected.Output)
				}
				// Detached schema DTOs and request clones cannot alter the live view.
				copy := snapshot.Schemas()
				copy[0].Name = "changed"
				copy[0].Description = "changed"
				fresh, _ := session.core.gate.catalog.Snapshot()
				if fresh.Fingerprint() != expected.Fingerprint {
					t.Fatal("schema DTO mutated live catalogue")
				}
			}
			if !reflect.DeepEqual(owners, row.Received) || len(provider.requests) != 4 || !reflect.DeepEqual(toolStrings(catalog.Names()), row.TemplateNames) {
				t.Fatal(owners, row.Received, provider.requests, row.TemplateNames)
			}
		})
	}
}
