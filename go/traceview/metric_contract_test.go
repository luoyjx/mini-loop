package traceview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestActualPythonMetricConversionsAndPhases(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trace-metrics.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name         string
			Document     string  `json:"document_wire"`
			BuildError   *string `json:"build_error"`
			RenderError  *string `json:"render_error"`
			Metrics      *string `json:"metrics_wire"`
			MetricsError *string `json:"metrics_error"`
			PageHash     *string `json:"page_sha256"`
			MultiHash    *string `json:"multi_sha256"`
			MultiError   *string `json:"multi_error"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 71 {
		t.Fatal(err)
	}
	canonical := func(data []byte) string {
		t.Helper()
		v, err := jsonvalue.Decode(string(data))
		if err != nil {
			t.Fatal(err)
		}
		b, err := jsonvalue.AppendLegacy(nil, v.Sorted())
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	now := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			ledger, err := Build([]byte(row.Document))
			if row.BuildError != nil {
				if err == nil {
					t.Fatal("Source build conversion fails")
				}
				return
			}
			if err != nil {
				t.Fatal("Source build succeeds", err)
			}
			metrics, err := ledger.Metrics.MarshalArchiveJSON()
			if row.MetricsError != nil {
				if err == nil {
					t.Fatal("Source decimal serialization fails")
				}
			} else {
				if err != nil || row.Metrics == nil {
					t.Fatal(err)
				}
				if canonical(metrics) != canonical([]byte(*row.Metrics)) {
					t.Fatalf("Source folded metrics differ: %s", metrics)
				}
			}
			for _, scenario := range []struct {
				copies      int
				hash, error *string
			}{{1, row.PageHash, row.RenderError}, {2, row.MultiHash, row.MultiError}} {
				ledgers := make([]Ledger, scenario.copies)
				for i := range ledgers {
					ledgers[i] = ledger
				}
				page, err := RenderUTF8(ledgers, "mini-loop trace · fixture", now)
				if scenario.error != nil {
					if err == nil {
						t.Fatal("Source render conversion fails")
					}
					continue
				}
				if err != nil || scenario.hash == nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(page)
				if hex.EncodeToString(hash[:]) != *scenario.hash {
					t.Fatal("complete Source metric HTML differs", scenario.copies)
				}
			}
		})
	}
}
