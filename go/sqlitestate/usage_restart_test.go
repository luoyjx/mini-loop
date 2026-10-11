package sqlitestate

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
)

// Compose pinned SDK usage captures with registered-secret canaries. Expected
// metadata comes from the SDK fixture, independently of Go usage normalization.
func usageRestartPayloads(t *testing.T, streamed bool, sse string) ([]byte, []byte, string) {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-provider-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Wire       json.RawMessage `json:"wire_usage"`
			Expected   json.RawMessage `json:"event_usage"`
			Delta      json.RawMessage `json:"delta_usage"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	mode := "direct"
	if streamed {
		mode = "stream"
	}
	var raw, expected map[string]json.RawMessage
	var delta json.RawMessage
	for _, row := range fixture.Cases {
		if row.Name == "nested" && row.Mode == mode {
			if err := json.Unmarshal(row.Wire, &raw); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(row.Expected, &expected); err != nil {
				t.Fatal(err)
			}
		}
		if row.Name == "server-delta" {
			delta = row.Delta
		}
	}
	if raw == nil || expected == nil || len(delta) == 0 {
		t.Fatal("missing SDK usage captures")
	}
	for _, fields := range []map[string]json.RawMessage{raw, expected} {
		fields["inference_geo"] = json.RawMessage(`"fixture-secret"`)
		fields["future"] = json.RawMessage(`{"secret-key":{"value":"fixture-secret"},"counter":8}`)
		var cache map[string]json.RawMessage
		if err := json.Unmarshal(fields["cache_creation"], &cache); err != nil {
			t.Fatal(err)
		}
		cache["secret-key"] = json.RawMessage(`"fixture-secret"`)
		fields["cache_creation"], err = json.Marshal(cache)
		if err != nil {
			t.Fatal(err)
		}
	}
	wire, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if streamed {
		var deltaFields map[string]json.RawMessage
		if err := json.Unmarshal(delta, &deltaFields); err != nil {
			t.Fatal(err)
		}
		expected["server_tool_use"] = deltaFields["server_tool_use"]
		lines := strings.Split(sse, "\n")
		for i, line := range lines {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event map[string]json.RawMessage
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				t.Fatal(err)
			}
			switch string(event["type"]) {
			case `"message_start"`:
				var message map[string]json.RawMessage
				if err := json.Unmarshal(event["message"], &message); err != nil {
					t.Fatal(err)
				}
				raw["output_tokens"] = json.RawMessage(`0`)
				message["usage"], err = json.Marshal(raw)
				if err != nil {
					t.Fatal(err)
				}
				event["message"], err = json.Marshal(message)
				if err != nil {
					t.Fatal(err)
				}
			case `"message_delta"`:
				event["usage"] = delta
			default:
				continue
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			lines[i] = "data: " + string(encoded)
		}
		sse = strings.Join(lines, "\n")
	}
	want, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	return wire, want, sse
}

// Inspect actual SQL JSON before separately checking its typed archival reader.
func usageRestartEvents(t *testing.T, store *Store, id agent.SessionID, want []byte, count int) {
	t.Helper()
	rows, err := store.conn.QueryContext(context.Background(), "SELECT payload FROM events WHERE session_id=? ORDER BY ordinal", id)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload, "fixture-secret") || strings.Contains(payload, "secret-key") {
			t.Fatal("usage secret reached SQL")
		}
		var event struct {
			Type  string
			Usage json.RawMessage
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "model_end" {
			citationRestartEqual(t, event.Usage, want)
			found++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if found != count {
		t.Fatal("SQL usage events missing", found, count)
	}
	records, err := store.LoadEvents(context.Background(), id, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	found = 0
	for _, record := range records {
		if event, ok := record.Event.ModelEnd(); ok {
			encoded, err := json.Marshal(event.Usage)
			if err != nil {
				t.Fatal(err)
			}
			citationRestartEqual(t, encoded, want)
			found++
		}
	}
	if found != count {
		t.Fatal("archival usage events missing", found, count)
	}
}
