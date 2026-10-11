package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateAuditResolvesUnreachableAliasesAndBuildSelection(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.test/privateaudit\n\ngo 1.23\n",
		"wire/types.go": `package wire
import "encoding/json"
type Raw = json.RawMessage
type Box[T any] struct { Value T }
`,
		"service/types.go": `package service
import (
 "crypto"
 "context"
 "net/http"
 "example.test/privateaudit/wire"
)
type hiddenAlias = wire.Raw
type hiddenImported struct { data hiddenAlias }
type hiddenBox struct { data wire.Box[any] }
type hiddenExternalAlias struct { key crypto.PublicKey }
type hiddenCallback interface { Receive(wire.Raw) }
type closed struct { data wire.Box[string]; next *closed }
type platform struct { client *http.Client; context context.Context }
`,
		"service/inactive.go": `//go:build privateaudit_inactive

package service
type inactive struct { data any }
`,
		"shadow/types.go": `package shadow
type any struct { name string }
type closed struct { value any }
`,
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	public, err := auditPublic(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, found := range public.findings {
		if found.name != "example.test/privateaudit/wire.Raw" {
			t.Fatalf("private root unexpectedly included by public audit: %+v", found)
		}
	}
	result, err := auditPrivate(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"example.test/privateaudit/service.hiddenAlias":         true,
		"example.test/privateaudit/service.hiddenImported":      true,
		"example.test/privateaudit/service.hiddenBox":           true,
		"example.test/privateaudit/service.hiddenExternalAlias": true,
		"example.test/privateaudit/service.hiddenCallback":      true,
	}
	for _, found := range result.findings {
		if !want[found.name] {
			t.Fatalf("unexpected finding: %+v", found)
		}
		delete(want, found.name)
	}
	if result.packages != 3 || result.types != 9 || len(result.findings) != 5 || len(want) != 0 {
		t.Fatalf("incomplete private scope: %+v missing=%v", result, want)
	}
}

func TestPrivateAuditRejectsCompilerInventoryFailure(t *testing.T) {
	if _, err := auditPrivate(t.TempDir()); err == nil {
		t.Fatal("missing module must not report a passing audit")
	}
}
