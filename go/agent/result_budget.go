package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type ToolResultBudgetOptions struct {
	MaxBytes     int
	PreviewChars int
}

func DefaultToolResultBudgetOptions() ToolResultBudgetOptions {
	return ToolResultBudgetOptions{MaxBytes: 200_000, PreviewChars: 2_000}
}

type ToolResultBudgetContext struct {
	Messages []protocol.Message
	Files    *workspace.Files
	Secrets  TextMasker
	// Now returns Unix milliseconds. Nil uses the system clock.
	Now func() int64
}

type ToolResultBudgetResult struct {
	Messages  []protocol.Message
	Persisted int
}

// ToolResultBudget spills only the newest user tool-result batch. It does not
// call a model or compact other messages. Disk writes are masked; previews retain
// Source text and Python slicing, including negative preview lengths. A later
// failure retains successful file writes but returns the original transcript.
func ToolResultBudget(ctx context.Context, value ToolResultBudgetContext, options ToolResultBudgetOptions) (ToolResultBudgetResult, error) {
	result := ToolResultBudgetResult{Messages: append([]protocol.Message(nil), value.Messages...)}
	index := -1
	for i := len(value.Messages) - 1; i >= 0; i-- {
		if value.Messages[i].Role == protocol.RoleUser && hasBlock(value.Messages[i], protocol.BlockToolResult) {
			index = i
			break
		}
	}
	if index < 0 {
		return result, nil
	}
	blocks, _ := value.Messages[index].Content.Blocks()
	type target struct {
		index    int
		original protocol.ToolResultBlock
		weight   int
	}
	var targets []target
	total := 0
	for i, block := range blocks {
		if output, ok := block.ToolResult(); ok {
			// Source encodes every original target before checking the budget or
			// creating directories. Masking cannot rescue an invalid original.
			if !utf8.ValidString(output.Content) {
				return result, protocol.ErrResultUTF8
			}
			total += len(output.Content)
			targets = append(targets, target{i, output, utf8.RuneCountInString(output.Content)})
		}
	}
	if total <= options.MaxBytes {
		return result, nil
	}
	if value.Files == nil {
		return result, errors.New("tool result budget requires a bound workspace")
	}
	if err := value.Files.EnsureDirectory(ctx, filepath.Join(".task_outputs", "tool-results")); err != nil {
		return result, err
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].weight > targets[j].weight })
	persisted := 0
	for _, target := range targets {
		if total <= options.MaxBytes {
			break
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		now := time.Now().UnixMilli()
		if value.Now != nil {
			now = value.Now()
		}
		path := filepath.Join(".task_outputs", "tool-results", fmt.Sprintf("%s-%d.txt", safeResultID(target.original.ToolUseID), now))
		resolved, err := value.Files.Resolve(path)
		if err != nil {
			return result, err
		}
		if _, err := value.Files.Write(ctx, protocol.WriteFileInput{Path: path, Content: maskedText(value.Secrets, target.original.Content)}); err != nil {
			return result, err
		}
		runes := []rune(target.original.Content)
		preview := string(runes[:pythonSliceStop(len(runes), options.PreviewChars)])
		replacement := fmt.Sprintf("<persisted-output path=\"%s\" bytes=\"%d\">\n%s\n</persisted-output>", resolved, len(target.original.Content), preview)
		blocks[target.index] = protocol.NewToolResult(target.original.ToolUseID, replacement, target.original.IsError)
		total += len(replacement) - len(target.original.Content)
		persisted++
	}
	if persisted > 0 {
		result.Messages[index].Content = protocol.BlockContent(blocks...)
		result.Persisted = persisted
	}
	return result, nil
}
