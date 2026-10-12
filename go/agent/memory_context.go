package agent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonsyntax"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const EventMemory SessionEventKind = "memory"

type MemoryAction string

const MemoryLoad MemoryAction = "load"
const MemoryExtract MemoryAction = "extract"

type MemoryEvent struct {
	Action       MemoryAction `json:"action"`
	Count        int          `json:"count"`
	Consolidated *int         `json:"consolidated,omitempty"`
}

func (v MemoryEvent) Validate() error {
	if v.Count < 0 || (v.Action != MemoryLoad && v.Action != MemoryExtract) ||
		(v.Action == MemoryLoad && v.Consolidated != nil) ||
		(v.Action == MemoryExtract && (v.Consolidated == nil || *v.Consolidated < 0)) {
		return errors.New("invalid memory event")
	}
	return nil
}
func (event SessionEvent) Memory() (MemoryEvent, bool) {
	v := event.memory
	v.Consolidated = clonePointer(v.Consolidated)
	return v, event.kind == EventMemory
}

func (s *Session) automaticMemoryEnabled() bool {
	_, remember := s.gate.catalog.Lookup(protocol.ToolRemember)
	_, recall := s.gate.catalog.Lookup(protocol.ToolRecall)
	return s.memoryAuto && s.memory != nil && remember && recall
}

// Closed JSON observations are discarded after selecting integer indices.
// Python treats booleans as ints and ignores all floats, including nonfinite values.
// Decode the whole array before selection so ignored values can still reject it.
func memoryIndices(text string, size, limit int, scope memorySelectionScope) []int {
	start, end := strings.IndexByte(text, '['), strings.LastIndexByte(text, ']')
	if start < 0 || end < start {
		return nil
	}
	raw := text[start : end+1]
	if jsonsyntax.Scan([]byte(raw), scope.limits()) != nil {
		return nil
	}
	decoded, err := jsonvalue.Decode(raw)
	if err != nil {
		return nil
	}
	items, ok := decoded.Array()
	if !ok {
		return nil
	}
	var result []int
	for _, item := range items {
		value, integer := item.Integer()
		if flag, boolean := item.Bool(); boolean {
			value, integer = "0", true
			if flag {
				value = "1"
			}
		}
		if !integer {
			continue
		}
		index, err := strconv.Atoi(value)
		if err == nil && index >= 0 && index < size {
			result = append(result, index)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func (s *Session) prepareMemoryContext(ctx context.Context, prompt string) (string, error) {
	return s.prepareMemoryContextInScope(ctx, prompt, memorySelectionDirect)
}

func (s *Session) prepareMemoryContextInScope(ctx context.Context, prompt string, scope memorySelectionScope) (string, error) {
	if !s.automaticMemoryEnabled() {
		return prompt, nil
	}
	records, err := s.memory.List(ctx)
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return prompt, nil
	}
	lines := make([]string, 0, len(records))
	for index, record := range records {
		lines = append(lines, fmt.Sprintf("%d: %s — %s", index, record.Name, record.Description))
	}
	query := []rune(prompt)
	if len(query) > 4000 {
		query = query[len(query)-4000:]
	}
	body := "Select relevant memory indices for the request. Return ONLY a JSON array of integers.\n\nRequest:\n" + string(query) + "\n\nCatalog:\n" + strings.Join(lines, "\n")
	request := protocol.ModelRequest{Model: s.model, MaxTokens: 200, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(body)}}, Purpose: protocol.PurposeMemorySelection}
	reply, callErr := s.completeModel(ctx, request, nil)
	if ctx.Err() != nil {
		return "", stateRunError(ctx)
	}
	if errors.Is(callErr, ErrStateTranscript) || errors.Is(callErr, ErrSessionLeaseLost) {
		return "", callErr
	}
	var selected []memory.Record
	if callErr == nil {
		var text strings.Builder
		for _, block := range reply.Content {
			if value, ok := block.Text(); ok {
				text.WriteString(value.Text)
			}
		}
		for _, index := range memoryIndices(text.String(), len(records), 5, scope) {
			selected = append(selected, records[index])
		}
	}
	if len(selected) == 0 {
		selected, err = s.memory.Search(ctx, prompt, 5)
		if err != nil {
			return "", err
		}
	}
	if len(selected) == 0 {
		return prompt, nil
	}
	blocks := make([]string, 0, len(selected))
	for _, record := range selected {
		blocks = append(blocks, memoryBlock(record))
	}
	s.events.append(SessionEvent{kind: EventMemory, memory: MemoryEvent{Action: MemoryLoad, Count: len(selected)}})
	return "<memory_context>\n" + strings.Join(blocks, "\n\n") + "\n</memory_context>\n\n" + prompt, nil
}
