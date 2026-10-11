package sqlitestate

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"os"
	"path/filepath"
	"testing"
)

func TestWholeUsageSourceSnapshotsSurviveSQLiteReopen(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-whole-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Usage      jsonvalue.Value `json:"event_usage"`
			Stages     []struct {
				Event string
				Usage jsonvalue.Value `json:"event_usage"`
			} `json:"usage_stages"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	type row struct {
		Name, Mode string
		Usage      jsonvalue.Value `json:"event_usage"`
	}
	rows := []row{}
	for _, c := range fixture.Cases {
		if c.Mode == "direct" {
			rows = append(rows, row{c.Name, c.Mode, c.Usage})
		}
		for _, stage := range c.Stages {
			rows = append(rows, row{c.Name, stage.Event, stage.Usage})
		}
	}
	if len(rows) != 43 {
		t.Fatal("Source snapshot inventory changed")
	}
	out, err := json.Marshal(struct{ Cases []row }{rows})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "source-snapshots.json")
	if err := os.WriteFile(path, out, 0600); err != nil {
		t.Fatal(err)
	}
	assertSDKUsageEventReopen(t, path, 43)
}
