package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVariableAuditResolvesInferredAndImportedPayloads(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.test/variableaudit\n\ngo 1.23\n",
		"wire/types.go": `package wire
import "encoding/json"
type Box[T any] struct { Value T }
func Open() any { return nil }
func Raw() json.RawMessage { return nil }
func Pair() (string, any) { return "", nil }
func init() { panic("audit must never execute package code") }
`,
		"service/vars.go": `package service
import (
 "context"
 "crypto"
 "fmt"
 "net/http"
 "sync/atomic"
 "example.test/variableaudit/wire"
)
var Exported any
var private map[string]any
var inferred = wire.Open()
var raw = wire.Raw()
var nested = struct { Payload wire.Box[any] }{}
var callback = func() any { return nil }
var channel chan interface{}
var functionValue = fmt.Sprintf
var foreignGeneric = atomic.Pointer[any]{}
var erased crypto.PublicKey
var pairedClosed, pairedOpen = wire.Pair()
var closed = wire.Box[string]{Value:"ok"}
var client = http.DefaultClient
var ctx = context.Background()
var sentinel = fmt.Errorf("fixed message")
var _ any
const constant = "not a variable"
func local() { var temporary any; _ = temporary }
`,
		"service/inactive.go":  "//go:build variableaudit_inactive\n\npackage service\nvar inactive any\n",
		"service/vars_test.go": "package service\nvar testingOnly any\n",
		"shadow/vars.go":       "package shadow\ntype any struct { name string }\nvar closed any\n",
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
	// These variables introduce no open named type roots: the earlier compiler
	// modes must pass while the new retained-variable mode finds the payloads.
	for _, inspect := range []func(string) (publicReport, error){auditPublic, auditPrivate} {
		prior, err := inspect(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(prior.findings) != 0 {
			t.Fatalf("fixture no longer isolates variable roots: %+v", prior)
		}
	}
	result, err := auditVariables(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, name := range []string{"Exported", "private", "inferred", "raw", "nested", "callback", "channel", "functionValue", "foreignGeneric", "erased", "pairedOpen"} {
		want["example.test/variableaudit/service."+name] = true
	}
	for _, found := range result.findings {
		if !want[found.name] {
			t.Fatalf("unexpected finding: %+v", found)
		}
		if !strings.HasSuffix(found.position.Filename, "service/vars.go") || found.position.Line < 1 {
			t.Fatalf("finding lost declaration location: %+v", found)
		}
		delete(want, found.name)
	}
	if result.packages != 3 || result.variables != 17 || len(result.findings) != 11 || len(want) != 0 {
		t.Fatalf("incomplete variable scope: %+v missing=%v", result, want)
	}
}

func TestVariableAuditFailsOnCompilerInventoryError(t *testing.T) {
	if _, err := auditVariables(t.TempDir()); err == nil {
		t.Fatal("missing module must fail closed")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/broken\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vars.go"), []byte("package broken\nvar value = missing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := auditVariables(root); err == nil {
		t.Fatal("unresolved initializer must fail closed")
	}
}
