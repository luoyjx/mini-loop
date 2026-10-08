package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestHistoricalMetadataDocumentsRetainSourceValues(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Raw string
			Document  string `json:"document_wire"`
			Summary   string `json:"summary_wire"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 11 {
		t.Fatal(err)
	}
	canonical := func(data []byte) string {
		t.Helper()
		value, err := jsonvalue.Decode(string(data))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := jsonvalue.AppendLegacy(nil, value.Sorted())
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			store, err := New(Config{Root: t.TempDir(), CaptureContent: true})
			if err != nil {
				t.Fatal(err)
			}
			id := agent.TrajectoryID("traj_dddddddddddddddddddddddd")
			if err := os.WriteFile(filepath.Join(store.Root(), string(id)+".jsonl"), []byte(row.Raw), 0600); err != nil {
				t.Fatal(err)
			}
			document, err := store.JSON(id, 1<<63-1)
			if err != nil {
				t.Fatal(err)
			}
			if canonical(document) != canonical([]byte(row.Document)) {
				t.Fatalf("full Source document differs: %s", document)
			}
			summary, err := store.Summary(id)
			if err != nil {
				t.Fatal(err)
			}
			var source struct{ Model, Workspace, Build *string }
			summaryValue, err := jsonvalue.Decode(row.Summary)
			if err != nil {
				t.Fatal(err)
			}
			projection, err := summaryValue.Select("model", "workspace", "build").MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(projection, &source); err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][2]*string{{summary.Model, source.Model}, {summary.Workspace, source.Workspace}, {summary.Build, source.Build}} {
				if (pair[0] == nil) != (pair[1] == nil) || pair[0] != nil && *pair[0] != *pair[1] {
					t.Fatal("summary metadata differs")
				}
			}
			rows, err := store.List(agent.TrajectoryQuery{Limit: 100})
			if err != nil || len(rows) != 1 {
				t.Fatal("streaming list", err)
			}
		})
	}
}
