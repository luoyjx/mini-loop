package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf8"
)

func TestCLIWorkflowArchivePagesMatchSourceExitAndPrivateFile(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-workflow-trace-view.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Rows []struct {
			Name, Raw string
			Exit      int    `json:"cli_exit"`
			Empty     bool   `json:"cli_empty"`
			Mode      uint32 `json:"cli_mode"`
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Rows) != 14 {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "traj_cccccccccccccccccccccccc.jsonl")
			if err := os.WriteFile(path, []byte(row.Raw), 0600); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{path, "traj_cccccccccccccccccccccccc", "s"} {
				output := filepath.Join(root, "page.html")
				var stdout, stderr bytes.Buffer
				exit := execute(context.Background(), []string{target, "--root", root, "--output", output}, nil, &stdout, &stderr)
				if exit != row.Exit {
					t.Fatal(target, exit, stderr.String())
				}
				body, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				info, _ := os.Stat(output)
				if (len(body) == 0) != row.Empty || uint32(info.Mode().Perm()) != row.Mode || !utf8.Valid(body) {
					t.Fatal("Source output boundary", target, len(body), info.Mode())
				}
			}
		})
	}
}
