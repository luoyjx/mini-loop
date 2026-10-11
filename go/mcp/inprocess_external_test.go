package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/luoyjx/mini-loop/go/mcp"
)

func TestInProcessEmbeddingNeedsOnlyPublicTypes(t *testing.T) {
	client := mcp.NewInProcess("embedding", []mcp.InProcessTool{{
		Definition: mcp.ToolDescription{Name: "echo"},
		Handler: func(_ context.Context, arguments mcp.InProcessValue) (mcp.InProcessValue, error) {
			value, _ := arguments.Lookup("value")
			return value, nil
		},
	}})
	var input mcp.InProcessValue
	if err := json.Unmarshal([]byte(`{"value":true}`), &input); err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), "echo", input)
	if err != nil || result != "True" {
		t.Fatal(result, err)
	}
}
