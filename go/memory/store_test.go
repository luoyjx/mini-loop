package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/secrets"
)

type expectedRecord struct {
	Record
	BodySHA256     string `json:"body_sha256"`
	BodyCharacters int    `json:"body_characters"`
}
type memoryStep struct {
	Op     string
	Owner  *OwnerID
	Scoped bool
	Input
	Repeat          int
	Query           string
	Limit           *int
	Memories        []Input
	Path, Text, Hex string
	Remove          bool
	Output          string
	Records         []expectedRecord
	IndexExists     bool `json:"index_exists"`
}

func TestActualPythonMemoryStore(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-memory-store.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Secret string
			Files        []struct {
				Path, Text, Hex string
				Directory       bool
			}
			Steps      []memoryStep
			FinalFiles []struct{ Path, SHA256 string } `json:"final_files"`
			Problems   []Problem
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			for _, file := range row.Files {
				path := filepath.Join(root, file.Path)
				if file.Directory {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
					continue
				}
				payload := []byte(file.Text)
				if file.Hex != "" {
					payload, err = hex.DecodeString(file.Hex)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, payload, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var masker Masker
			if row.Secret != "" {
				registry := secrets.New(secrets.Config{})
				registry.RegisterValue("TEST_SECRET", row.Secret)
				masker = registry
			}
			store, err := NewStore(ctx, root, masker)
			if err != nil {
				t.Fatal(err)
			}
			for i, step := range row.Steps {
				var output string
				records := []Record{}
				var bound *ScopedStore
				if step.Scoped {
					if step.Owner == nil {
						t.Fatal("scoped recipe has no owner")
					}
					bound, err = Bind(store, *step.Owner)
					if err != nil {
						t.Fatal(err)
					}
				}
				switch step.Op {
				case "write":
					input := step.Input
					if step.Repeat > 1 {
						input.Body = repeat(input.Body, step.Repeat)
					}
					if bound != nil {
						output, err = bound.Write(ctx, input)
					} else {
						if step.Owner == nil {
							t.Fatal("write recipe has no owner")
						}
						output, err = store.Write(ctx, *step.Owner, input)
					}
				case "replace":
					if bound != nil {
						err = bound.ReplaceAll(ctx, step.Memories, step.Origin)
					} else {
						err = store.ReplaceAll(ctx, step.Owner, step.Memories, step.Origin)
					}
				case "list":
					if bound != nil {
						records, err = bound.List(ctx)
					} else {
						records, err = store.List(ctx, step.Owner)
					}
				case "index":
					if bound != nil {
						output, err = bound.Index(ctx)
					} else {
						output, err = store.Index(ctx, step.Owner)
					}
				case "search":
					limit := 5
					if step.Limit != nil {
						limit = *step.Limit
					}
					if bound != nil {
						records, err = bound.Search(ctx, step.Query, limit)
					} else {
						records, err = store.Search(ctx, step.Owner, step.Query, limit)
					}
				case "flush":
					err = store.Flush(ctx)
				case "mutate":
					path := filepath.Join(root, step.Path)
					if step.Remove {
						err = os.Remove(path)
					} else {
						payload := []byte(step.Text)
						if step.Hex != "" {
							payload, err = hex.DecodeString(step.Hex)
							if err != nil {
								t.Fatal(err)
							}
						}
						err = os.WriteFile(path, payload, 0600)
					}
				default:
					t.Fatal(step.Op)
				}
				if err != nil {
					t.Fatalf("step %d %s: %v", i, step.Op, err)
				}
				if output != step.Output {
					t.Fatalf("step %d output %q != %q", i, output, step.Output)
				}
				if len(records) != len(step.Records) {
					t.Fatalf("step %d records %v != %v", i, records, step.Records)
				}
				for j, record := range records {
					expected := step.Records[j]
					sum := sha256.Sum256([]byte(record.Body))
					if utf8.RuneCountInString(record.Body) != expected.BodyCharacters || hex.EncodeToString(sum[:]) != expected.BodySHA256 {
						t.Fatalf("step %d body mismatch", i)
					}
					record.Body = ""
					if !reflect.DeepEqual(record, expected.Record) {
						t.Fatalf("step %d record %+v != %+v", i, record, expected.Record)
					}
				}
				_, statErr := os.Stat(filepath.Join(root, "MEMORY.md"))
				if (statErr == nil) != step.IndexExists {
					t.Fatalf("step %d index state", i)
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, entry := range entries {
				if !entry.IsDir() {
					paths = append(paths, entry.Name())
				}
			}
			sort.Strings(paths)
			if len(paths) != len(row.FinalFiles) {
				t.Fatal(paths, row.FinalFiles)
			}
			for i, path := range paths {
				data, err := os.ReadFile(filepath.Join(root, path))
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(data)
				if path != row.FinalFiles[i].Path || hex.EncodeToString(sum[:]) != row.FinalFiles[i].SHA256 {
					t.Fatal("file mismatch", path)
				}
			}
			problems, err := store.Problems(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != len(row.Problems) {
				t.Fatal(problems, row.Problems)
			}
			for i, p := range problems {
				if p != row.Problems[i] {
					t.Fatal(p, row.Problems[i])
				}
			}
		})
	}
}
func repeat(text string, n int) string {
	var out []byte
	for i := 0; i < n; i++ {
		out = append(out, text...)
	}
	return string(out)
}

func TestConcurrentScopedReplacementAndCancellation(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := NewStore(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	alice, _ := Bind(store, "alice")
	bob, _ := Bind(store, "bob")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := bob.Write(ctx, Input{Name: fmt.Sprintf("b-%d", i), Body: "protected"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			if err := alice.ReplaceAll(ctx, []Input{{Name: "a", Body: "replacement"}}, Consolidated); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
	records, err := bob.List(ctx)
	if err != nil || len(records) != 20 {
		t.Fatal(len(records), err)
	}
	records[0].Body = "changed"
	again, _ := bob.List(ctx)
	if again[0].Body != "protected" {
		t.Fatal("cache exposed mutable records")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := alice.Write(cancelled, Input{Name: "cancelled"}); err != context.Canceled {
		t.Fatal(err)
	}
	records, err = alice.List(ctx)
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	if _, err := Bind(nil, "alice"); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err := NewStore(cancelled, filepath.Join(root, "uncached"), nil); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "uncached")); !os.IsNotExist(err) {
		t.Fatal("cancelled constructor changed disk")
	}
}
