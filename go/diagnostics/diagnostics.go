// Package diagnostics defines the language-provider seam and Source tool report.
// It does not claim that an injected provider is a Python syntax parser.
package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/workspace"
)

const MaxReported = 50

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is a provider observation, not execution authority. Severity is
// provider vocabulary: Source preserves arbitrary labels without rewriting them.
type Diagnostic struct {
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	Message  string   `json:"message"`
	Severity Severity `json:"severity"`
}

// NewDiagnostic supplies Source's default severity. An explicit struct field
// may retain other provider vocabulary, including an intentionally empty label.
func NewDiagnostic(path string, line int, message string) Diagnostic {
	return Diagnostic{Path: path, Line: line, Message: message, Severity: SeverityError}
}

func (diagnostic Diagnostic) Render() string {
	return fmt.Sprintf("%s:%d [%s] %s", diagnostic.Path, diagnostic.Line, diagnostic.Severity, diagnostic.Message)
}

// Request can only be constructed from the trusted workspace and its resolved
// selection. Target is absent for a tree scan; an explicit directory is preserved.
type Request struct {
	root      string
	target    string
	hasTarget bool
}

func (request Request) Root() string           { return request.root }
func (request Request) Target() (string, bool) { return request.target, request.hasTarget }

// Provider implementations honor context cancellation and return an owned
// observation snapshot. Fleet/shared providers must support concurrent calls.
// Scope is read after Diagnose, matching Source's report boundary.
type Provider interface {
	Diagnose(context.Context, Request) ([]Diagnostic, error)
	Scope() string
}

// Tool applies explicit-path confinement before invoking the selected provider.
// It preserves provider order and labels, and caps reporting even for providers
// whose own work bounds differ. A clean result always names its provider scope.
func Tool(ctx context.Context, files *workspace.Files, provider Provider, path *string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if files == nil || provider == nil {
		return "", errors.New("diagnostics requires a workspace and provider")
	}
	request := Request{root: files.Root()}
	if path != nil && *path != "" {
		target, err := files.Resolve(*path)
		if err != nil {
			var escape *workspace.PathEscapeError
			if errors.As(err, &escape) {
				return "Error: " + err.Error(), nil
			}
			return "", err
		}
		request.target, request.hasTarget = target, true
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	found, err := provider.Diagnose(ctx, request)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	header := "[scope: " + provider.Scope() + "]"
	if len(found) == 0 {
		return header + "\nNo diagnostics.", nil
	}
	count := min(len(found), MaxReported)
	lines := make([]string, 1, count+2)
	lines[0] = header
	for _, diagnostic := range found[:count] {
		lines = append(lines, diagnostic.Render())
	}
	if len(found) >= MaxReported {
		lines = append(lines, fmt.Sprintf("... (reporting capped at %d)", MaxReported))
	}
	return strings.Join(lines, "\n"), nil
}
