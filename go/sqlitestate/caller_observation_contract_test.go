package sqlitestate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type sqliteCallerObservationCase struct {
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	ExpectedContent string `json:"expected_content_json"`
}

type sqliteCallerObservationFixture struct {
	Profiles []string                      `json:"profile_names"`
	Cases    []sqliteCallerObservationCase `json:"cases"`
}

func TestSourceCallerObservationsSurviveSQLiteCloseAndReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-caller-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture sqliteCallerObservationFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 224 || len(fixture.Profiles) != 28 {
		t.Fatalf("Source caller inventory changed: %d cases, %d profiles", len(fixture.Cases), len(fixture.Profiles))
	}
	wanted := make(map[string]bool, 224)
	for _, kind := range []string{"tool_use", "server_tool_use", "web_search_tool_result", "web_fetch_tool_result"} {
		for _, profile := range fixture.Profiles {
			for _, mode := range []string{"direct", "stream"} {
				wanted[kind+"/"+profile+"/"+mode] = true
			}
		}
	}
	if len(wanted) != 224 {
		t.Fatal("Source caller profile inventory contains duplicates")
	}
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store := openStore(t, path)
	for i, row := range fixture.Cases {
		label := row.Name + "/" + row.Mode
		if !wanted[label] {
			t.Fatalf("unexpected or duplicate Source caller case %q", label)
		}
		delete(wanted, label)
		// Source observations include NaN and Infinity. Archive admission keeps
		// them inert, while ordinary Message.UnmarshalJSON remains finite.
		message, err := protocol.DecodeStoredMessage([]byte(`{"role":"assistant","content":` + row.ExpectedContent + `}`))
		if err != nil {
			t.Fatalf("%s: decode Source archive: %v", label, err)
		}
		id := agent.SessionID(fmt.Sprintf("caller-observation-%d", i))
		if count, err := store.AppendMessages(ctx, id, []protocol.Message{message}, 1); err != nil || count != 1 {
			t.Fatalf("%s: append count=%d: %v", label, count, err)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing %d Source caller cases", len(wanted))
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	for i, row := range fixture.Cases {
		t.Run(row.Name+"/"+row.Mode, func(t *testing.T) {
			id := agent.SessionID(fmt.Sprintf("caller-observation-%d", i))
			messages, err := store.LoadMessages(ctx, id, nil)
			if err != nil || len(messages) != 1 {
				t.Fatalf("reopened messages count=%d: %v", len(messages), err)
			}
			archive, err := messages[0].MarshalArchiveJSON()
			if err != nil {
				t.Fatal(err)
			}
			got := decodeSQLiteCallerObservation(t, string(archive))
			want := decodeSQLiteCallerObservation(t, `{"role":"assistant","content":`+row.ExpectedContent+`}`)
			assertSQLiteCallerObservation(t, "complete message", got, want)
			blocks, ok := messages[0].Content.Blocks()
			if !ok || len(blocks) != 2 {
				t.Fatalf("reopened content is not the two Source blocks: %d", len(blocks))
			}
			kind, _, _ := strings.Cut(row.Name, "/")
			if string(blocks[0].Kind()) != kind {
				t.Fatalf("caller changed outer block kind: %s", blocks[0].Kind())
			}
			content, _ := want.Lookup("content")
			items, _ := content.Array()
			wantCaller, present := items[0].Lookup("caller")
			if !present {
				t.Fatal("Source observation lost caller field")
			}
			caller := sqliteObservedCaller(t, blocks[0])
			observation, ok := caller.SDKObservation()
			if !ok {
				t.Fatal("archive caller lost its SDK observation")
			}
			assertSQLiteCallerObservation(t, "caller root", observation.Value(), wantCaller)
			legacy, err := observation.LegacyJSON()
			if err != nil {
				t.Fatal(err)
			}
			assertSQLiteCallerObservation(t, "caller legacy JSON", decodeSQLiteCallerObservation(t, string(legacy)), wantCaller)
			wantType, _ := wantCaller.Lookup("type")
			assertSQLiteCallerObservation(t, "caller type", caller.TypeMetadata().Observation(), wantType)
			wantKind := protocol.CallerObserved
			wantID := jsonvalue.NullValue()
			if tag, ok := wantType.Text(); ok {
				switch protocol.CallerKind(tag) {
				case protocol.CallerDirect:
					wantKind = protocol.CallerDirect
				case protocol.CallerCodeExecution2025, protocol.CallerCodeExecution2026:
					wantKind = protocol.CallerKind(tag)
					wantID, _ = wantCaller.Lookup("tool_id")
				}
			}
			if caller.Kind() != wantKind {
				t.Fatalf("caller kind=%q, want %q", caller.Kind(), wantKind)
			}
			assertSQLiteCallerObservation(t, "caller tool ID", caller.ToolIDMetadata().Observation(), wantID)
			wantText, _ := wantID.Text()
			if caller.ToolID() != wantText {
				t.Fatalf("caller text ID=%q, want %q", caller.ToolID(), wantText)
			}
			_, finiteError := want.MarshalJSON()
			if _, err := json.Marshal(messages[0]); (err == nil) != (finiteError == nil) {
				t.Fatalf("archive changed finite HTTP admission: got %v, Source value %v", err, finiteError)
			}
		})
	}
}

func sqliteObservedCaller(t *testing.T, block protocol.Block) *protocol.ToolCaller {
	t.Helper()
	var caller *protocol.ToolCaller
	switch block.Kind() {
	case protocol.BlockToolUse:
		value, _ := block.ToolUse()
		caller = value.Caller
	case protocol.BlockServerToolUse:
		value, _ := block.ServerToolUse()
		caller = value.Caller
	case protocol.BlockWebSearchToolResult:
		value, _ := block.WebSearchToolResult()
		caller = value.Caller
	case protocol.BlockWebFetchToolResult:
		value, _ := block.WebFetchToolResult()
		caller = value.Caller
	}
	if caller == nil {
		t.Fatal("reopened block lost caller metadata")
	}
	return caller
}

func decodeSQLiteCallerObservation(t *testing.T, data string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertSQLiteCallerObservation(t *testing.T, field string, got, want jsonvalue.Value) {
	t.Helper()
	// PythonEqual makes NaN unequal to itself and conflates bool/numeric leaves.
	// Sorted legacy JSON preserves exact scalar variants and array order.
	actual, err := jsonvalue.AppendLegacy(nil, got.Sorted())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := jsonvalue.AppendLegacy(nil, want.Sorted())
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("%s changed after SQLite reopen: %s != %s", field, actual, expected)
	}
}
