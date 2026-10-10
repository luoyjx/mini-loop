package reload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventFilterMatchesActualSourceDefaultPolicy(t *testing.T) {
	var contract struct {
		DefaultBackend string `json:"default_backend"`
		Filters        []struct {
			Name  string `json:"name"`
			Paths []struct {
				Path    string `json:"path"`
				Allowed bool   `json:"allowed"`
			} `json:"paths"`
		} `json:"filters"`
	}
	data, err := os.ReadFile("../testdata/python-watchfiles.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.DefaultBackend != "WatchFilesReload" {
		t.Fatal("unexpected Source backend", contract.DefaultBackend)
	}
	checked := 0
	for _, profile := range contract.Filters {
		if profile.Name != "default" {
			continue
		}
		for _, language := range []Language{PythonSource, GoSource} {
			filter, err := NewEventFilter(language)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range profile.Paths {
				path := row.Path
				if language == GoSource {
					path = strings.ReplaceAll(strings.ReplaceAll(path, ".py", ".go"), ".PY", ".GO")
				}
				path = filepath.Join(t.TempDir(), filepath.FromSlash(path))
				if got := filter.Matches(path); got != row.Allowed {
					t.Fatalf("%s %q: got %t want %t", language, path, got, row.Allowed)
				}
				checked++
			}
		}
	}
	if checked != 28 {
		t.Fatal("missing actual default Source cases", checked)
	}
}

func TestEventFilterInitializationAndLexicalBoundary(t *testing.T) {
	if (EventFilter{}).Matches("a.go") {
		t.Fatal("uninitialized filter matched")
	}
	if _, err := NewEventFilter(Language("unknown")); err == nil {
		t.Fatal("unknown language admitted")
	}
	filter, err := NewEventFilter("")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"removed.go", "directory.go", "../outside.go"} {
		if !filter.Matches(path) {
			t.Fatal("filter inferred filesystem/root authority", path)
		}
	}
}
