package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const EventMemoryCaptureError SessionEventKind = "memory_capture_error"

type MemoryCaptureErrorEvent struct {
	Detail string `json:"detail"`
}

func (event SessionEvent) MemoryCaptureError() (MemoryCaptureErrorEvent, bool) {
	return event.memoryCaptureError, event.kind == EventMemoryCaptureError
}

type memoryIdentity struct {
	Name        string
	Type        memory.Type
	Description string
	Body        string
}

func memoryInputIdentity(input memory.Input) memoryIdentity {
	return memoryIdentity{input.Name, input.Type, input.Description, input.Body}
}

func (s *Session) consolidateMemories(ctx context.Context) (int, error) {
	// Source List is outside its best-effort block, so failure reaches capture.
	records, err := s.memory.List(ctx)
	if err != nil {
		return 0, err
	}
	if len(records) < 10 {
		return 0, nil
	}
	body, err := protocol.PythonJSON(records, false, false)
	if err != nil {
		return 0, memoryExtractionFailure(ctx, err)
	}
	prompt := "Deduplicate and consolidate these memories. Preserve current facts and remove obsolete or contradictory duplicates. Return ONLY JSON array entries with name,type,description,body.\n\n" + body
	reply, err := s.completeSideModel(ctx, protocol.ModelRequest{Model: s.model, MaxTokens: 2500, Purpose: protocol.PurposeMemoryConsolidation,
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(prompt)}}})
	if err != nil {
		return 0, memoryExtractionFailure(ctx, err)
	}
	origins := make(map[memoryIdentity]memory.Origin, len(records))
	for _, record := range records {
		origins[memoryIdentity{record.Name, record.Type, record.Description, record.Body}] = record.Origin
	}
	inputs := make([]memory.Input, 0)
	for _, raw := range memoryReplyArray(reply) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			continue
		}
		if _, ok := fields["name"]; !ok {
			continue
		}
		input, err := extractedMemoryInput(raw)
		if err != nil {
			return 0, nil
		}
		input.Origin = memory.Consolidated
		if origin, ok := origins[memoryInputIdentity(input)]; ok {
			input.Origin = origin
		}
		inputs = append(inputs, input)
	}
	if len(inputs) == 0 {
		return 0, nil
	}
	if err := s.memory.ReplaceAll(ctx, inputs, memory.Consolidated); err != nil {
		return 0, memoryExtractionFailure(ctx, err)
	}
	if err := stateRunError(ctx); err != nil {
		return 0, err
	}
	return len(inputs), nil
}

// captureMemories contains ordinary end-of-turn faults. Context cancellation
// and native persistence authority loss remain errors, never a healthy capture.
func (s *Session) captureMemories(ctx context.Context) error {
	if err := stateRunError(ctx); err != nil {
		return err
	}
	if !s.automaticMemoryEnabled() || s.permissionMode() == ModeReadonly {
		return nil
	}
	var count, consolidated int
	err := s.memory.WithLifecycle(ctx, func() error {
		var err error
		count, err = s.extractMemories(ctx)
		if err != nil {
			return err
		}
		consolidated, err = s.consolidateMemories(ctx)
		return err
	})
	if failure := memoryExtractionFailure(ctx, err); failure != nil {
		return failure
	}
	if err != nil {
		detail := truncateRunes(fmt.Sprintf("%T: %v", err, err), 200)
		s.events.append(SessionEvent{kind: EventMemoryCaptureError, memoryCaptureError: MemoryCaptureErrorEvent{Detail: detail}})
		return nil
	}
	if count > 0 || consolidated > 0 {
		s.events.append(SessionEvent{kind: EventMemory, memory: MemoryEvent{Action: MemoryExtract, Count: count, Consolidated: &consolidated}})
	}
	return nil
}
