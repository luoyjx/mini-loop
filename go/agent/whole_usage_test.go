package agent

import (
	"encoding/json"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"os"
	"path/filepath"
	"testing"
)

func TestWholeUsageSourceSnapshotsMaskCloneArchiveAndAuthority(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-whole-usage.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode, Outcome string
			Usage               jsonvalue.Value `json:"event_usage"`
			Stages              []struct {
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
	assertSDKUsageMaskArchive(t, path, 43)
	for _, raw := range []string{"null", "13", "[{}]"} {
		usage, err := protocol.DecodeSDKUsage([]byte(raw), false)
		if err != nil {
			t.Fatal(err)
		}
		usage.InputTokens = protocol.IntegerUsageCount(99)
		if count, ok := PromptTokens(usage); ok || count != 0 {
			t.Fatal("whole observation acquired meter authority")
		}
		if usage.Validate() == nil {
			t.Fatal("mutated whole usage accepted")
		}
	}
}
