package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const DefaultMemorySelectionLimit = 5
const DefaultMemoryConsolidationThreshold = 10

// MemoryRequest is the agent's side-query seam. The caller owns transport,
// recovery, cache and event policy; the helpers never acquire a live transcript.
type MemoryRequest func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error)

// MemoryQueryContext binds an explicit owner store and a typed model callback.
// Selection and consolidation are available independently of automatic capture.
type MemoryQueryContext struct {
	Store  *memory.ScopedStore
	Model  string
	Create MemoryRequest
}

type MemoryLoadSender func(context.Context, MemoryEvent) error

// Enabled represents the caller's remember/recall and memory_auto selection.
// Send receives only a load event; it cannot select an owner or execute a tool.
type MemoryContext struct {
	Query   MemoryQueryContext
	Enabled bool
	Send    MemoryLoadSender
}

// SelectRelevantMemories preserves order, duplicates and Python [:maxItems]
// slicing, including zero and negative limits. Use DefaultMemorySelectionLimit
// for the Source default. Ordinary model failures use lexical fallback; initial
// listing, fallback, cancellation and native persistence authority errors escape.
func SelectRelevantMemories(ctx context.Context, value MemoryQueryContext, query string, maxItems int) ([]memory.Record, error) {
	return selectRelevantMemoriesInScope(ctx, value, query, maxItems, memorySelectionDirect)
}

func selectRelevantMemoriesInScope(ctx context.Context, value MemoryQueryContext, query string, maxItems int, scope memorySelectionScope) ([]memory.Record, error) {
	if value.Store == nil {
		return nil, errors.New("memory query requires a bound store")
	}
	records, err := value.Store.List(ctx)
	if err != nil || len(records) == 0 {
		return records, err
	}
	selected, found, err := queryRelevantMemories(ctx, value, query, maxItems, records, scope)
	if err != nil || found {
		return selected, err
	}
	return value.Store.Search(ctx, query, maxItems)
}

func queryRelevantMemories(ctx context.Context, value MemoryQueryContext, query string, maxItems int, records []memory.Record, scope memorySelectionScope) (selected []memory.Record, found bool, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			cause, _ := failure.(error)
			selected, found, err = nil, false, memoryExtractionFailure(ctx, cause)
		}
	}()
	lines := make([]string, 0, len(records))
	for index, record := range records {
		lines = append(lines, fmt.Sprintf("%d: %s — %s", index, record.Name, record.Description))
	}
	tail := []rune(query)
	if len(tail) > 4000 {
		tail = tail[len(tail)-4000:]
	}
	body := "Select relevant memory indices for the request. Return ONLY a JSON array of integers.\n\nRequest:\n" + string(tail) + "\n\nCatalog:\n" + strings.Join(lines, "\n")
	if value.Create == nil {
		return nil, false, memoryExtractionFailure(ctx, nil)
	}
	reply, callErr := value.Create(ctx, protocol.ModelRequest{Model: value.Model, MaxTokens: 200, Purpose: protocol.PurposeMemorySelection,
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(body)}}})
	if callErr != nil || ctx.Err() != nil {
		return nil, false, memoryExtractionFailure(ctx, callErr)
	}
	var text strings.Builder
	for _, block := range reply.Content {
		if part, ok := block.Text(); ok {
			text.WriteString(part.Text)
		}
	}
	for _, index := range memoryIndices(text.String(), len(records), 0, scope) {
		selected = append(selected, records[index])
	}
	found = len(selected) > 0
	return selected[:pythonSliceStop(len(selected), maxItems)], found, nil
}

func pythonSliceStop(size, stop int) int {
	if stop < 0 {
		stop = max(0, size+stop)
	}
	return min(size, stop)
}

// PrepareMemoryContext emits a load event after selection and before returning
// the prefixed text. It does not mutate a transcript or initiate capture.
func PrepareMemoryContext(ctx context.Context, value MemoryContext, text string) (string, error) {
	return prepareMemoryContextInScope(ctx, value, text, memorySelectionDirect)
}

func prepareMemoryContextInScope(ctx context.Context, value MemoryContext, text string, scope memorySelectionScope) (string, error) {
	if !value.Enabled {
		return text, nil
	}
	selected, err := selectRelevantMemoriesInScope(ctx, value.Query, text, DefaultMemorySelectionLimit, scope)
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return text, nil
	}
	blocks := make([]string, 0, len(selected))
	for _, record := range selected {
		blocks = append(blocks, memoryBlock(record))
	}
	if value.Send == nil {
		return "", errors.New("memory context requires a load sender")
	}
	if err := value.Send(ctx, MemoryEvent{Action: MemoryLoad, Count: len(selected)}); err != nil {
		return "", err
	}
	return "<memory_context>\n" + strings.Join(blocks, "\n\n") + "\n</memory_context>\n\n" + text, nil
}

// ConsolidateMemories uses the caller's bound store, irrespective of automatic
// memory enablement. Use DefaultMemoryConsolidationThreshold for the Source
// default. The initial List is outside best-effort containment. Replacement has
// no rollback and does not acquire the caller's process-local lifecycle lock.
func ConsolidateMemories(ctx context.Context, value MemoryQueryContext, threshold int) (int, error) {
	return consolidateMemoriesInScope(ctx, value, threshold, memorySideReply)
}

func consolidateMemoriesInScope(ctx context.Context, value MemoryQueryContext, threshold int, scope memoryReplyScope) (int, error) {
	if value.Store == nil {
		return 0, errors.New("memory consolidation requires a bound store")
	}
	records, err := value.Store.List(ctx)
	if err != nil || len(records) < threshold {
		return 0, err
	}
	return consolidateMemoryRecords(ctx, value, records, scope)
}

func (s *Session) memoryQueryContext() MemoryQueryContext {
	return MemoryQueryContext{Store: s.memory, Model: s.model, Create: s.completeSideModel}
}
