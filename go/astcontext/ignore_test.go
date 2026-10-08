package astcontext

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestIgnoreMatcherMatchesSource(t *testing.T) {
	var fixture struct {
		PathspecVersion string `json:"pathspec_version"`
		Patterns        []struct {
			Name, Error  string
			ErrorClass   string `json:"error_class"`
			Lines, Paths []string
			PatternCount int `json:"pattern_count"`
			Matches      []struct {
				Include *bool
				Index   *int
			}
		}
	}
	data, err := os.ReadFile("../testdata/python-ast-ignore.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.PathspecVersion != "1.1.1" || len(fixture.Patterns) != 41 {
		t.Fatal("Source ignore profile changed")
	}
	for _, row := range fixture.Patterns {
		t.Run(row.Name, func(t *testing.T) {
			spec, err := compileIgnore(row.Lines)
			if row.Error != "" {
				if err == nil {
					t.Fatal("Source compile refusal became native success")
				}
				// Python re.error diagnostics expose its regex syntax/offsets;
				// native range compiler faults retain failure, not Python wording.
				if row.ErrorClass != "error" && err.Error() != row.Error {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(spec.patterns) != row.PatternCount {
				t.Fatal(len(spec.patterns), row.PatternCount)
			}
			if len(row.Matches) != len(row.Paths) {
				t.Fatal("incomplete Source matches")
			}
			for i, path := range row.Paths {
				result := spec.check(path)
				expected := row.Matches[i]
				kind, index := unmatched, -1
				if expected.Include != nil {
					if *expected.Include {
						kind = ignored
					} else {
						kind = kept
					}
					index = *expected.Index
				}
				if result.disposition != kind || result.index != index {
					t.Fatal(path, result, kind, index)
				}
			}
		})
	}
}

func TestIgnoreControlDecodingUsesSourceSnapshotRecipes(t *testing.T) {
	var fixture struct {
		Snapshots []struct {
			Recipe struct {
				Name  string
				Files []struct{ Path, Text, Hex string }
			}
		}
	}
	data, err := os.ReadFile("../testdata/python-ast-snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Snapshots {
		if row.Recipe.Name == "ignore-invalid-utf8" {
			data, err := hex.DecodeString(row.Recipe.Files[0].Hex)
			if err != nil {
				t.Fatal(err)
			}
			if lines := ignoreLines(data); !reflect.DeepEqual(lines, []string{"ignored.py"}) {
				t.Fatal(lines)
			}
		}
		if row.Recipe.Name != "ignore-unicode-splitlines" {
			continue
		}
		lines := ignoreLines([]byte(row.Recipe.Files[0].Text))
		if !reflect.DeepEqual(lines, []string{"ignored.py", "other.py"}) {
			t.Fatal(lines)
		}
	}
	if lines := ignoreLines([]byte("ig\xffnored.py\n")); !reflect.DeepEqual(lines, []string{"ignored.py"}) {
		t.Fatal(lines)
	}
	if lines := ignoreLines([]byte("a\r\nb\rc\v\f\x1c\x1d\x1e\u0085\u2028\u2029")); !reflect.DeepEqual(lines, []string{"a", "b", "c", "", "", "", "", "", "", ""}) {
		t.Fatal(lines)
	}
	if lines := ignoreLines(nil); len(lines) != 0 {
		t.Fatal(lines)
	}
}
