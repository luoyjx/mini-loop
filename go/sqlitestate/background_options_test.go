package sqlitestate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestBackgroundOptionObservationsSurviveSQLiteCloseReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-background-options.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name  string
			Input string `json:"input_json"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 48 {
		t.Fatal("Source option inventory changed")
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "options.db")
	store := openStore(t, path)
	for _, row := range fixture.Cases {
		input, err := protocol.DecodeModelToolInput(protocol.ToolBackgroundRun, []byte(row.Input))
		if err != nil {
			t.Fatal(err)
		}
		message := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewToolUse("options", input))}
		if count, err := store.AppendMessages(ctx, agent.SessionID(row.Name), []protocol.Message{message}, 1); err != nil || count != 1 {
			t.Fatal("append failed", row.Name, count, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			messages, err := store.LoadMessages(ctx, agent.SessionID(row.Name), nil)
			if err != nil || len(messages) != 1 {
				t.Fatal("message reopen failed", err)
			}
			blocks, _ := messages[0].Content.Blocks()
			use, ok := blocks[0].ToolUse()
			if !ok {
				t.Fatal("reopened message lost tool kind")
			}
			expected, err := protocol.DecodeModelToolInput(protocol.ToolBackgroundRun, []byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			want, _ := expected.CanonicalJSON()
			got, err := use.Input.CanonicalJSON()
			if err != nil || want != got {
				t.Fatal("SQL archive changed observed option identity", got, want, err)
			}
			_, wantErr := json.Marshal(expected)
			_, gotErr := json.Marshal(use.Input)
			if (wantErr == nil) != (gotErr == nil) {
				t.Fatal("reopen changed finite admission", gotErr, wantErr)
			}
		})
	}
}
