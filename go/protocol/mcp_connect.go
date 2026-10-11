package protocol

import (
	"encoding/json"
	"errors"
	"strings"
)

const ToolConnectMCP ToolName = "connect_mcp"

type ConnectMCPInput struct {
	Name string `json:"name"`
}

func ConnectMCPToolInput(value ConnectMCPInput) ToolInput {
	return ToolInput{name: ToolConnectMCP, connectMCP: value}
}
func (input ToolInput) ConnectMCP() (ConnectMCPInput, bool) {
	return input.connectMCP, input.name == ToolConnectMCP
}
func decodeConnectMCPInput(data []byte) (ToolInput, error) {
	var wire struct {
		Name *string `json:"name"`
	}
	if err := decodeToolObject(data, &wire); err != nil {
		return ToolInput{}, err
	}
	if wire.Name == nil {
		return ToolInput{}, errors.New("connect_mcp requires a string name")
	}
	return ConnectMCPToolInput(ConnectMCPInput{Name: *wire.Name}), nil
}
func ConnectMCPSchema(aliases []string) ToolSchema {
	available := strings.Join(aliases, ", ")
	if available == "" {
		available = "(none)"
	}
	properties := SchemaProperties{"name": InputSchema{Type: SchemaString}}
	return ToolSchema{Name: ToolConnectMCP, Description: "Connect an MCP server and add its tools. Available: " + available + ".", InputSchema: InputSchema{Type: SchemaObject, Properties: &properties, Required: []string{"name"}}}
}
func marshalConnectMCP(input ToolInput) ([]byte, error) { return json.Marshal(input.connectMCP) }
