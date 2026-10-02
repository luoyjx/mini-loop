package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

const CompactionPrompt = "You are performing a context checkpoint compaction. Write a handoff summary for another instance of this agent that will resume the task. Include: current progress and key decisions made; important context, constraints, or user preferences; what remains to be done, as clear next steps; any critical data, examples, or references needed to continue. Be concise, structured, and focused on letting the next instance continue seamlessly."
const SummaryPrefix = "A previous instance of this agent worked on this task and left the handoff summary below. Build on what is already done; do not repeat completed work."

type CompactionKind string

const (
	CompactBudget CompactionKind = "budget"
	CompactSnip   CompactionKind = "snip"
	CompactMicro  CompactionKind = "micro"
	CompactAuto   CompactionKind = "auto"
	CompactFailed CompactionKind = "failed"
)

type SummaryReceipt struct {
	Transcript             string
	ReplacedMessages       int
	ReplacedTokensEstimate int
	InputTokens            int
	OutputTokens           int
	Model                  string
}
type CompactionEvent struct {
	kind    CompactionKind
	count   int
	summary SummaryReceipt
	failure string
}

func (event CompactionEvent) Kind() CompactionKind { return event.kind }
func (event CompactionEvent) Count() (int, bool) {
	switch event.kind {
	case CompactBudget, CompactSnip, CompactMicro:
		return event.count, true
	}
	return 0, false
}
func (event CompactionEvent) Summary() (SummaryReceipt, bool) {
	return event.summary, event.kind == CompactAuto
}
func (event CompactionEvent) Failure() (string, bool) {
	return event.failure, event.kind == CompactFailed
}

// The context contains a detached transcript, a bound workspace and an explicit
// model dependency. A custom strategy returns a typed replacement; Session
// validates pairing before accepting it. Summaries never observe the live meter.
type CompactionContext struct {
	Messages []protocol.Message
	Files    *workspace.Files
	Provider Provider
	Model    string
	Meter    TokenMeter
	Envelope string
}
type CompactionResult struct {
	Messages []protocol.Message
	Events   []CompactionEvent
}
type Compactor interface {
	MaybeCompact(context.Context, CompactionContext) (CompactionResult, error)
	Compact(context.Context, CompactionContext) (CompactionResult, error)
}

func hasBlock(message protocol.Message, kind protocol.BlockKind) bool {
	blocks, _ := message.Content.Blocks()
	for _, block := range blocks {
		if block.Kind() == kind {
			return true
		}
	}
	return false
}

func SnipCompact(messages []protocol.Message, maxMessages int) ([]protocol.Message, int) {
	if len(messages) <= maxMessages || maxMessages < 4 {
		return append([]protocol.Message(nil), messages...), 0
	}
	headEnd := min(3, len(messages))
	if hasBlock(messages[headEnd-1], protocol.BlockToolUse) {
		for headEnd < len(messages) && hasBlock(messages[headEnd], protocol.BlockToolResult) {
			headEnd++
		}
	}
	tailBudget := max(1, maxMessages-headEnd-1)
	tailStart := max(headEnd, len(messages)-tailBudget)
	if tailStart > 0 && hasBlock(messages[tailStart], protocol.BlockToolResult) && hasBlock(messages[tailStart-1], protocol.BlockToolUse) {
		tailStart--
	}
	removed := tailStart - headEnd
	if removed <= 0 {
		return append([]protocol.Message(nil), messages...), 0
	}
	result := append([]protocol.Message(nil), messages[:headEnd]...)
	result = append(result, protocol.Message{Role: protocol.RoleUser, Content: protocol.PlainContent(fmt.Sprintf("[snipped %d messages from conversation middle]", removed))})
	return append(result, messages[tailStart:]...), removed
}

