package sqlitestate

import (
	"context"
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNonfiniteSourceMessagesSurviveSQLiteCloseAndReopen(t *testing.T) {
	assertNonfiniteSourceMessagesReopen(t, "../testdata/python-provider-nonfinite-consumer.json", 4)
}

func TestNonfiniteBackgroundSourceMessagesSurviveSQLiteCloseAndReopen(t *testing.T) {
	assertNonfiniteSourceMessagesReopen(t, "../testdata/python-provider-nonfinite-background.json", 18)
}

func assertNonfiniteSourceMessagesReopen(t *testing.T, pathFixture string, count int) {
	t.Helper()
	data, err := os.ReadFile(pathFixture)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Kind, Name, Mode string
			Messages         string `json:"reopened_messages_json"`
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	selected := fixture.Cases[:0]
	for _, row := range fixture.Cases {
		if row.Mode == "" || row.Mode == "keep" {
			selected = append(selected, row)
		}
	}
	fixture.Cases = selected
	if len(fixture.Cases) != count {
		t.Fatal("Source inventory changed")
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store := openStore(t, path)
	for _, row := range fixture.Cases {
		value, err := jsonvalue.Decode(row.Messages)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := value.Array()
		raw, err := rows[0].MarshalLegacyUTF8()
		if err != nil {
			t.Fatal(err)
		}
		message, err := protocol.DecodeStoredMessage(raw)
		if err != nil {
			t.Fatal(err)
		}
		id := agent.SessionID(row.Kind + "-" + row.Name)
		if count, err := store.AppendMessages(ctx, id, []protocol.Message{message}, 1); err != nil || count != 1 {
			t.Fatal(id, count, err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	for _, row := range fixture.Cases {
		messages, err := store.LoadMessages(ctx, agent.SessionID(row.Kind+"-"+row.Name), nil)
		if err != nil || len(messages) != 1 {
			t.Fatal(err)
		}
		got, err := messages[0].MarshalArchiveJSON()
		if err != nil {
			t.Fatal(err)
		}
		observed, err := jsonvalue.Decode(string(got))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := jsonvalue.Decode(row.Messages)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := expected.Array()
		a, _ := observed.Sorted().MarshalLegacyUTF8()
		b, _ := rows[0].Sorted().MarshalLegacyUTF8()
		if string(a) != string(b) {
			t.Fatalf("reopened SDK observation changed: %s != %s", a, b)
		}
		if _, err = json.Marshal(messages[0]); err == nil {
			t.Fatal("archive read enabled finite HTTP encoding")
		}
	}
}

func TestNonfiniteToolEventsSurviveSQLiteCloseAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "events.db")
	store := openStore(t, path)
	for _, number := range []string{"NaN", "Infinity"} {
		raw := []byte(`{"type":"tool_use","seq":1,"ts":1,"session":"session","transcript_epoch":1,"name":"bash","id":"call","input":{"command":"true","keep":` + number + `},"display":{"verb":"Bash","object":"true"}}`)
		record, err := agent.DecodeStoredEvent(raw)
		if err != nil {
			t.Fatal(err)
		}
		if ordinal, err := store.AppendEvent(ctx, agent.SessionID(number), record); err != nil || ordinal != 1 {
			t.Fatal(ordinal, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	for _, number := range []string{"NaN", "Infinity"} {
		records, err := store.LoadEvents(ctx, agent.SessionID(number), 0, nil)
		if err != nil || len(records) != 1 {
			t.Fatal(records, err)
		}
		raw, err := records[0].MarshalArchiveJSON()
		if err != nil || !strings.Contains(string(raw), `"keep":`+number) {
			t.Fatal(string(raw), err)
		}
		if _, err = json.Marshal(records[0]); err == nil {
			t.Fatal("archive read enabled finite event encoding")
		}
	}
}
