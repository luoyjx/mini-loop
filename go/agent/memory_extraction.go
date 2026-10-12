package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonsyntax"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

var memoryContextPrefix = regexp.MustCompile(`(?s)\A<memory_context>\n.*\n</memory_context>\n\n`)
var memoryRuntimeFacts = regexp.MustCompile(`(?s)\A<runtime-state>\n.*\n</runtime-state>\z`)

func stripMemoryContext(text string) string {
	if memoryRuntimeFacts.MatchString(text) {
		return ""
	}
	return memoryContextPrefix.ReplaceAllString(text, "")
}

// This projection is serialized inside one plain prompt, never submitted as a
// provider transcript: stripping tool results intentionally leaves tool uses.
func cleanMemoryMessages(messages []protocol.Message) []protocol.Message {
	cleaned := make([]protocol.Message, 0, len(messages))
	for _, message := range messages {
		if plain, ok := message.Content.Plain(); ok {
			text := stripMemoryContext(plain)
			if text != "" {
				cleaned = append(cleaned, protocol.Message{Role: message.Role, Content: protocol.PlainContent(text)})
			}
			continue
		}
		blocks, _ := message.Content.Blocks()
		parts := make([]protocol.Block, 0, len(blocks))
		for _, block := range blocks {
			if block.Kind() == protocol.BlockToolResult {
				continue
			}
			if text, ok := block.Text(); ok {
				value := stripMemoryContext(text.Text)
				if value != "" {
					parts = append(parts, protocol.NewTextBlock(value))
				}
			} else {
				parts = append(parts, block)
			}
		}
		if len(parts) > 0 {
			cleaned = append(cleaned, protocol.Message{Role: message.Role, Content: protocol.BlockContent(parts...)})
		}
	}
	return cleaned
}

// Only a named Input reaches storage. Field lowering is per item, after the
// complete reply has parsed, preserving source incremental extraction writes.
func extractedMemoryInput(fields jsonvalue.Value) (memory.Input, error) {
	if fields.Kind() != jsonvalue.Object {
		return memory.Input{}, errors.New("memory entry must be an object")
	}
	_, present := fields.Lookup("name")
	if !present {
		return memory.Input{}, errors.New("memory entry requires name")
	}
	values := [3]string{"", "project", ""}
	for index, key := range []string{"name", "type", "description"} {
		value, ok := fields.Lookup(key)
		if !ok {
			continue
		}
		if key == "name" || key == "description" {
			values[index] = value.PythonString()
			continue
		}
		text, ok := value.Text()
		if !ok {
			values[index] = "project" // No non-text JSON value equals a recognized type.
			continue
		}
		// Keep text types for the existing storage normalization.
		values[index] = text
	}
	input := (memory.Input{Name: values[0], Type: memory.Type(values[1]), Description: values[2], Origin: memory.AutoExtracted}).WithBody(memory.TextBody(""))
	if value, present := fields.Lookup("body"); present {
		body, err := observedMemoryBody(value)
		if err != nil {
			return memory.Input{}, err
		}
		input = input.WithBody(body)
	}
	return input, nil
}

func observedMemoryBody(value jsonvalue.Value) (memory.Body, error) {
	if text, ok := value.Text(); ok {
		return memory.TextBody(text), nil
	}
	size := 0
	switch value.Kind() {
	case jsonvalue.Array:
		items, _ := value.Array()
		size = len(items)
	case jsonvalue.Object:
		size = len(value.Keys())
	default:
		return memory.UnsizedBody(), nil
	}
	repr := ""
	if size <= memory.MaxBody {
		repr = value.PythonString()
	}
	return memory.ContainerBody(repr, size)
}

type memoryReplyScope uint8

const (
	memorySideReply memoryReplyScope = iota
	memoryStopReply
)

// Pinned CPython default-recursion profiles, measured through the actual Source
// consumers. Counts include the reply array and entry object: 977+2 for direct
// side operations, 972+2 for Agent.run capture. Other host/caller profiles remain
// separate acceptance work; archive decoding keeps its independent 1000 bound.
func (scope memoryReplyScope) limits() jsonsyntax.Limits {
	containers := 979
	if scope == memoryStopReply {
		containers = 974
	}
	return jsonsyntax.Limits{Containers: containers, IntegerDigits: 4300}
}