func commaCount(count int) string {
	text := fmt.Sprint(count)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

func MicroCompact(messages []protocol.Message) ([]protocol.Message, int) {
	result := append([]protocol.Message(nil), messages...)
	lastAssistant := -1
	names := make(map[string]protocol.ToolName)
	for i, message := range messages {
		if message.Role != protocol.RoleAssistant {
			continue
		}
		lastAssistant = i
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if use, ok := block.ToolUse(); ok {
				names[use.ID] = use.Name
			}
		}
	}
	type position struct{ message, block int }
	var consumed []position
	for i, message := range messages {
		if i >= lastAssistant || message.Role != protocol.RoleUser {
			continue
		}
		blocks, _ := message.Content.Blocks()
		for j, block := range blocks {
			if block.Kind() == protocol.BlockToolResult {
				consumed = append(consumed, position{i, j})
			}
		}
	}
	cleared := 0
	for _, pos := range consumed[:max(0, len(consumed)-3)] {
		blocks, _ := result[pos.message].Content.Blocks()
		value, _ := blocks[pos.block].ToolResult()
		weight := utf8.RuneCountInString(value.Content)
		if weight <= 100 {
			continue
		}
		label := ""
		if name := names[value.ToolUseID]; name != "" {
			label = string(name) + ", "
		}
		blocks[pos.block] = protocol.NewToolResult(value.ToolUseID, "[cleared: "+label+commaCount(weight)+" chars]", value.IsError)
		result[pos.message].Content = protocol.BlockContent(blocks...)
		cleared++
	}
	return result, cleared
}

type InMemoryCompactor struct{ TokenThreshold, MaxMessages int }

func (compactor InMemoryCompactor) Compact(ctx context.Context, value CompactionContext) (CompactionResult, error) {
	if err := ctx.Err(); err != nil {
		return CompactionResult{Messages: value.Messages}, err
	}
	messages, _ := SnipCompact(value.Messages, compactor.MaxMessages)
	messages, _ = MicroCompact(messages)
	return CompactionResult{Messages: messages}, nil
}
func (compactor InMemoryCompactor) MaybeCompact(ctx context.Context, value CompactionContext) (CompactionResult, error) {
	if value.Meter.UsedFor(value.Messages, value.Envelope) < compactor.TokenThreshold {
		return CompactionResult{Messages: value.Messages}, ctx.Err()
	}
	return compactor.Compact(ctx, value)
}

type DefaultCompactor struct {
	TokenThreshold int
	MaxMessages    int
	ResultBudget   int
	PreviewChars   int
	now            func() int64
}

func NewDefaultCompactor() *DefaultCompactor {
	return &DefaultCompactor{TokenThreshold: DefaultTokenThreshold, MaxMessages: 50, ResultBudget: 200_000, PreviewChars: 2_000}
}
func (compactor DefaultCompactor) timestamp() int64 {
	if compactor.now != nil {
		return compactor.now()
	}
	return time.Now().UnixMilli()
}
func (compactor DefaultCompactor) validate(value CompactionContext) error {
	if value.Files == nil || value.Provider == nil || value.Model == "" || compactor.TokenThreshold < 1 || compactor.MaxMessages < 1 || compactor.ResultBudget < 0 || compactor.PreviewChars < 0 {
		return errors.New("invalid compaction dependencies or budgets")
	}
	return protocol.ValidateTranscript(value.Messages)
}

func safeResultID(value string) string {
	if value == "" {
		value = "tool-result"
	}
	runes := []rune(value)
	for i, r := range runes {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-') {
			runes[i] = '_'
		}
	}
	return string(runes[:min(100, len(runes))])
}

func (compactor DefaultCompactor) budget(ctx context.Context, value CompactionContext) (CompactionResult, error) {
	result := CompactionResult{Messages: append([]protocol.Message(nil), value.Messages...)}
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
			total += len(output.Content)
			targets = append(targets, target{i, output, utf8.RuneCountInString(output.Content)})
		}
	}
	if total <= compactor.ResultBudget {
		return result, nil
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].weight > targets[j].weight })
	persisted := 0
	for _, target := range targets {
		if total <= compactor.ResultBudget {
			break
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		path := filepath.Join(".task_outputs", "tool-results", fmt.Sprintf("%s-%d.txt", safeResultID(target.original.ToolUseID), compactor.timestamp()))
		resolved, err := value.Files.Resolve(path)
		if err != nil {
			return result, err
		}
		if _, err := value.Files.Write(ctx, protocol.WriteFileInput{Path: path, Content: target.original.Content}); err != nil {
			return result, err
		}
		runes := []rune(target.original.Content)
		preview := string(runes[:min(len(runes), compactor.PreviewChars)])
		replacement := fmt.Sprintf("<persisted-output path=\"%s\" bytes=\"%d\">\n%s\n</persisted-output>", resolved, len(target.original.Content), preview)
		blocks[target.index] = protocol.NewToolResult(target.original.ToolUseID, replacement, target.original.IsError)
		total += len(replacement) - len(target.original.Content)
		result.Messages[index].Content = protocol.BlockContent(blocks...)
		persisted++
	}
	if persisted > 0 {
		result.Events = append(result.Events, CompactionEvent{kind: CompactBudget, count: persisted})
	}
	return result, nil
}

