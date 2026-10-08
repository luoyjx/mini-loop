package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const MaxFiles = 200
const MaxFileBytes = 1_000_000
const PythonSyntaxScope = "Python syntax only (ast.parse); clean output does not mean type- or logic-clean"

type SyntaxKind uint8

const (
	SyntaxInvalid SyntaxKind = iota
	SyntaxClean
	SyntaxProblem
)

// SyntaxResult is a closed parser observation. Non-syntax parser failures are
// errors, not clean results or synthesized filesystem warnings.
type SyntaxResult struct {
	kind    SyntaxKind
	line    int
	message string
}

func CleanSyntax() SyntaxResult { return SyntaxResult{kind: SyntaxClean} }
func NewSyntaxProblem(line int, message string) SyntaxResult {
	if message == "" {
		message = "syntax error"
	}
	return SyntaxResult{SyntaxProblem, line, message}
}
func (result SyntaxResult) Kind() SyntaxKind { return result.kind }
func (result SyntaxResult) Problem() (int, string, bool) {
	return result.line, result.message, result.kind == SyntaxProblem
}

// PythonSyntaxParser must implement the selected CPython ast.parse profile for
// already-decoded text. It must honor context and support concurrent requests.
// No parser implementation or external compiler is selected by this interface.
type PythonSyntaxParser interface {
	Parse(context.Context, string) (SyntaxResult, error)
}

type PythonSyntaxDiagnostics struct{ parser PythonSyntaxParser }

func NewPythonSyntaxDiagnostics(parser PythonSyntaxParser) (*PythonSyntaxDiagnostics, error) {
	if parser == nil {
		return nil, errors.New("Python syntax diagnostics requires an explicit parser")
	}
	return &PythonSyntaxDiagnostics{parser}, nil
}
func (*PythonSyntaxDiagnostics) Scope() string { return PythonSyntaxScope }

func (provider *PythonSyntaxDiagnostics) Diagnose(ctx context.Context, request Request) ([]Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if provider == nil || provider.parser == nil || request.root == "" {
		return nil, errors.New("Python syntax diagnostics is not bound")
	}
	var files []string
	if request.hasTarget {
		// pathlib's suffix treats the hidden basename .py as having no suffix.
		base := filepath.Base(request.target)
		if dot := strings.LastIndexByte(base, '.'); dot > 0 && base[dot:] == ".py" {
			files = []string{request.target}
		}
	} else {
		var err error
		files, err = pythonFiles(ctx, request.root)
		if err != nil {
			return nil, err
		}
	}
	found := make([]Diagnostic, 0)
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(request.root, path)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			found = append(found, Diagnostic{relative, 0, pythonFileError(err), SeverityWarning})
		} else if info.Size() > MaxFileBytes {
			found = append(found, Diagnostic{relative, 0, "skipped: over 1,000,000 bytes", SeverityWarning})
			// Source checks its report stop after ordinary files, not here.
			continue
		} else {
			text, err := readPythonText(ctx, path)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				found = append(found, Diagnostic{relative, 0, pythonFileError(err), SeverityWarning})
			} else {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				result, err := provider.parser.Parse(ctx, text)
				if err != nil {
					return nil, err
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				switch result.Kind() {
				case SyntaxClean:
				case SyntaxProblem:
					found = append(found, NewDiagnostic(relative, result.line, result.message))
				default:
					return nil, errors.New("Python parser returned an invalid result")
				}
			}
		}
		if len(found) >= MaxReported {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return found, nil
}

// Source enumerates the complete tree before its sorted 200-entry selection.
// Symlink directories are not traversed; matching files/directories are retained.
func pythonFiles(ctx context.Context, root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EBADF) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return err
		}
		if path != root && strings.HasSuffix(entry.Name(), ".py") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// PurePath compares component tuples, not slash-joined strings: a/b.py
	// precedes a.py even though '/' sorts after '.' as a byte.
	sort.Slice(files, func(i, j int) bool {
		left, right := strings.Split(files[i], string(filepath.Separator)), strings.Split(files[j], string(filepath.Separator))
		for index := 0; index < min(len(left), len(right)); index++ {
			if left[index] != right[index] {
				return left[index] < right[index]
			}
		}
		return len(left) < len(right)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(files) > MaxFiles {
		files = files[:MaxFiles]
	}
	return files, nil
}

// Like Source, the byte gate is stat-before-read. It is not an atomic file-size
// limit against concurrent growth; UTF-8 replacement and newlines are shared.
func readPythonText(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	reader := pytext.NewReader(file)
	var text strings.Builder
	for count := 0; ; count++ {
		if count%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		r, err := reader.ReadTextRune()
		if err == io.EOF {
			return text.String(), nil
		}
		if err != nil {
			return "", err
		}
		text.WriteRune(r)
	}
}

func pythonFileError(err error) string {
	var pathError *os.PathError
	var errno syscall.Errno
	if !errors.As(err, &pathError) || !errors.As(pathError.Err, &errno) {
		return err.Error()
	}
	message := errno.Error()
	if message != "" {
		message = strings.ToUpper(message[:1]) + message[1:]
	}
	return fmt.Sprintf("[Errno %d] %s: %s", errno, message, pytext.Repr(pathError.Path))
}
