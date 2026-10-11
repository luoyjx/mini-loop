package improvement

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
)

type archiveMaskFunc func(string) string

func (mask archiveMaskFunc) MaskText(value string) string { return mask(value) }

func TestArchiveSourceAppendContracts(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-improvement-archive-append.json")
	if err != nil {
		t.Fatal(err)
	}
	// Raw JSON stays in this transient source-fixture decoder, not the archive service.
	var fixture struct {
		Cases []struct {
			Name     string            `json:"name"`
			Proposal ProposalFields    `json:"proposal"`
			Owner    *ArchiveOwnerID   `json:"owner"`
			ParentID *ProposalID       `json:"parent_id"`
			Secrets  []string          `json:"secrets"`
			Fault    string            `json:"fault"`
			ID       ProposalID        `json:"proposal_id"`
			Rows     []json.RawMessage `json:"rows"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "[literal]*?")
			switch recipe.Fault {
			case "root-file":
				if err := os.WriteFile(root, []byte("occupied"), 0600); err != nil {
					t.Fatal(err)
				}
			case "archive-directory":
				if err := os.MkdirAll(root+"/archive.jsonl", 0700); err != nil {
					t.Fatal(err)
				}
			}
			var mask ArchiveMasker
			if len(recipe.Secrets) > 0 {
				mask = archiveMaskFunc(func(value string) string {
					for _, secret := range recipe.Secrets {
						value = strings.ReplaceAll(value, secret, "[MASK]")
					}
					return value
				})
			}
			archive := NewArchive(root, mask)
			archive.newID = func() (ProposalID, error) { return recipe.ID, nil }
			archive.now = func() float64 { return 1700000000.125 }
			id, err := archive.Record(recipe.Proposal, ArchiveRecordOptions{Owner: recipe.Owner, ParentID: recipe.ParentID})
			if err != nil || id != recipe.ID {
				t.Fatalf("record: %q %v", id, err)
			}
			if len(recipe.Rows) == 0 {
				return
			}
			body, err := os.ReadFile(root + "/archive.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Count(body, []byte{'\n'}) != 1 || body[len(body)-1] != '\n' {
				t.Fatalf("not one JSONL record: %q", body)
			}
			var got, want map[string]json.RawMessage
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(recipe.Rows[0], &want); err != nil {
				t.Fatal(err)
			}
			// Decode each scalar/list independently: source indentation and Unicode escapes differ.
			if len(got) != len(want) {
				t.Fatalf("keys differ: %s", body)
			}
			for key, expected := range want {
				var gotValue, wantValue interface{}
				if err := json.Unmarshal(got[key], &gotValue); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(expected, &wantValue); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(gotValue, wantValue) {
					t.Fatalf("%s: %s != %s", key, got[key], expected)
				}
			}
		})
	}
}

func TestArchiveProjectionFailureNeverWritesRaw(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	archive := NewArchive(root, archiveMaskFunc(func(string) string { panic("private secret") }))
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("constructor IO: %v", err)
	}
	if id, err := archive.Record(ProposalFields{}, ArchiveRecordOptions{}); id != "" || err == nil || strings.Contains(err.Error(), "private secret") {
		t.Fatalf("projection: %q %v", id, err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed projection wrote: %v", err)
	}
	archive.masker = nil
	archive.newID = func() (ProposalID, error) { return "", errors.New("entropy unavailable") }
	if id, err := archive.Record(ProposalFields{}, ArchiveRecordOptions{}); id != "" || err == nil {
		t.Fatalf("entropy failure: %q %v", id, err)
	}
	archive.newID = func() (ProposalID, error) { return "imp_fixed", nil }
	archive.now = func() float64 { return math.NaN() }
	if id, err := archive.Record(ProposalFields{}, ArchiveRecordOptions{}); id != "" || err == nil {
		t.Fatalf("nonfinite clock: %q %v", id, err)
	}
	var absent *Archive
	if _, err := absent.Record(ProposalFields{}, ArchiveRecordOptions{}); err == nil {
		t.Fatal("nil archive accepted")
	}
	if _, err := (&Archive{}).Record(ProposalFields{}, ArchiveRecordOptions{}); err == nil {
		t.Fatal("zero archive accepted")
	}
}

func TestArchiveAppendsConcurrentCompleteRecords(t *testing.T) {
	root := t.TempDir()
	archive := NewArchive(root, nil)
	var wait sync.WaitGroup
	ids := make(chan ProposalID, 64)
	for i := 0; i < 64; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			id, err := archive.Record(ProposalFields{}, ArchiveRecordOptions{})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- id
		}()
	}
	wait.Wait()
	close(ids)
	expected := map[ProposalID]bool{}
	format := regexp.MustCompile(`^imp_[0-9a-f]{12}$`)
	for id := range ids {
		if expected[id] || !format.MatchString(string(id)) {
			t.Fatalf("bad ID: %q", id)
		}
		expected[id] = true
	}
	body, err := os.ReadFile(root + "/archive.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(body, []byte{'\n'}), []byte{'\n'})
	if len(lines) != 64 {
		t.Fatalf("rows: %d", len(lines))
	}
	for _, line := range lines {
		var row archiveRow
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatal(err)
		}
		if !expected[row.ProposalID] {
			t.Fatalf("unexpected/duplicate row %q", row.ProposalID)
		}
		delete(expected, row.ProposalID)
		if row.Owner != "anonymous" || row.CreatedAt <= 0 {
			t.Fatalf("metadata: %+v", row)
		}
	}
	if len(expected) != 0 {
		t.Fatalf("lost records: %d", len(expected))
	}
}

func TestArchiveMasksDetachedProjectionBeforeLock(t *testing.T) {
	root := t.TempDir()
	objective := "before"
	paths := []string{"before"}
	archive := NewArchive(root, nil)
	archive.masker = archiveMaskFunc(func(value string) string {
		if !archive.mu.TryLock() {
			t.Fatal("mask callback ran under IO lock")
		}
		archive.mu.Unlock()
		objective = "after"
		paths[0] = "after"
		return value
	})
	if _, err := archive.Record(ProposalFields{Objective: &objective, TouchesVerifiers: &paths}, ArchiveRecordOptions{}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(root + "/archive.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var row archiveRow
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatal(err)
	}
	if *row.Objective != "before" || (*row.TouchesVerifiers)[0] != "before" {
		t.Fatalf("projection aliased inputs: %s", body)
	}
	// A second append keeps the first row and records the caller's new values.
	if _, err := archive.Record(ProposalFields{Objective: &objective}, ArchiveRecordOptions{}); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(root + "/archive.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(body, []byte{'\n'}) != 2 {
		t.Fatalf("append lost history: %s", body)
	}
}
