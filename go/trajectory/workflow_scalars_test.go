package trajectory

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestWorkflowTrajectoryScalarsMatchActualSourceFiles(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-trajectory.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name        string
			Capture     bool
			Input       string  `json:"input_wire"`
			AppendError *string `json:"append_error"`
			Document    string  `json:"document_wire"`
			Records     []string
			Iter        []string `json:"iter_events"`
			Summary     jsonvalue.Value
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 10 {
		t.Fatal(err)
	}
	canonical := func(data []byte) string {
		t.Helper()
		value, err := jsonvalue.Decode(string(data))
		if err != nil {
			t.Fatal(err)
		}
		var normalize func(jsonvalue.Value) jsonvalue.Value
		normalize = func(value jsonvalue.Value) jsonvalue.Value {
			if value.Kind() == jsonvalue.Object {
				fields := []jsonvalue.Field{}
				for _, name := range value.Keys() {
					child, _ := value.Lookup(name)
					if name == "started_at" || name == "ended_at" || name == "duration_ms" {
						child, _ = jsonvalue.Decode("0")
					} else {
						child = normalize(child)
					}
					fields = append(fields, jsonvalue.Field{Name: name, Value: child})
				}
				return jsonvalue.ObjectValue(fields)
			}
			if items, ok := value.Array(); ok {
				for i, v := range items {
					items[i] = normalize(v)
				}
				return jsonvalue.ArrayValue(items)
			}
			return value
		}
		encoded, err := jsonvalue.AppendLegacyDefault(normalize(value).Sorted())
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name+map[bool]string{true: "/capture", false: "/private"}[row.Capture], func(t *testing.T) {
			store, err := New(Config{Root: t.TempDir(), CaptureContent: row.Capture})
			if err != nil {
				t.Fatal(err)
			}
			id := agent.TrajectoryID("traj_" + strings.Repeat("c", 24))
			store.newID = func() (agent.TrajectoryID, error) { return id, nil }
			_, err = store.Start(agent.TrajectoryStart{Session: "s", Owner: "alice", RunIndex: 1, Input: "input"})
			if err != nil {
				t.Fatal(err)
			}
			record, err := agent.DecodeStoredEvent([]byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			err = store.Append(id, agent.TrajectoryRecord{Record: record})
			if (err != nil) != (row.AppendError != nil) {
				t.Fatal("append boundary", err, row.AppendError)
			}
			if row.AppendError != nil && *row.AppendError != "UnicodeEncodeError" {
				t.Fatal("Source failure changed")
			}
			output := "done"
			duration := 0.0
			if err := store.Finish(id, agent.TrajectoryFinish{Status: agent.TrajectoryCompleted, Output: &output, DurationMS: &duration}); err != nil {
				t.Fatal(err)
			}
			document, err := store.JSON(id, 8*1024*1024)
			if err != nil {
				t.Fatal(err)
			}
			if canonical(document) != canonical([]byte(row.Document)) {
				t.Fatalf("document: %s; expected %s", document, row.Document)
			}
			summary, err := store.Summary(id)
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(summary)
			expected, _ := row.Summary.MarshalJSON()
			if canonical(encoded) != canonical(expected) {
				t.Fatalf("summary: %s; expected %s", encoded, expected)
			}
			var buffer bytes.Buffer
			if err := store.Stream(context.Background(), id, &buffer); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
			if len(lines) != len(row.Records) {
				t.Fatal(len(lines), len(row.Records))
			}
			for i, line := range lines {
				if canonical([]byte(line)) != canonical([]byte(row.Records[i])) {
					t.Fatal("line mismatch", line, row.Records[i])
				}
			}
			count := 0
			err = store.VisitRecords(context.Background(), id, agent.TrajectoryEventQuery{Types: []agent.SessionEventKind{"workflow_paused"}, Limit: 10}, func(line []byte) error {
				if count >= len(row.Iter) || canonical(line) != canonical([]byte(row.Iter[count])) {
					t.Fatal("iterator mismatch")
				}
				count++
				return nil
			})
			if err != nil || count != len(row.Iter) {
				t.Fatal(count, err)
			}
		})
	}
}
