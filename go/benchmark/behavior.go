package benchmark

import (
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type Behavior struct {
	Rounds        int64 `json:"rounds"`
	ToolCalls     int64 `json:"tool_calls"`
	ToolErrors    int64 `json:"tool_errors"`
	RepeatedReads int64 `json:"repeated_reads"`
}
type readWindow struct {
	path                string
	offset, limit       int
	hasOffset, hasLimit bool
}

func failedExitNote(text string) bool {
	text = strings.TrimRightFunc(text, pytext.Space)
	if !strings.HasSuffix(text, ")") {
		return false
	}
	start := strings.LastIndex(text, "(exit ")
	if start < 0 {
		return false
	}
	digits := text[start+6 : len(text)-1]
	if len(digits) == 0 || digits[0] < '1' || digits[0] > '9' {
		return false
	}
	for _, digit := range digits[1:] {
		if _, ok := pytext.DecimalDigit(digit); !ok {
			return false
		}
	}
	return true
}

// BehavioralMetrics counts transcript motion, independently of effect judgments.
// Distinct windows of one path are paging; only identical windows are repeats.
func BehavioralMetrics(messages []protocol.Message) Behavior {
	var result Behavior
	seen := make(map[readWindow]bool)
	for _, message := range messages {
		if message.Role == protocol.RoleAssistant {
			result.Rounds++
		}
		blocks, ok := message.Content.Blocks()
		if !ok {
			continue
		}
		for _, block := range blocks {
			if use, ok := block.ToolUse(); ok && message.Role == protocol.RoleAssistant {
				result.ToolCalls++
				if read, ok := use.Input.ReadFile(); ok && read.Path != "" {
					window := readWindow{path: read.Path}
					if read.Offset != nil {
						window.hasOffset, window.offset = true, *read.Offset
					}
					if read.Limit != nil {
						window.hasLimit, window.limit = true, *read.Limit
					}
					if seen[window] {
						result.RepeatedReads++
					}
					seen[window] = true
				}
			} else if output, ok := block.ToolResult(); ok {
				trimmed := strings.TrimLeftFunc(output.Content, pytext.Space)
				if strings.HasPrefix(trimmed, "Error") || strings.HasPrefix(trimmed, "Unknown tool") || failedExitNote(trimmed) {
					result.ToolErrors++
				}
			}
		}
	}
	return result
}