func (compactor DefaultCompactor) MaybeCompact(ctx context.Context, value CompactionContext) (CompactionResult, error) {
	result := CompactionResult{Messages: value.Messages}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := compactor.validate(value); err != nil {
		return result, err
	}
	result, err := compactor.budget(ctx, value)
	if err != nil {
		return result, err
	}
	var count int
	result.Messages, count = SnipCompact(result.Messages, compactor.MaxMessages)
	if count > 0 {
		result.Events = append(result.Events, CompactionEvent{kind: CompactSnip, count: count})
	}
	if value.Meter.UsedFor(result.Messages, value.Envelope) > compactor.TokenThreshold/2 {
		result.Messages, count = MicroCompact(result.Messages)
		if count > 0 {
			result.Events = append(result.Events, CompactionEvent{kind: CompactMicro, count: count})
		}
	}
	if value.Meter.UsedFor(result.Messages, value.Envelope) > compactor.TokenThreshold {
		value.Messages = result.Messages
		compressed, err := compactor.Compact(ctx, value)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return result, err
			}
			failure := fmt.Sprintf("%T: %s", err, err)
			runes := []rune(failure)
			result.Events = append(result.Events, CompactionEvent{kind: CompactFailed, failure: string(runes[:min(500, len(runes))])})
		} else {
			result.Messages = compressed.Messages
			result.Events = append(result.Events, compressed.Events...)
		}
	}
	return result, nil
}

func (compactor DefaultCompactor) Compact(ctx context.Context, value CompactionContext) (CompactionResult, error) {
	result := CompactionResult{Messages: value.Messages}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := compactor.validate(value); err != nil {
		return result, err
	}
	var archive strings.Builder
	for _, message := range value.Messages {
		line, err := protocol.PythonJSON(message, true, false)
		if err != nil {
			return result, err
		}
		archive.WriteString(line)
		archive.WriteByte('\n')
	}
	path := filepath.Join(".transcripts", fmt.Sprintf("transcript_%d.jsonl", compactor.timestamp()))
	resolved, err := value.Files.Resolve(path)
	if err != nil {
		return result, err
	}
	if _, err := value.Files.Write(ctx, protocol.WriteFileInput{Path: path, Content: archive.String()}); err != nil {
		return result, err
	}
	conversation, err := protocol.PythonJSON(value.Messages, true, false)
	if err != nil {
		return result, err
	}
	conversation = conversation[max(0, len(conversation)-80_000):]
	request := protocol.ModelRequest{Model: value.Model, MaxTokens: 2000, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(CompactionPrompt + "\n" + conversation)}}, Purpose: protocol.PurposeCompaction}
	reply, err := value.Provider.Complete(ctx, request)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := reply.Validate(); err != nil {
		return result, err
	}
	var summary strings.Builder
	for _, block := range reply.Content {
		if text, ok := block.Text(); ok {
			summary.WriteString(text.Text)
		}
	}
	if pytext.Strip(summary.String()) == "" {
		return result, errors.New("compaction summary came back empty; refusing to replace the transcript with nothing")
	}
	result.Messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(fmt.Sprintf("[Context compressed. Full transcript: %s]\n%s\n%s", resolved, SummaryPrefix, summary.String()))}}
	result.Events = []CompactionEvent{{kind: CompactAuto, summary: SummaryReceipt{Transcript: resolved, ReplacedMessages: len(value.Messages), ReplacedTokensEstimate: EstimateTokens(value.Messages), InputTokens: reply.Usage.InputTokens, OutputTokens: reply.Usage.OutputTokens, Model: reply.Model}}}
	return result, nil
}
