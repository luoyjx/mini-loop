// Package sessionquery reads historical transcript projections without granting
// execution authority or changing the live conversation.
package sessionquery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

const (
	MaxEpochsScanned = 20
	MaxMatches       = 20
	SnippetChars     = 240
)

type SessionID string
type Epoch int

// ArchiveValue is immutable, closed JSON data. Historical role/content fields
// may be scalars or containers; they are data, never a live protocol message.
type ArchiveValue = jsonvalue.Value

type Message struct {
	Role    ArchiveValue
	Content ArchiveValue
}

// Reader returns messages in stored order. Calls select only the supplied session
// and epoch; gaps are empty epochs. The caller owns authorization and lifetime.
type Reader interface {
	TranscriptEpoch(context.Context, SessionID) (Epoch, error)
	LoadMessages(context.Context, SessionID, Epoch) ([]Message, error)
}

type Match struct {
	Epoch   Epoch        `json:"epoch"`
	Index   int          `json:"index"`
	Role    ArchiveValue `json:"role"`
	Snippet string       `json:"snippet"`
}

type Result struct {
	Matches       []Match `json:"matches"`
	FirstEpoch    Epoch   `json:"first_epoch"`
	CurrentEpoch  Epoch   `json:"current_epoch"`
	EpochsSkipped Epoch   `json:"epochs_skipped"`
}

// Search preserves Source's direct-search semantics, including an empty needle.
// ToolSearch applies the model-facing empty-query refusal and whitespace trimming.
func Search(ctx context.Context, reader Reader, id SessionID, query string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if reader == nil {
		return Result{}, errors.New("transcript reader is required")
	}
	if !utf8.ValidString(query) {
		return Result{}, jsonvalue.ErrSurrogate
	}
	current, err := reader.TranscriptEpoch(ctx, id)
	if err != nil {
		return Result{}, err
	}
	current = max(1, current)
	first := max(1, current-MaxEpochsScanned+1)
	result := Result{Matches: []Match{}, FirstEpoch: first, CurrentEpoch: current, EpochsSkipped: first - 1}
	needle := pytext.Lower(query)
	for epoch := first; ; epoch++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		messages, err := reader.LoadMessages(ctx, id, epoch)
		if err != nil {
			return Result{}, err
		}
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		for index, message := range messages {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			text, err := messageText(message.Content)
			if err != nil {
				return Result{}, err
			}
			lower := pytext.Lower(text)
			position := strings.Index(lower, needle)
			if position < 0 {
				continue
			}
			// Python indexes the original text using the lowercased position,
			// even when full Unicode lowercasing expands a character.
			start := max(0, utf8.RuneCountInString(lower[:position])-SnippetChars/2)
			runes := []rune(text)
			start = min(start, len(runes))
			result.Matches = append(result.Matches, Match{epoch, index, message.Role, string(runes[start:min(len(runes), start+SnippetChars)])})
			if len(result.Matches) >= MaxMatches {
				return result, nil
			}
		}
		// Avoid overflowing an Epoch at the largest representable value.
		if epoch == current {
			break
		}
	}
	return result, nil
}

func messageText(content ArchiveValue) (string, error) {
	if text, ok := content.Text(); ok {
		if !utf8.ValidString(text) {
			return "", jsonvalue.ErrSurrogate
		}
		return text, nil
	}
	compact, err := content.MarshalLegacyUTF8()
	if err != nil {
		return "", err
	}
	var out strings.Builder
	quoted, escaped := false, false
	for _, c := range compact {
		out.WriteByte(c)
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
		} else if c == '"' {
			quoted = true
		} else if c == ',' || c == ':' {
			out.WriteByte(' ')
		}
	}
	return out.String(), nil
}

// ToolSearch renders the Source read-tool response. A null/no-op reader can
// return an empty transcript; an absent reader or session is a distinct refusal.
func ToolSearch(ctx context.Context, reader Reader, id SessionID, query string) (string, error) {
	if reader == nil || id == "" {
		return "Error: no durable transcript on this surface", nil
	}
	trimmed := pytext.Strip(query)
	if trimmed == "" {
		return "Error: an empty query matches everything and answers nothing", nil
	}
	result, err := Search(ctx, reader, id, trimmed)
	if err != nil {
		return "", err
	}
	return Render(query, result), nil
}

func Render(query string, result Result) string {
	var lines []string
	for _, match := range result.Matches {
		lines = append(lines, fmt.Sprintf("epoch %d #%d (%s): ...%s...", match.Epoch, match.Index, match.Role.PythonString(), match.Snippet))
	}
	if len(lines) == 0 {
		lines = append(lines, fmt.Sprintf("No matches for %s in the searched transcript.", pytext.Repr(query)))
	} else if len(lines) >= MaxMatches {
		lines = append(lines, fmt.Sprintf("... (capped at %d matches; refine the query)", MaxMatches))
	}
	output := strings.Join(lines, "\n")
	if result.EpochsSkipped != 0 {
		output += fmt.Sprintf("\n(searched epochs %d-%d only; %d earlier epoch(s) were not scanned -- older history exists beyond this search)", result.FirstEpoch, result.CurrentEpoch, result.EpochsSkipped)
	}
	return output
}
