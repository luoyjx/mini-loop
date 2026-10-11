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
)

func TestHistoricalMetadataLedgersAndPagesMatchSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-trajectory-metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Raw string
			Document  string `json:"direct_wire"`
			PageHash  string `json:"page_sha256"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 11 {
		t.Fatal(err)
	}
	now := time.Date(2000, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			ledger, err := Build([]byte(row.Document))
			if err != nil {
				t.Fatal(err)
			}
			verify := func(ledger Ledger) {
				t.Helper()
				page, err := RenderUTF8([]Ledger{ledger}, "mini-loop trace · fixture", now)
				if err != nil {
					t.Fatal(err)
				}
				hash := sha256.Sum256(page)
				if hex.EncodeToString(hash[:]) != row.PageHash {
					t.Fatalf("complete Source HTML differs: %s", hex.EncodeToString(hash[:]))
				}
			}
			verify(ledger)
			root := t.TempDir()
			path := filepath.Join(root, "traj_dddddddddddddddddddddddd.jsonl")
			if err := os.WriteFile(path, []byte(row.Raw), 0600); err != nil {
				t.Fatal(err)
			}
			assembled, err := AssembleFile(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			verify(assembled)
			for _, target := range []string{"traj_dddddddddddddddddddddddd", "metadata-session"} {
				loaded, err := Load(context.Background(), target, root)
				if err != nil || len(loaded) != 1 {
					t.Fatal(target, err)
				}
				verify(loaded[0])
			}
		})
	}
}
