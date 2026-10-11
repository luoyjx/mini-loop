// Package mcp implements the source harness's best-effort stdio MCP transport.
// Discovery values are inert closed JSON; registration must lower them into the
// agent's supported schemas and pass effects through its existing execution gate.
package mcp

import (
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

const MaxRPCLine = 8 * 1024 * 1024
const MaxToolResult = 50000

// ToolDescription retains untrusted discovery data without granting authority.
// A server's annotations, including readOnlyHint, never establish a safe risk.
type ToolDescription struct {
	Name        string
	Description jsonvalue.Value
	InputSchema jsonvalue.Value
	Annotations jsonvalue.Value
}

func decodeTools(result jsonvalue.Value) ([]ToolDescription, error) {
	if result.Kind() != jsonvalue.Object {
		return nil, fmt.Errorf("MCP tools/list result must be an object")
	}
	tools, present := result.Lookup("tools")
	if !present {
		return []ToolDescription{}, nil
	}
	items, ok := tools.Array()
	if !ok {
		return nil, fmt.Errorf("MCP tools/list tools must be an array")
	}
	out := make([]ToolDescription, 0, len(items))
	for _, item := range items {
		nameValue, _ := item.Lookup("name")
		name, ok := nameValue.Text()
		if !ok {
			return nil, fmt.Errorf("MCP tool name must be text")
		}
		description, exists := item.Lookup("description")
		if !exists {
			description = jsonvalue.TextValue("")
		}
		schema, exists := item.Lookup("inputSchema")
		if !exists {
			schema = object(field("type", jsonvalue.TextValue("object")), field("properties", object()))
		}
		annotations, exists := item.Lookup("annotations")
		if !exists {
			annotations = object()
		}
		out = append(out, ToolDescription{name, description, schema, annotations})
	}
	return out, nil
}

func renderResult(result jsonvalue.Value) (string, error) {
	if result.Kind() != jsonvalue.Object {
		return "", fmt.Errorf("MCP tools/call result must be an object")
	}
	content, exists := result.Lookup("content")
	var parts []string
	if exists {
		items, ok := content.Array()
		if !ok && content.Kind() != jsonvalue.Object && content.Kind() != jsonvalue.Text {
			return "", fmt.Errorf("MCP content must be iterable")
		}
		for _, item := range items {
			if item.Kind() != jsonvalue.Object {
				continue
			}
			value, present := item.Lookup("text")
			if !present {
				parts = append(parts, "")
				continue
			}
			text, ok := value.Text()
			if !ok {
				return "", fmt.Errorf("MCP content text must be text")
			}
			parts = append(parts, text)
		}
	}
	rendered := strings.Join(parts, "\n")
	if rendered == "" {
		data, err := jsonvalue.AppendLegacyDefault(result)
		if err != nil {
			return "", err
		}
		// Source caps the fallback before checking whether to append a notice.
		return jsonvalue.TextPrefix(string(data), MaxToolResult), nil
	}
	count := jsonvalue.RuneCount(rendered)
	if count > MaxToolResult {
		rendered = jsonvalue.TextPrefix(rendered, MaxToolResult) + fmt.Sprintf("\n[truncated from %s characters]", comma(count))
	}
	return rendered, nil
}

func comma(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func field(name string, value jsonvalue.Value) jsonvalue.Field {
	return jsonvalue.Field{Name: name, Value: value}
}
func object(fields ...jsonvalue.Field) jsonvalue.Value { return jsonvalue.ObjectValue(fields) }
