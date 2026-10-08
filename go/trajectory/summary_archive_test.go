package trajectory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestHistoricalSummaryArchiveValuesMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Raw    string
			Document     string  `json:"document_wire"`
			Summary      *string `json:"summary_wire"`
			SummaryError *string `json:"summary_error"`
			Listing      *string `json:"listing_wire"`
			ListingError *string `json:"listing_error"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 40 {
		t.Fatal(err)
	}
	canonical := func(value jsonvalue.Value) string {
		t.Helper()
		encoded, err := jsonvalue.AppendLegacy(nil, value.Sorted())
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	source := func(text string) jsonvalue.Value {
		t.Helper()
		value, err := jsonvalue.Decode(text)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			store, err := New(Config{Root: t.TempDir(), CaptureContent: true})
			if err != nil {
				t.Fatal(err)
			}
			id := agent.TrajectoryID("traj_eeeeeeeeeeeeeeeeeeeeeeee")
			if err := os.WriteFile(filepath.Join(store.Root(), string(id)+".jsonl"), []byte(row.Raw), 0600); err != nil {
				t.Fatal(err)
			}
			document, err := store.JSON(id, 1<<63-1)
			if err != nil {
				t.Fatal("Source get succeeds", err)
			}
			if canonical(source(string(document))) != canonical(source(row.Document)) {
				t.Fatal("complete Source document differs")
			}
			summary, err := store.Summary(id)
			if row.SummaryError != nil {
				if !errors.Is(err, ErrMetadataShape) || *row.SummaryError != "AttributeError" {
					t.Fatal("Source summary shape failure", err)
				}
			} else {
				if err != nil || row.Summary == nil {
					t.Fatal(err)
				}
				value, err := summary.ArchiveValue()
				if err != nil || canonical(value) != canonical(source(*row.Summary)) {
					t.Fatal("complete Source summary differs", err)
				}
			}
			listing, err := store.List(agent.TrajectoryQuery{Limit: 100})
			if row.ListingError != nil {
				if !errors.Is(err, ErrMetadataShape) || *row.ListingError != "AttributeError" {
					t.Fatal("Source list shape failure", err)
				}
				return
			}
			if err != nil || row.Listing == nil {
				t.Fatal(err)
			}
			values := make([]jsonvalue.Value, 0, len(listing))
			for _, summary := range listing {
				value, err := summary.ArchiveValue()
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, value)
			}
			if canonical(jsonvalue.ArrayValue(values)) != canonical(source(*row.Listing)) {
				t.Fatal("complete Source list differs")
			}
		})
	}
}
