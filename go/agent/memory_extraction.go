package agent

import (
	"context"
	"errors"
	"regexp"
	"strings"

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
	values := [4]string{"", "project", "", ""}
	for index, key := range []string{"name", "type", "description", "body"} {
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
			if key == "type" { // No non-text JSON value equals a recognized type.
				values[index] = "project"
				continue
			}
			return memory.Input{}, errors.New("memory body must be a string")
		}
		// Preserve text for the storage boundary's UTF-8 check. In consolidation
		// that failure must occur after owner-scoped deletion and earlier writes.
		values[index] = text
	}
	return memory.Input{Name: values[0], Type: memory.Type(values[1]), Description: values[2], Body: values[3], Origin: memory.AutoExtracted}, nil
}

func memoryReplyArray(reply protocol.ModelReply) []jsonvalue.Value {
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
	decoded, err := jsonvalue.Decode(value[first : last+1])
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
func (s *Session) extractMemories(ctx context.Context) (int, error) {
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
	items := memoryReplyArray(reply)
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
