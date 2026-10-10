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
)

func TestSDKUsageCountsSurviveSQLiteEventReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-usage-consumer.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Usage      jsonvalue.Value `json:"event_usage"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 34 {
		t.Fatal("Source inventory changed")
	}
	path := filepath.Join(t.TempDir(), "usage.db")
	store := openStore(t, path)
	ctx := context.Background()
	for i, row := range fixture.Cases {
		usage, err := row.Usage.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		record, err := agent.DecodeStoredEvent([]byte(fmt.Sprintf(`{"seq":%d,"ts":10,"session":"counts","transcript_epoch":1,"type":"model_end","status":"completed","purpose":"agent_turn","usage":%s}`, i+1, usage)))
		if err != nil {
			t.Fatal(row.Name, row.Mode, err)
		}
		if _, err := store.AppendEvent(ctx, "counts", record); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	rows, err := store.LoadEvents(ctx, "counts", 0, nil)
	if err != nil || len(rows) != 34 {
		t.Fatal(len(rows), err)
	}
	for i, row := range rows {
		if row.Scope.RunContext.Authority() != agent.AuthorityUntrusted {
			t.Fatal("counts supplied authority")
		}
		end, ok := row.Event.ModelEnd()
		if !ok || end.Usage == nil {
			t.Fatal("model_end usage missing")
		}
		actual, err := json.Marshal(end.Usage)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := fixture.Cases[i].Usage.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		citationRestartEqual(t, actual, expected)
		// Kind inspection avoids equality conflating bool with integer values.
		value, err := jsonvalue.Decode(string(actual))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"input_tokens", "output_tokens"} {
			a, _ := value.Lookup(key)
			b, _ := fixture.Cases[i].Usage.Lookup(key)
			if a.Kind() != b.Kind() {
				t.Fatal("count kind lost in SQLite", i, key)
			}
		}
	}
}
