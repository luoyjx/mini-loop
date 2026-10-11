package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditDetectsRetainedPayloadsAcrossPlatformFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"types.go": `package sample
import j "encoding/json"
type Generic[T any] struct { Value T }
type Closed struct { Name string; Counts map[string]int }
type Open struct { Payload map[string][]any }
type Empty interface{}
type Raw = j.RawMessage
func decode() { var wire struct { Raw j.RawMessage }; _ = wire }
`,
		"other_windows.go": `//go:build windows
package sample
type Platform struct { Value interface{} }
`,
		"ignored_test.go": `package sample; type Fixture struct { Value any }`,
	}
	for path, source := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.files != 2 || result.types != 6 {
		t.Fatalf("unexpected scope: %+v", result)
	}
	want := map[string]bool{"Open": true, "Empty": true, "Raw": true, "Platform": true}
	for _, found := range result.findings {
		if !want[found.name] {
			t.Fatalf("unexpected finding: %+v", found)
		}
		delete(want, found.name)
	}
	if len(want) != 0 {
		t.Fatalf("missing findings: %v", want)
	}
}

func TestAuditFailsOnSyntaxErrorsAndHandlesDotJSONImport(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "types.go")
	if err := os.WriteFile(path, []byte(`package sample; import . "encoding/json"; type Raw struct { Value RawMessage }`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := audit(root)
	if err != nil || len(result.findings) != 1 {
		t.Fatalf("dot import: %+v %v", result, err)
	}
	if err := os.WriteFile(path, []byte(`package sample; type Broken struct {`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := audit(root); err == nil {
		t.Fatal("syntax error accepted")
	}
}
