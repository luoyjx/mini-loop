package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
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

// Python hashes the original tuple before any conversion or replacement. Stored
// records have string fields, so other hashable JSON scalars cannot match them.
func memoryReplyIdentity(fields jsonvalue.Value) (memoryIdentity, bool, error) {
	values := [4]string{"", "project", "", ""}
	matchable := true
	for index, key := range []string{"name", "type", "description", "body"} {
		value, present := fields.Lookup(key)
		if !present {
			continue
		}
		if value.Kind() == jsonvalue.Array || value.Kind() == jsonvalue.Object {
			return memoryIdentity{}, false, errors.New("memory identity contains an unhashable value")
		}
		text, ok := value.Text()
		if !ok {
			matchable = false
			continue
		}
		values[index] = text
	}
	return memoryIdentity{values[0], memory.Type(values[1]), values[2], values[3]}, matchable, nil
}

func (s *Session) consolidateMemories(ctx context.Context) (int, error) {
	return s.consolidateMemoriesInScope(ctx, memorySideReply)
}

func (s *Session) consolidateMemoriesInScope(ctx context.Context, scope memoryReplyScope) (int, error) {
	// Source List is outside its best-effort block, so failure reaches capture.
	records, err := s.memory.List(ctx)
	if err != nil {
		return 0, err
	}
	if len(records) < 10 {
		return 0, nil
	}
	return s.consolidateMemoryRecords(ctx, records, scope)
}

// Source's List occurs before its exception handler. Keep that read in the
// caller so listing faults still reach the outer capture diagnostic boundary.
func (s *Session) consolidateMemoryRecords(ctx context.Context, records []memory.Record, scope memoryReplyScope) (count int, err error) {
	defer recoverMemoryFailure(ctx, &count, &err)
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
	for _, raw := range memoryReplyArray(reply, scope) {
		if raw.Kind() != jsonvalue.Object {
			continue
		}
		if _, ok := raw.Lookup("name"); !ok {
			continue
		}
		identity, matchable, err := memoryReplyIdentity(raw)
		if err != nil {
			return 0, nil
		}
		input, err := extractedMemoryInput(raw)
		if err != nil {
			return 0, nil
		}
		input.Origin = memory.Consolidated
		if origin, ok := origins[identity]; matchable && ok {
			input.Origin = origin
		}
		input.Type = storedMemoryType(input.Type)
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
	scope := memoryStopReply
	if s.currentRun.memoryReplyScope == memoryHTTPPostReply {
		scope = memoryHTTPPostReply
	}
	err := s.memory.WithLifecycle(ctx, func() error {
		var err error
		count, err = s.extractMemoriesInScope(ctx, scope)
		if err != nil {
			return err
		}
		consolidated, err = s.consolidateMemoriesInScope(ctx, scope)
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
