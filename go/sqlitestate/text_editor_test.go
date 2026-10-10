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

func TestTextEditorSourceMetadataSurvivesSQLiteReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-text-editor.json")
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
	if len(fixture.Cases) != 48 {
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
	base := protocol.TextEditorViewResult{Content: "text", FileType: protocol.EditorText}
	lines := []string{bad}
	values := []protocol.TextEditorToolResultBlock{
		{ToolUseID: bad, Content: protocol.NewTextEditorView(base)},
		{ToolUseID: "id", Content: protocol.NewTextEditorFailure(protocol.TextEditorErrorCode(bad), nil)},
		{ToolUseID: "id", Content: protocol.NewTextEditorFailure(protocol.TextEditorFileNotFound, &bad)},
		{ToolUseID: "id", Content: protocol.NewTextEditorReplace(protocol.TextEditorReplaceResult{Lines: &lines})},
	}
	for _, field := range []string{"content", "file_type"} {
		result := base
		if field == "content" {
			result.Content = bad
		} else {
			result.FileType = protocol.EditorFileType(bad)
		}
		values = append(values, protocol.TextEditorToolResultBlock{ToolUseID: "id", Content: protocol.NewTextEditorView(result)})
	}
	for _, value := range values {
		valid := protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("must roll back")}
		invalid := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(value.Block())}
		if _, err := store.AppendMessages(ctx, "source-0", []protocol.Message{valid, invalid}, 1); err == nil {
			t.Fatal("invalid editor text was persisted")
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
