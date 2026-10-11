package diagnostics

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type recordingProvider struct {
	scope   string
	rows    []Diagnostic
	failure error
	calls   []Request
}

func (provider *recordingProvider) Scope() string { return provider.scope }
func (provider *recordingProvider) Diagnose(ctx context.Context, request Request) ([]Diagnostic, error) {
	provider.calls = append(provider.calls, request)
	return append([]Diagnostic(nil), provider.rows...), provider.failure
}

func TestToolActualSourceInjectedProviderRecipes(t *testing.T) {
	var source struct {
		Providers []struct {
			Name, Scope, Output, Error string
			Diagnostics                []Diagnostic
			Input                      protocol.DiagnosticsInput
			Calls                      []struct {
				Root   string
				Target *string
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
	for _, row := range source.Providers {
		t.Run(row.Name, func(t *testing.T) {
			files, err := workspace.NewFiles(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			provider := &recordingProvider{scope: row.Scope, rows: row.Diagnostics}
			if row.Error != "" {
				provider.failure = errors.New(row.Error)
			}
			output, err := Tool(context.Background(), files, provider, row.Input.Path)
			if provider.failure != nil {
				if !errors.Is(err, provider.failure) || output != "" {
					t.Fatal(output, err)
				}
			} else if err != nil || output != row.Output {
				t.Fatal(output, err, row.Output)
			}
			if len(provider.calls) != len(row.Calls) {
				t.Fatal(provider.calls)
			}
			for index, call := range provider.calls {
				if call.Root() != files.Root() {
					t.Fatal("foreign workspace")
				}
				target, present := call.Target()
				if expected := row.Calls[index].Target; expected == nil {
					if present {
						t.Fatal("tree scan acquired a target")
					}
				} else if !present || target != filepath.Join(files.Root(), *expected) {
					t.Fatal(target, present, *expected)
				}
			}
		})
	}
}

func TestToolResolvesExplicitSymlinksBeforeProviderAndPreservesVocabulary(t *testing.T) {
	if got := NewDiagnostic("module.py", 2, "syntax").Render(); got != "module.py:2 [error] syntax" {
		t.Fatal("default Source severity changed", got)
	}
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "missing.py"), filepath.Join(files.Root(), "link.py")); err != nil {
		t.Fatal(err)
	}
	provider := &recordingProvider{scope: "only explicit fixture facts", rows: []Diagnostic{{Path: "问题.py", Line: -1, Severity: "information", Message: "line one\nline two"}}}
	path := "link.py"
	output, err := Tool(context.Background(), files, provider, &path)
	if err != nil || !strings.HasPrefix(output, "Error: Path escapes workspace: link.py.") || len(provider.calls) != 0 {
		t.Fatal(output, err)
	}
	path = ""
	output, err = Tool(context.Background(), files, provider, &path)
	if err != nil || output != "[scope: only explicit fixture facts]\n问题.py:-1 [information] line one\nline two" {
		t.Fatal(output, err)
	}
}

type cancelProvider struct {
	cancel    context.CancelFunc
	scopeRead bool
}

func (provider *cancelProvider) Diagnose(context.Context, Request) ([]Diagnostic, error) {
	provider.cancel()
	return nil, nil
}
func (provider *cancelProvider) Scope() string { provider.scopeRead = true; return "cancelled" }

func TestToolCancellationAndRequiredDependencies(t *testing.T) {
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	provider := &cancelProvider{cancel: cancel}
	output, err := Tool(ctx, files, provider, nil)
	if !errors.Is(err, context.Canceled) || output != "" || provider.scopeRead {
		t.Fatal(output, err)
	}
	if _, err := Tool(context.Background(), nil, provider, nil); err == nil {
		t.Fatal("nil workspace admitted")
	}
	if _, err := Tool(context.Background(), files, nil, nil); err == nil {
		t.Fatal("nil provider admitted")
	}
	pre := &recordingProvider{scope: "fixture"}
	if _, err := Tool(ctx, files, pre, nil); !errors.Is(err, context.Canceled) || len(pre.calls) != 0 {
		t.Fatal(err, pre.calls)
	}
}
