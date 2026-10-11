package sqlitestate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestCodeExecutionSourceMetadataSurvivesSQLiteReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-code-execution.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Expected []protocol.Block `json:"expected_content"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 28 {
		t.Fatal("Source execution inventory changed")
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store := openStore(t, path)
	for i, row := range fixture.Cases {
		id := agent.SessionID(fmt.Sprintf("source-%d", i))
		message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(row.Expected...)}
		if count, err := store.AppendMessages(ctx, id, []protocol.Message{message}, 1); err != nil || count != 1 {
			t.Fatal("append", count, err)
		}
	}
	bad := string([]byte{0xff})
	base := protocol.CodeExecutionResult{ReturnCode: protocol.NewExecutionReturnCode(0)}
	encrypted := protocol.EncryptedCodeExecutionResult{ReturnCode: protocol.NewExecutionReturnCode(0)}
	values := []protocol.CodeExecutionToolResultBlock{
		{ToolUseID: bad, Content: protocol.NewCodeExecutionPlain(base)},
		{ToolUseID: "id", Content: protocol.NewCodeExecutionFailure(protocol.CodeExecutionErrorCode(bad))},
	}
	for _, field := range []string{"stdout", "stderr", "file"} {
		result := base
		switch field {
		case "stdout":
			result.Stdout = bad
		case "stderr":
			result.Stderr = bad
		case "file":
			result.OutputFiles = []protocol.CodeExecutionOutput{{FileID: bad}}
		}
		values = append(values, protocol.CodeExecutionToolResultBlock{ToolUseID: "id", Content: protocol.NewCodeExecutionPlain(result)})
	}
	for _, field := range []string{"cipher", "stderr", "file"} {
		result := encrypted
		switch field {
		case "cipher":
			result.EncryptedStdout = bad
		case "stderr":
			result.Stderr = bad
		case "file":
			result.OutputFiles = []protocol.CodeExecutionOutput{{FileID: bad}}
		}
		values = append(values, protocol.CodeExecutionToolResultBlock{ToolUseID: "id", Content: protocol.NewCodeExecutionEncrypted(result)})
	}
	for _, value := range values {
		valid := protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("must roll back")}
		invalid := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(value.Block())}
		if _, err := store.AppendMessages(ctx, "source-0", []protocol.Message{valid, invalid}, 1); err == nil {
			t.Fatal("invalid execution text was persisted")
		}
		if count, err := store.MessageCount(ctx, "source-0", nil); err != nil || count != 1 {
			t.Fatal("invalid batch did not roll back", count, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	for i, row := range fixture.Cases {
		id := agent.SessionID(fmt.Sprintf("source-%d", i))
		messages, err := store.LoadMessages(ctx, id, nil)
		if err != nil || len(messages) != 1 {
			t.Fatal("reopened messages", err)
		}
		blocks, _ := messages[0].Content.Blocks()
		got, _ := json.Marshal(blocks)
		expected, _ := json.Marshal(row.Expected)
		g, err := jsonvalue.Decode(string(got))
		if err != nil {
			t.Fatal(err)
		}
		e, err := jsonvalue.Decode(string(expected))
		if err != nil || !g.PythonEqual(e) {
			t.Fatal("reopened execution metadata differs", string(got), err)
		}
	}
}
