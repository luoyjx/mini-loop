package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublicAuditResolvesImportedAliasesAndInstantiatedFields(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.test/audit\n\ngo 1.23\n",
		"wire/types.go": `package wire
import "encoding/json"
type Raw = json.RawMessage
type Empty = interface{}
type Box[T any] struct { Value T }
type Numeric interface { ~int | ~float64 }
`,
		"service/types.go": `package service
import (
 "crypto"
 "context"
 "net/http"
 "example.test/audit/wire"
)
type Imported struct { Data map[string][]wire.Raw; State wire.Empty }
type Boxed struct { Data wire.Box[any] }
type ExternalAlias struct { Key crypto.PublicKey }
type hidden = wire.Raw
type HiddenField struct { Value hidden }
type Closed struct { Data wire.Box[string]; Next *Closed }
type Platform struct { Client *http.Client; Context context.Context }
type Callback interface { Receive(wire.Empty) }
`,
		"shadow/types.go": `package shadow
type any struct { Name string }
type Closed struct { Value any }
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
	result, err := auditPublic(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"example.test/audit/wire.Raw":              true,
		"example.test/audit/wire.Empty":            true,
		"example.test/audit/service.Imported":      true,
		"example.test/audit/service.Boxed":         true,
		"example.test/audit/service.ExternalAlias": true,
		"example.test/audit/service.HiddenField":   true,
		"example.test/audit/service.Callback":      true,
	}
	for _, found := range result.findings {
		if !want[found.name] {
			t.Fatalf("unexpected finding: %+v", found)
		}
		delete(want, found.name)
	}
	if result.packages != 3 || result.types != 12 || len(want) != 0 {
		t.Fatalf("incomplete public scope: %+v missing=%v", result, want)
	}
}

func TestPublicAuditRejectsCompilerInventoryFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/broken\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "broken.go"), []byte("package broken; type Bad struct {"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := auditPublic(root); err == nil {
		t.Fatal("failed compiler inventory accepted")
	}
}
