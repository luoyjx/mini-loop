package protocol

import (
	"errors"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// NormalizeMCPName is the source ASCII namespace normalization. Components
// cannot retain the double-underscore separator, even through lossy escaping.
func NormalizeMCPName(name string) string {
	var out strings.Builder
	underscore := false
	for _, r := range name {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-'
		if allowed {
			out.WriteRune(r)
			underscore = false
		} else if !underscore {
			out.WriteByte('_')
			underscore = true
		}
	}
	result := strings.Trim(out.String(), "_")
	if result == "" {
		return "unnamed"
	}
	return result
}
func MCPToolName(server, tool string) ToolName {
	return ToolName("mcp__" + NormalizeMCPName(server) + "__" + NormalizeMCPName(tool))
}
func IsMCPToolName(name ToolName) bool {
	parts := strings.Split(string(name), "__")
	return len(parts) == 3 && parts[0] == "mcp" && parts[1] != "" && parts[2] != "" && NormalizeMCPName(parts[1]) == parts[1] && NormalizeMCPName(parts[2]) == parts[2]
}

// MCPInput is an explicit external-tool variant. The closed object retains
// server-defined argument fields without allowing executable values or raw JSON.
type MCPInput struct{ Arguments jsonvalue.Value }

func MCPToolInput(name ToolName, arguments jsonvalue.Value) (ToolInput, error) {
	input := ToolInput{name: name, mcpArguments: arguments}
	if !IsMCPToolName(name) {
		return ToolInput{}, errors.New("invalid MCP tool namespace")
	}
	return input, input.Validate()
}
func (input ToolInput) MCP() (MCPInput, bool) {
	return MCPInput{input.mcpArguments}, IsMCPToolName(input.name)
}
func decodeMCPInput(name ToolName, data []byte) (ToolInput, error) {
	arguments, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolInput{}, err
	}
	return MCPToolInput(name, arguments)
}

// MCPToolSchema keeps the server's complete schema as inert closed JSON. Schema
// extensions (refs, numeric enum, patterns, etc.) describe model arguments; they
// are not validators or authority, and cannot affect the external risk policy.
func MCPToolSchema(name ToolName, description string, schema jsonvalue.Value) (ToolSchema, error) {
	if !IsMCPToolName(name) {
		return ToolSchema{}, errors.New("invalid MCP tool namespace")
	}
	result := ToolSchema{Name: name, Description: description, InputSchema: InputSchema{mcpSchema: &schema}}
	return result, result.Validate()
}
