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

func TestSDKPartialValuesSurviveSQLiteReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-partial-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name string
			Expected   string `json:"expected_json"`
			Error      *struct{ Class, Message string }
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store := openStore(t, path)
	expectedRows := []string{}
	for _, row := range fixture.Cases {
		if row.Error != nil {
			continue
		}
		content, err := jsonvalue.Decode(row.Expected)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := content.MarshalJSON()
		if err != nil {
			continue
		} // captured nonfinite SDK adaptation
		var typed []protocol.Block
		if err := json.Unmarshal(encoded, &typed); err != nil {
			t.Fatal(row.Name, err)
		}
		id := agent.SessionID(fmt.Sprintf("partial-%d", len(expectedRows)))
		message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(typed...)}
		if count, err := store.AppendMessages(ctx, id, []protocol.Message{message}, 1); err != nil || count != 1 {
			t.Fatal(row.Name, count, err)
		}
		expectedRows = append(expectedRows, row.Expected)
	}
	if len(expectedRows) != 78 {
		t.Fatal("finite partial object inventory changed", len(expectedRows))
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	for i, expectedJSON := range expectedRows {
		rows, err := store.LoadMessages(ctx, agent.SessionID(fmt.Sprintf("partial-%d", i)), nil)
		if err != nil || len(rows) != 1 {
			t.Fatal("reopen", err)
		}
		blocks, _ := rows[0].Content.Blocks()
		encoded, err := json.Marshal(blocks)
		if err != nil {
			t.Fatal(err)
		}
		got, err := jsonvalue.Decode(string(encoded))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := jsonvalue.Decode(expectedJSON)
		if err != nil || !got.PythonEqual(expected) {
			t.Fatal("SDK partial object changed in SQLite", i, string(encoded), expectedJSON)
		}
	}
}