func memoryReplyArray(reply protocol.ModelReply, scope memoryReplyScope) []jsonvalue.Value {
	var text strings.Builder
	for _, block := range reply.Content {
		if value, ok := block.Text(); ok {
			text.WriteString(value.Text)
		}
	}
	value := text.String()
	first, last := strings.Index(value, "["), strings.LastIndex(value, "]")
	if first < 0 || last < first {
		return nil
	}
	raw := value[first : last+1]
	if jsonsyntax.Scan([]byte(raw), scope.limits()) != nil {
		return nil
	}
	decoded, err := jsonvalue.Decode(raw)
	if err != nil {
		return nil
	}
	items, _ := decoded.Array()
	return items
}

func memoryExtractionFailure(ctx context.Context, err error) error {
	if stateErr := stateRunError(ctx); stateErr != nil {
		return stateErr
	}
	if errors.Is(err, ErrStateTranscript) || errors.Is(err, ErrSessionLeaseLost) {
		return err
	}
	return nil
}

// Called directly by defer at Source's ordinary-exception boundaries. A panic
// payload is inspected only for native authority errors, never retained or sent
// as model text. Cancellation and persistence authority loss still propagate.
func recoverMemoryFailure(ctx context.Context, count *int, err *error) {
	if failure := recover(); failure != nil {
		*count = 0
		cause, _ := failure.(error)
		*err = memoryExtractionFailure(ctx, cause)
	}
}

// Normalize only at the storage handoff. Consolidation compares original fields
// first: an unknown type must not inherit a project record's previous origin.
func storedMemoryType(kind memory.Type) memory.Type {
	switch kind {
	case memory.User, memory.Feedback, memory.Project, memory.Reference:
		return kind
	default:
		return memory.Project
	}
}

// extractMemories is the source extraction stage. Its lifecycle caller owns
// WithLifecycle; automatic endpoint capture/consolidation is wired separately.
// Ordinary faults return zero, even if preceding items already reached disk.
func (s *Session) extractMemories(ctx context.Context) (count int, err error) {
	return s.extractMemoriesInScope(ctx, memorySideReply)
}

func (s *Session) extractMemoriesInScope(ctx context.Context, scope memoryReplyScope) (count int, err error) {
	defer recoverMemoryFailure(ctx, &count, &err)
	if err := stateRunError(ctx); err != nil {
		return 0, err
	}
	if s.memory == nil {
		return 0, nil
	}
	conversation, err := protocol.PythonJSON(cleanMemoryMessages(s.messages), true, false)
	if err != nil {
		return 0, memoryExtractionFailure(ctx, err)
	}
	if len(conversation) > 40000 {
		conversation = conversation[len(conversation)-40000:]
	}
	records, err := s.memory.List(ctx)
	if err != nil {
		return 0, memoryExtractionFailure(ctx, err)
	}
	lines := make([]string, 0, len(records))
	for _, record := range records {
		lines = append(lines, "- "+record.Name+": "+record.Description)
	}
	prompt := "From this conversation, extract durable facts worth remembering across sessions (types: user, feedback, project, reference). Return ONLY a JSON array of " +
		`{"name","type","description","body"}. Empty array if nothing durable or already covered.` +
		"\n\nExisting memories:\n" + strings.Join(lines, "\n") + "\n\nConversation:\n" + conversation
	reply, err := s.completeSideModel(ctx, protocol.ModelRequest{Model: s.model, MaxTokens: 1500, Purpose: protocol.PurposeAgentTurn,
		Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(prompt)}}})
	if err != nil {
		return 0, memoryExtractionFailure(ctx, err)
	}
	items := memoryReplyArray(reply, scope)
	if len(items) > 5 {
		items = items[:5]
	}
	for _, raw := range items {
		input, err := extractedMemoryInput(raw)
		if err != nil {
			return 0, nil
		}
		input.Type = storedMemoryType(input.Type)
		if _, err = s.memory.Write(ctx, input); err != nil {
			return 0, memoryExtractionFailure(ctx, err)
		}
	}
	if err := stateRunError(ctx); err != nil {
		return 0, err
	}
	return len(items), nil
}
