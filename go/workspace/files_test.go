package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type textRecipe struct {
	Unit   string `json:"unit"`
	Repeat int    `json:"repeat"`
	Suffix string `json:"suffix"`
}

type fixtureFile struct {
	Path string     `json:"path"`
	Text textRecipe `json:"text"`
	Hex  string     `json:"hex"`
}

type fileDigest struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type fileCase struct {
	Name           string         `json:"name"`
	Files          []fixtureFile  `json:"files"`
	Directories    []string       `json:"directories"`
	Call           protocol.Block `json:"call"`
	ExpectedOutput string         `json:"expected_output"`
	ExpectedError  bool           `json:"expected_error"`
	ExpectedFiles  []fileDigest   `json:"expected_files"`
}

type fileContract struct {
	ReadCharCap int        `json:"read_char_cap"`
	OutputCap   int        `json:"output_cap"`
	Cases       []fileCase `json:"cases"`
}

func TestFileEffectsMatchPythonContracts(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-file-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract fileContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.ReadCharCap != ReadCharCap || contract.OutputCap != OutputCap || len(contract.Cases) != 34 {
		t.Fatal("Python file contract inventory changed")
	}
	for _, fixture := range contract.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			files, err := NewFiles(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, initial := range fixture.Files {
				data := []byte(strings.Repeat(initial.Text.Unit, initial.Text.Repeat) + initial.Text.Suffix)
				if initial.Hex != "" {
					data, err = hex.DecodeString(initial.Hex)
					if err != nil {
						t.Fatal(err)
					}
				}
				target := filepath.Join(files.Root(), initial.Path)
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, directory := range fixture.Directories {
				if err := os.MkdirAll(filepath.Join(files.Root(), directory), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			call, ok := fixture.Call.ToolUse()
			if !ok {
				t.Fatal("fixture is not a typed tool use")
			}
			var output string
			switch call.Name {
			case protocol.ToolReadFile:
				input, _ := call.Input.ReadFile()
				output, err = files.Read(context.Background(), input)
			case protocol.ToolWriteFile:
				input, _ := call.Input.WriteFile()
				output, err = files.Write(context.Background(), input)
			case protocol.ToolEditFile:
				input, _ := call.Input.EditFile()
				output, err = files.Edit(context.Background(), input)
			default:
				t.Fatalf("unimplemented fixture tool %q", call.Name)
			}
			if (err != nil) != fixture.ExpectedError {
				t.Fatalf("error=%v want failure=%v", err, fixture.ExpectedError)
			}
			if err != nil {
				output = "Error: " + err.Error()
			}
			output = strings.ReplaceAll(output, files.Root(), "$WORKSPACE")
			if output != fixture.ExpectedOutput {
				t.Fatalf("output differs from Python: got %q want %q", preview(output), preview(fixture.ExpectedOutput))
			}
			digests := make([]fileDigest, 0)
			err = filepath.WalkDir(files.Root(), func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				relative, err := filepath.Rel(files.Root(), path)
				if err != nil {
					return err
				}
				hash := sha256.Sum256(data)
				digests = append(digests, fileDigest{filepath.ToSlash(relative), len(data), hex.EncodeToString(hash[:])})
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(digests, fixture.ExpectedFiles) {
				t.Fatalf("file effects differ: got %+v want %+v", digests, fixture.ExpectedFiles)
			}
		})
	}
}

func preview(text string) string {
	if len(text) < 300 {
		return text
	}
	return text[:150] + " ... " + text[len(text)-150:]
}

func TestAtomicWriteFailurePreservesPreviousFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("rename failed")
	err := atomicWrite(context.Background(), path, []byte("replacement"), func(string, string) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("lost write failure: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("failed write changed original: %q %v", data, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "note.txt" {
		t.Fatalf("temporary file leaked: %+v %v", entries, err)
	}
}

func TestCancelledAtomicWriteDoesNotPublish(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := atomicWrite(ctx, path, []byte("replacement"), func(string, string) error { t.Fatal("cancelled write reached rename"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write returned %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("cancelled write changed original: %q %v", data, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leaked: %+v %v", entries, err)
	}
}

func TestResolvePreservesSymlinkParentSemantics(t *testing.T) {
	files, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "child"), filepath.Join(files.Root(), "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Resolve("link/../escape.txt"); err == nil {
		t.Fatal("cleaning hid a symlink escape")
	}
	if err := os.Mkdir(filepath.Join(files.Root(), "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inside", filepath.Join(files.Root(), "safe")); err != nil {
		t.Fatal(err)
	}
	got, err := files.Resolve("safe/../note.txt")
	if err != nil || got != filepath.Join(files.Root(), "note.txt") {
		t.Fatalf("safe symlink parent resolved to %q %v", got, err)
	}
	if err := os.Symlink("loop", filepath.Join(files.Root(), "loop")); err != nil {
		t.Fatal(err)
	}
	if _, err := files.Resolve("loop/note.txt"); err == nil {
		t.Fatal("symlink cycle accepted")
	}
}
