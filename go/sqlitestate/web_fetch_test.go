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

func TestWebFetchSourceMetadataSurvivesSQLiteReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-web-fetch.json")
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
	if len(fixture.Cases) != 42 {
		t.Fatal("Source web-fetch inventory changed")
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
	base := protocol.WebFetchResult{URL: "url", Document: protocol.FetchedDocument{Source: protocol.TextDocumentSource("text")}}
	values := []protocol.WebFetchToolResultBlock{
		{ToolUseID: bad, Content: protocol.NewWebFetchSuccess(base)},
		{ToolUseID: "id", Content: protocol.NewWebFetchFailure(protocol.WebFetchErrorCode(bad))},
	}
	for _, field := range []string{"url", "data", "media", "title", "time"} {
		result := base
		switch field {
		case "url":
			result.URL = bad
		case "data":
			result.Document.Source = protocol.TextDocumentSource(bad)
		case "media":
			result.Document.Source.MediaType = protocol.DocumentMediaType(bad)
		case "title":
			result.Document.Title = &bad
		case "time":
			result.RetrievedAt = &bad
		}
		values = append(values, protocol.WebFetchToolResultBlock{ToolUseID: "id", Content: protocol.NewWebFetchSuccess(result)})
	}
	for _, value := range values {
		valid := protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent("must roll back")}
		invalid := protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(value.Block())}
		if _, err := store.AppendMessages(ctx, "source-0", []protocol.Message{valid, invalid}, 1); err == nil {
			t.Fatal("invalid web-fetch text was persisted")
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
			t.Fatal("reopened web-fetch metadata differs", string(got), err)
		}
	}
}
