package traceview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestActualPythonLedgerAndCompleteHTML(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trace-view.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CSS   string `json:"css_sha256"`
		JS    string `json:"js_sha256"`
		Cases []struct {
			Name     string
			Document json.RawMessage
			Page     string
			RowCount int `json:"row_count"`
			Omitted  int
			Metrics  struct{ Input, Output int64 }
			Kinds    []Kind
			EndedAt  *float64 `json:"ended_at"`
		}
		File struct{ Raw, Page string }
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, asset := range []struct{ path, hash string }{{"style.css", fixture.CSS}, {"filter.js", fixture.JS}} {
		data, _ := assets.ReadFile(asset.path)
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != asset.hash {
			t.Fatal("source asset drift", asset.path)
		}
	}
	now := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ledger, err := Build(row.Document)
			if err != nil {
				t.Fatal(err)
			}
			kinds := make([]Kind, len(ledger.Rows))
			for i, r := range ledger.Rows {
				kinds[i] = r.Kind
			}
			if len(ledger.Rows) != row.RowCount || ledger.Omitted != row.Omitted || !reflect.DeepEqual(kinds, row.Kinds) || !reflect.DeepEqual(ledger.EndedAt, row.EndedAt) {
				t.Fatal("ledger shape differs", len(ledger.Rows), ledger.Omitted, kinds)
			}
			actual, err := Render([]Ledger{ledger}, "mini-loop trace · fixture", now)
			if err != nil {
				t.Fatal(err)
			}
			if actual != row.Page {
				i := 0
				for i < len(actual) && i < len(row.Page) && actual[i] == row.Page[i] {
					i++
				}
				a, b := i+200, i+200
				if a > len(actual) {
					a = len(actual)
				}
				if b > len(row.Page) {
					b = len(row.Page)
				}
				t.Fatalf("HTML differs at byte %d\nGo %q\nPython %q", i, actual[i:a], row.Page[i:b])
			}
		})
	}
	path := filepath.Join(t.TempDir(), "export.jsonl")
	os.WriteFile(path, []byte(fixture.File.Raw), 0600)
	ledger, err := AssembleFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := Render([]Ledger{ledger}, "mini-loop trace · fixture", now); err != nil || page != fixture.File.Page {
		t.Fatal("export assembly differs from Python")
	}
	for _, invalid := range [][]byte{[]byte(`null`), []byte(`{} {}`), {0xff}} {
		if _, err := Build(invalid); err == nil {
			t.Fatal("invalid document accepted", bytes.TrimSpace(invalid))
		}
	}
}
