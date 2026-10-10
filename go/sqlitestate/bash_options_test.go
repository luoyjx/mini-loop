package sqlitestate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBashObservedOptionsSurviveSQLiteReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-bash-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Input   string `json:"input_json"`
			Enabled bool   `json:"background_enabled"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store := openStore(t, path)
	expected := []string{}
	for _, row := range fixture.Cases {
		if row.Enabled {
			continue
		}
		input, err := protocol.DecodeModelToolInput(protocol.ToolBash, []byte(row.Input))
		if err != nil {
			t.Fatal(err)
		}
		id := agent.SessionID(fmt.Sprintf("options-%d", len(expected)))
		message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewToolUse("options", input))}
		if count, err := store.AppendMessages(ctx, id, []protocol.Message{message}, 1); err != nil || count != 1 {
			t.Fatal(count, err)
		}
		expected = append(expected, row.Input)
	}
	if len(expected) != 28 {
		t.Fatal("distinct Source option inventory changed")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	for i, raw := range expected {
		rows, err := store.LoadMessages(ctx, agent.SessionID(fmt.Sprintf("options-%d", i)), nil)
		if err != nil || len(rows) != 1 {
			t.Fatal("reopen", err)
		}
		blocks, _ := rows[0].Content.Blocks()
		use, ok := blocks[0].ToolUse()
		if !ok {
			t.Fatal("Bash discriminator lost")
		}
		encoded, err := json.Marshal(use.Input)
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(raw)); err != nil {
			t.Fatal(err)
		}
		if err != nil || string(encoded) != compact.String() {
			t.Fatal("SQLite changed original option kind/value", i, string(encoded), err)
		}
	}
}
