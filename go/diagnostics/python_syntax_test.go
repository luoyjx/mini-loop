package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type sourceParseObservation struct {
	SHA           string `json:"source_sha256"`
	Kind, Message string
	Line          int
	ErrorClass    string `json:"error_class"`
}

// This parser only replays actual CPython observations at the selected seam.
// It proves native scan/read/control flow, not a native Python grammar.
type sourceParserReplay struct {
	observations []sourceParseObservation
	index        int
	fault        error
}

func (parser *sourceParserReplay) Parse(ctx context.Context, text string) (SyntaxResult, error) {
	if err := ctx.Err(); err != nil {
		return SyntaxResult{}, err
	}
	if parser.index >= len(parser.observations) {
		return SyntaxResult{}, errors.New("unexpected parser invocation")
	}
	call := parser.observations[parser.index]
	parser.index++
	if hash := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); hash != call.SHA {
		return SyntaxResult{}, fmt.Errorf("parser input/order mismatch at call %d", parser.index)
	}
	switch call.Kind {
	case "clean":
		return CleanSyntax(), nil
	case "syntax":
		return NewSyntaxProblem(call.Line, call.Message), nil
	case "fault":
		parser.fault = errors.New(call.Message)
		return SyntaxResult{}, parser.fault
	default:
		return SyntaxResult{}, errors.New("unknown Source parser observation")
	}
}

func TestPythonSyntaxNativeScannerActualSourceRecipes(t *testing.T) {
	var source struct {
		Limits struct {
			Files     int
			FileBytes int `json:"file_bytes"`
			Reported  int
		}
		Rows []struct {
			Name, Output, Error string
			ErrorClass          string `json:"error_class"`
			Input               json.RawMessage
			Parsed              []sourceParseObservation
			Files               []struct {
				Path, Text, Symlink string
				Directory           bool
				Repeat              int
				BytesHex            string `json:"bytes_hex"`
			}
		}
	}
	data, err := os.ReadFile("../testdata/python-diagnostics.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if source.Limits.Files != MaxFiles || source.Limits.FileBytes != MaxFileBytes || source.Limits.Reported != MaxReported {
		t.Fatal("Source work/report limits changed")
	}
	replayed, rejected := 0, 0
	for _, row := range source.Rows {
		t.Run(row.Name, func(t *testing.T) {
			base := t.TempDir()
			outside := filepath.Join(base, "outside")
			if err := os.MkdirAll(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "bad.py"), []byte("x =\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			files, err := workspace.NewFiles(filepath.Join(base, row.Name))
			if err != nil {
				t.Fatal(err)
			}
			materialize := func(text string) string {
				return strings.ReplaceAll(strings.ReplaceAll(text, "<workspace>", files.Root()), "<outside>", outside)
			}
			normalize := func(text string) string {
				return strings.ReplaceAll(strings.ReplaceAll(text, files.Root(), "<workspace>"), outside, "<outside>")
			}
			for _, entry := range row.Files {
				path := filepath.Join(files.Root(), entry.Path)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				switch {
				case entry.Directory:
					if err := os.MkdirAll(path, 0o700); err != nil {
						t.Fatal(err)
					}
				case entry.Symlink != "":
					if err := os.Symlink(materialize(entry.Symlink), path); err != nil {
						t.Fatal(err)
					}
				default:
					count := entry.Repeat
					if count == 0 {
						count = 1
					}
					body := []byte(strings.Repeat(entry.Text, count))
					if entry.BytesHex != "" {
						var err error
						body, err = hex.DecodeString(entry.BytesHex)
						if err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(path, body, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			input, err := protocol.DecodeToolInput(protocol.ToolDiagnostics, row.Input)
			if err != nil {
				// Native typed admission intentionally refuses Python's arbitrary
				// keyword/list payloads, including its dynamically falsey empty list.
				if row.Name != "nonempty-list-path" && row.Name != "empty-list-path" && row.Name != "unknown-key" {
					t.Fatal(err)
				}
				rejected++
				return
			}
			value, _ := input.Diagnostics()
			if value.Path != nil {
				path := materialize(*value.Path)
				value.Path = &path
			}
			parser := &sourceParserReplay{observations: row.Parsed}
			provider, err := NewPythonSyntaxDiagnostics(parser)
			if err != nil {
				t.Fatal(err)
			}
			output, err := Tool(context.Background(), files, provider, value.Path)
			if row.ErrorClass != "" {
				if parser.fault == nil || !errors.Is(err, parser.fault) || err.Error() != row.Error || output != "" {
					t.Fatal(output, err, row.Error)
				}
			} else if err != nil || normalize(output) != row.Output {
				t.Fatal(normalize(output), err, row.Output)
			}
			if parser.index != len(row.Parsed) {
				t.Fatal("parser calls omitted", parser.index, len(row.Parsed))
			}
			replayed++
		})
	}
	if replayed != 34 || rejected != 3 {
		t.Fatal("unexpected Source/native admission coverage", replayed, rejected)
	}
}

type syntaxParserFunc func(context.Context, string) (SyntaxResult, error)

func (parser syntaxParserFunc) Parse(ctx context.Context, text string) (SyntaxResult, error) {
	return parser(ctx, text)
}

func TestPythonSyntaxInvalidResultsFaultsAndCancellation(t *testing.T) {
	if _, err := NewPythonSyntaxDiagnostics(nil); err == nil {
		t.Fatal("nil parser selected")
	}
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(files.Root(), "one.py"), []byte("pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"invalid", "fault", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			marker := errors.New("parser failure")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			parser := syntaxParserFunc(func(context.Context, string) (SyntaxResult, error) {
				switch kind {
				case "fault":
					return SyntaxResult{}, marker
				case "cancel":
					cancel()
					return CleanSyntax(), nil
				default:
					return SyntaxResult{}, nil
				}
			})
			provider, err := NewPythonSyntaxDiagnostics(parser)
			if err != nil {
				t.Fatal(err)
			}
			output, err := Tool(ctx, files, provider, nil)
			if err == nil || output != "" {
				t.Fatal("parser failure became clean output", output, err)
			}
			if kind == "fault" && !errors.Is(err, marker) || kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
	if _, err := (&PythonSyntaxDiagnostics{}).Diagnose(context.Background(), Request{}); err == nil {
		t.Fatal("unbound provider admitted")
	}
	result := NewSyntaxProblem(0, "")
	line, message, problem := result.Problem()
	if result.Kind() != SyntaxProblem || !problem || line != 0 || message != "syntax error" {
		t.Fatal(result)
	}
}
