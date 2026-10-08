package traceview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestWorkflowArchiveLedgersAndFinalUTF8PagesMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-workflow-trace-view.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Raw string
			Document  string  `json:"document_wire"`
			Contents  string  `json:"contents_wire"`
			PageHash  *string `json:"page_sha256"`
			PageError *string `json:"page_error"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 14 {
		t.Fatal(err)
	}
	now := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			ledger, err := Build([]byte(row.Document))
			if err != nil {
				t.Fatal(err)
			}
			value, err := jsonvalue.Decode(row.Contents)
			if err != nil {
				t.Fatal(err)
			}
			contents, _ := value.Array()
			if len(contents) != len(ledger.Rows) {
				t.Fatal("row count")
			}
			for i, source := range contents {
				text, _ := source.Text()
				if ledger.Rows[i].Content != text {
					t.Fatalf("content %d differs: %q; %q", i, ledger.Rows[i].Content, text)
				}
			}
			verify := func(ledger Ledger) {
				t.Helper()
				page, err := RenderUTF8([]Ledger{ledger}, "mini-loop trace · fixture", now)
				if row.PageError != nil {
					if err == nil || *row.PageError != "UnicodeEncodeError" {
						t.Fatal("final UTF-8 boundary", err)
					}
					return
				}
				if err != nil || row.PageHash == nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(page)
				if hex.EncodeToString(hash[:]) != *row.PageHash {
					t.Fatalf("complete Source HTML differs: %s", hex.EncodeToString(hash[:]))
				}
			}
			verify(ledger)
			root := t.TempDir()
			path := filepath.Join(root, "traj_cccccccccccccccccccccccc.jsonl")
			if err := os.WriteFile(path, []byte(row.Raw), 0600); err != nil {
				t.Fatal(err)
			}
			assembled, err := AssembleFile(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			verify(assembled)
			for _, target := range []string{"traj_cccccccccccccccccccccccc", "s"} {
				loaded, err := Load(context.Background(), target, root)
				if err != nil || len(loaded) != 1 {
					t.Fatal(target, err)
				}
				verify(loaded[0])
			}
		})
	}
}
