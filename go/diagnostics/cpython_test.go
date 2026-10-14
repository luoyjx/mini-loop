package diagnostics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

func TestCPythonParserActualSource62Profile(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-syntax-parser.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Implementation string `json:"implementation"`
		Version        []int  `json:"version"`
		Operation      string `json:"operation"`
		Encoding       string `json:"encoding"`
		Cases          []struct {
			Name       string          `json:"name"`
			SourceRepr string          `json:"source_repr"`
			SourceUTF8 jsonvalue.Value `json:"source_utf8"`
			Kind       string          `json:"kind"`
			Line       int             `json:"line"`
			Message    string          `json:"message"`
			ErrorClass string          `json:"error_class"`
		} `json:"cases"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Implementation != "cpython" || len(fixture.Version) != 3 || fixture.Version[0] != 3 || fixture.Version[1] != 11 || fixture.Version[2] != 3 || fixture.Operation != "ast.parse(source)" || fixture.Encoding != "UTF-8 with surrogatepass, base64" || len(fixture.Cases) != 62 {
		t.Fatal("fixed actual CPython Source profile changed")
	}
	parser, err := NewCPythonParser(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, row := range fixture.Cases {
		if seen[row.Name] {
			t.Fatal("duplicate Source recipe", row.Name)
		}
		seen[row.Name] = true
		counts[row.Kind]++
		t.Run(row.Name, func(t *testing.T) {
			encoded, ok := row.SourceUTF8.Text()
			if !ok {
				t.Fatal("Source bytes must use the typed base64 text variant")
			}
			bytes, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			source := string(bytes)
			if row.Name == "lone-surrogate" && (utf8.ValidString(source) || !strings.Contains(source, "\xed\xa0\x80") || strings.Contains(source, "\ufffd")) {
				t.Fatal("Source surrogatepass bytes were replaced before parser admission")
			}
			result, err := parser.Parse(context.Background(), source)
			switch row.Kind {
			case "clean":
				if err != nil || result.Kind() != SyntaxClean {
					t.Fatal(result, err)
				}
			case "syntax":
				line, message, ok := result.Problem()
				if err != nil || !ok || line != row.Line || message != row.Message {
					t.Fatal(line, message, err, row)
				}
				// The existing public SyntaxResult exposes line/message, not a
				// Python exception hierarchy. The helper keeps the exact subclass.
				observed, observeErr := parser.run(context.Background(), "parse", source)
				if observeErr != nil || observed.Class != row.ErrorClass {
					t.Fatal(observed, observeErr, row.ErrorClass)
				}
			case "error":
				var fault *ParserFault
				if !errors.As(err, &fault) || fault.Class() != row.ErrorClass || fault.Error() != row.Message || result.Kind() != SyntaxInvalid {
					t.Fatal(result, err, row)
				}
			default:
				t.Fatal("unhandled Source disposition", row.Kind)
			}
		})
	}
	if counts["clean"] != 24 || counts["syntax"] != 36 || counts["error"] != 2 || len(counts) != 3 {
		t.Fatal(counts)
	}
}

func TestCPythonParserConcurrentCallsAndInputBoundaries(t *testing.T) {
	parser, err := NewCPythonParser(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for index := 0; index < 6; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := parser.Parse(context.Background(), "变量 = '✓'\n")
			if err != nil || result.Kind() != SyntaxClean {
				t.Error(result, err)
			}
		}()
	}
	wait.Wait()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := parser.Parse(ctx, "x=1"); !errors.Is(err, context.Canceled) || result.Kind() != SyntaxInvalid {
		t.Fatal(result, err)
	}
	if result, err := parser.Parse(context.Background(), strings.Repeat("x", MaxParserSourceBytes+1)); err == nil || result.Kind() != SyntaxInvalid {
		t.Fatal("oversized source accepted", err)
	}
	provider, err := NewPythonSyntaxDiagnostics(parser)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "nul.py"), []byte("x=1\x00\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if found, err := provider.Diagnose(context.Background(), Request{root: root}); err == nil || found != nil {
		t.Fatal("parser fault became a clean/file diagnostic", found, err)
	}
}
