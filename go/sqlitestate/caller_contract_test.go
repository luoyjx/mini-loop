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

func TestDeclaredCallerSourceMetadataSurvivesSQLiteReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-caller.json")
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
	if len(fixture.Cases) != 160 {
		t.Fatal("Source caller inventory changed")
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
			t.Fatal("reopened caller metadata differs", string(got), err)
		}
	}
}
