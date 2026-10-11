package traceview

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestHistoricalSummaryDirectPagesMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory-summary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name      string
			Document  string  `json:"document_wire"`
			PageHash  *string `json:"page_sha256"`
			PageError *string `json:"page_error"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 40 {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			ledger, err := Build([]byte(row.Document))
			if row.PageError != nil {
				if err == nil {
					t.Fatal("Source rejects the metrics shape")
				}
				return
			}
			if err != nil || row.PageHash == nil {
				t.Fatal(err)
			}
			page, err := RenderUTF8([]Ledger{ledger}, "mini-loop trace · fixture", time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(page)
			if hex.EncodeToString(hash[:]) != *row.PageHash {
				t.Fatal("complete Source direct page differs")
			}
		})
	}
}
