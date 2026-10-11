package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type fileRecipe struct {
	Path, Prefix, Unit, Suffix, Hex, Target string
	Repeat                                  int
	Remove                                  bool
}
type expectedEntry struct {
	Name, Description, Digest string
	SourceDigest              string `json:"source_digest"`
}
type expectedLoad struct {
	Input  protocol.LoadSkillInput
	SHA256 string
	Failed bool
}
type expectedProblem struct {
	Message string
	Count   int
}
type skillCase struct {
	Name             string
	Files, Mutations []fileRecipe
	Missing          bool
	Descriptions     string
	Entries          []expectedEntry
	Loads            []expectedLoad
	Problems         []expectedProblem
}
type skillContract struct {
	BodyCap        int `json:"body_cap"`
	DescriptionCap int `json:"description_cap"`
	CatalogueCap   int `json:"catalogue_cap"`
	Cases          []skillCase
}

func writeRecipe(t *testing.T, root, outside string, recipe fileRecipe) {
	t.Helper()
	target := filepath.Join(root, recipe.Path)
	if recipe.Remove {
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if recipe.Target == "$DIRECTORY" {
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if recipe.Target != "" {
		if err := os.Symlink(strings.ReplaceAll(recipe.Target, "$OUTSIDE", outside), target); err != nil {
			t.Fatal(err)
		}
		return
	}
	payload := []byte(recipe.Prefix + strings.Repeat(recipe.Unit, recipe.Repeat) + recipe.Suffix)
	if recipe.Hex != "" {
		var err error
		payload, err = hex.DecodeString(recipe.Hex)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(target, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogMatchesPythonSkillContracts(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-skills.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract skillContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if len(contract.Cases) != 19 || contract.BodyCap != MaxBody || contract.DescriptionCap != MaxDescription || contract.CatalogueCap != MaxCatalogue {
		t.Fatal("skill contract inventory drift")
	}
	for _, fixture := range contract.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			parent := t.TempDir()
			root, outside := filepath.Join(parent, "skills"), filepath.Join(parent, "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("outside"), 0o644); err != nil {
				t.Fatal(err)
			}
			if !fixture.Missing {
				if err := os.Mkdir(root, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, recipe := range fixture.Files {
				writeRecipe(t, root, outside, recipe)
			}
			catalog, err := NewCatalog(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if got := catalog.Descriptions(); got != fixture.Descriptions {
				t.Fatalf("descriptions differ: %q", got)
			}
			entries := []expectedEntry{}
			for _, entry := range catalog.Entries() {
				entries = append(entries, expectedEntry{entry.Name, entry.Description, entry.Digest, entry.SourceDigest})
			}
			if !reflect.DeepEqual(entries, fixture.Entries) {
				t.Fatalf("entry digests differ: %+v want %+v", entries, fixture.Entries)
			}
			for _, recipe := range fixture.Mutations {
				writeRecipe(t, root, outside, recipe)
			}
			for _, load := range fixture.Loads {
				output, err := catalog.Load(context.Background(), load.Input)
				if (err != nil) != load.Failed {
					t.Fatalf("load %s: failure=%v want %v", load.Input.Name, err, load.Failed)
				}
				if err != nil {
					output = "Error: " + err.Error()
				}
				digest := sha256.Sum256([]byte(output))
				if hex.EncodeToString(digest[:]) != load.SHA256 {
					t.Fatalf("load %s differs: %q", load.Input.Name, output)
				}
			}
			problems := []expectedProblem{}
			normalize := strings.NewReplacer(root, "$SKILLS", outside, "$OUTSIDE")
			for _, problem := range catalog.Problems() {
				problems = append(problems, expectedProblem{normalize.Replace(problem.Message), problem.Count})
			}
			if !reflect.DeepEqual(problems, fixture.Problems) {
				t.Fatalf("problems=%+v want %+v", problems, fixture.Problems)
			}
		})
	}
}

func TestSourceRetainsBoundedTextAndHashesWholeFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "SKILL.md")
	text := "---\n" + strings.Repeat(" ", 1_000_000) + "name: note\n---\n\n" + strings.Repeat("中", MaxBody*10) + "\x1c\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := readSource(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(text))
	if source.name != "note" || len([]rune(source.body)) != MaxBody || source.bodyLength != MaxBody*10 || source.digest != hex.EncodeToString(digest[:]) {
		t.Fatalf("unbounded or partial source: name=%q body=%d total=%d digest=%s", source.name, len([]rune(source.body)), source.bodyLength, source.digest)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewCatalog(ctx, root); err != context.Canceled {
		t.Fatalf("cancelled construction: %v", err)
	}
	if _, err := readSource(ctx, path); err != context.Canceled {
		t.Fatalf("cancelled source: %v", err)
	}
}

func TestSharedCatalogDeduplicatesConcurrentRefusalsAndReportsEvictions(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: note\n---\nold"), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := NewCatalog(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for attempt := 0; attempt < 10; attempt++ {
				if _, err := catalog.Load(context.Background(), protocol.LoadSkillInput{Name: "note"}); err == nil {
					t.Error("changed source was served")
				}
				_ = catalog.Descriptions()
			}
		}()
	}
	workers.Wait()
	problems := catalog.Problems()
	if len(problems) != 1 || problems[0].Count != 80 || catalog.ProblemStatistics() != (ProblemStatistics{Total: 80}) {
		t.Fatalf("dedup drift: %+v %+v", problems, catalog.ProblemStatistics())
	}
	for i := 0; i <= MaxProblems; i++ {
		catalog.report(ProblemName, fmt.Sprintf("invalid name %d", i))
	}
	if len(catalog.Problems()) != MaxProblems || catalog.ProblemStatistics() != (ProblemStatistics{Total: 131, Dropped: 2}) {
		t.Fatal("problem bound lost occurrence or eviction counts")
	}
	problems[0].Message = "mutated"
	if catalog.Problems()[0].Message == "mutated" {
		t.Fatal("problem snapshot aliases catalogue")
	}
}
