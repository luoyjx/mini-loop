package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestInProcessMatchesActualPython(t *testing.T) {
	var fixture struct {
		Name        string
		Definitions []struct {
			Name        string
			Description InProcessValue
			InputSchema InProcessValue `json:"input_schema"`
		}
		Rows []struct {
			Recipe struct {
				Name      string
				Arguments InProcessValue
			}
			Output string
		}
		AfterClose string `json:"after_close"`
	}
	data, err := os.ReadFile("../testdata/python-mcp-inprocess.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	tools := []InProcessTool{}
	for index, definition := range fixture.Definitions {
		var handler InProcessHandler
		switch index {
		case 0:
			handler = func(_ context.Context, args InProcessValue) (InProcessValue, error) {
				value, _ := args.Lookup("value")
				return value, nil
			}
		case 1:
			handler = func(_ context.Context, args InProcessValue) (InProcessValue, error) { return args, nil }
		case 2:
			handler = func(context.Context, InProcessValue) (InProcessValue, error) {
				return InProcessValue{}, errors.New("bad 中文")
			}
		case 3:
			handler = func(context.Context, InProcessValue) (InProcessValue, error) {
				return jsonvalue.TextValue("first"), nil
			}
		case 4:
			handler = func(context.Context, InProcessValue) (InProcessValue, error) {
				return jsonvalue.TextValue("second"), nil
			}
		}
		tools = append(tools, InProcessTool{Definition: ToolDescription{Name: definition.Name, Description: definition.Description, InputSchema: definition.InputSchema}, Handler: handler})
	}
	client := NewInProcess(fixture.Name, tools)
	before, _ := client.ListTools(context.Background())
	tools[0] = InProcessTool{}
	if client.Name() != fixture.Name || len(before) != len(fixture.Definitions) {
		t.Fatal("discovery identity/order changed")
	}
	for i, definition := range fixture.Definitions {
		if before[i].Name != definition.Name || !reflect.DeepEqual(before[i].Description, definition.Description) || !reflect.DeepEqual(before[i].InputSchema, definition.InputSchema) {
			t.Fatal("discovery lost metadata", i)
		}
	}
	before[0] = ToolDescription{}
	after, _ := client.ListTools(context.Background())
	if after[0].Name != "value" {
		t.Fatal("discovery container aliases client")
	}
	for index, row := range fixture.Rows {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			result, err := client.CallTool(context.Background(), row.Recipe.Name, row.Recipe.Arguments)
			if err != nil || result != row.Output {
				t.Fatalf("result=%q err=%v want=%q", result, err, row.Output)
			}
		})
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), "duplicate", jsonvalue.ObjectValue(nil))
	if err != nil || result != fixture.AfterClose {
		t.Fatal("close revoked in-process handlers", result, err)
	}
}

func TestInProcessCancellationPanicAndUncappedText(t *testing.T) {
	input := jsonvalue.ObjectValue(nil)
	entered := make(chan struct{})
	client := NewInProcess("", []InProcessTool{
		{Definition: ToolDescription{Name: "cancel"}, Handler: func(ctx context.Context, _ InProcessValue) (InProcessValue, error) {
			close(entered)
			<-ctx.Done()
			return InProcessValue{}, ctx.Err()
		}},
		{Definition: ToolDescription{Name: "panic"}, Handler: func(context.Context, InProcessValue) (InProcessValue, error) { panic("private details") }},
		{Definition: ToolDescription{Name: "large"}, Handler: func(context.Context, InProcessValue) (InProcessValue, error) {
			return jsonvalue.TextValue(strings.Repeat("中文", 30000)), nil
		}},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListTools(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := client.CallTool(ctx, "cancel", input); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	active, cancelActive := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.CallTool(active, "cancel", input); done <- err }()
	<-entered
	cancelActive()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), "panic", input)
	if err != nil || result != "Error: handler panicked (string)" {
		t.Fatal(result, err)
	}
	result, err = client.CallTool(context.Background(), "large", input)
	if err != nil || len([]rune(result)) != 60000 {
		t.Fatal("stdio cap incorrectly applied", len([]rune(result)), err)
	}
	if _, err := client.CallTool(context.Background(), "large", jsonvalue.NullValue()); err == nil {
		t.Fatal("non-object arguments accepted")
	}
}
