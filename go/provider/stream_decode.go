package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// These accumulators belong to the bounded wire adapter. Partial JSON never
// reaches the protocol domain: each stopped block is decoded and validated.
type streamBlock struct {
	kind          protocol.BlockKind
	text          *strings.Builder
	citations     protocol.Citations
	citationBytes int
	signature, id string
	redacted      string
	fileID        string
	name          protocol.ToolName
	caller        *protocol.ToolCaller
	input         []byte
	parsedInput   jsonvalue.Value
	partialInput  bool
	stopped       bool
	block         protocol.Block
}
type streamAssembly struct {
	reply                   protocol.ModelReply
	blocks                  []streamBlock
	started, delta, stopped bool
}

func badStream(message string) *Failure {
	return &Failure{Kind: FailureProtocol, Message: "invalid provider stream: " + message}
}

// SSE supports CR, LF and CRLF, comments and multiline data. The limiter bounds
// the complete wire, including ignored ping/extension frames, before scanning.
func streamLine(data []byte, eof bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' {
			return i + 1, data[:i], nil
		}
		if b == '\r' {
			if i+1 == len(data) && !eof {
				return 0, nil, nil
			}
			n := i + 1
			if n < len(data) && data[n] == '\n' {
				n++
			}
			return n, data[:i], nil
		}
	}
	if eof && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

type countedStream struct {
	reader io.Reader
	read   int64
}

func (r *countedStream) Read(data []byte) (int, error) {
	n, e := r.reader.Read(data)
	r.read += int64(n)
	return n, e
}
func (c *Client) readStream(ctx context.Context, body io.Reader, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	reader := &countedStream{reader: io.LimitReader(body, c.responseLimit+1)}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), protocol.MaxWireBytes+1)
	scanner.Split(streamLine)
	state := streamAssembly{}
	var event string
	var data bytes.Buffer
	dispatch := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		if !utf8.Valid(data.Bytes()) {
			return badStream("invalid UTF-8 event")
		}
		err := c.consumeStream(&state, event, bytes.TrimSuffix(data.Bytes(), []byte{'\n'}), emit)
		event = ""
		data.Reset()
		return err
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return protocol.ModelReply{}, streamReadFailure(ctx, ctx.Err())
		}
		if reader.read > c.responseLimit {
			return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Message: "provider stream exceeds wire limit"}
		}
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return protocol.ModelReply{}, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		name, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch name {
		case "event":
			event = value
		case "data":
			if data.Len()+len(value)+1 > protocol.MaxWireBytes {
				return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Message: "provider stream event exceeds protocol limit"}
			}
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if reader.read > c.responseLimit {
		return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Message: "provider stream exceeds wire limit"}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return protocol.ModelReply{}, &Failure{Kind: FailureLimit, Message: "provider stream line exceeds protocol limit"}
		}
		return protocol.ModelReply{}, streamReadFailure(ctx, err)
	}
	// Match SDK SSE framing: an unterminated final event is not dispatched.
	if !state.started || !state.delta || !state.stopped {
		return protocol.ModelReply{}, &Failure{Kind: FailureConnection, Message: "Provider stream ended before a completed message."}
	}
	if err := state.reply.Validate(); err != nil {
		return protocol.ModelReply{}, badStream(c.clean(err.Error(), 500))
	}
	return state.reply.Clone(), nil
}
func (c *Client) consumeStream(s *streamAssembly, event string, data []byte, emit func(protocol.StreamDelta) error) error {
	if event == "ping" {
		return nil
	}
	if event == "error" {
		failure := &Failure{Kind: FailureStatus, Status: 200, Message: c.statusMessage(200, data)}
		var wire struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &wire) == nil {
			switch wire.Error.Type {
			case "overloaded_error":
				failure.recoveryKind = protocol.ModelFailureOverloaded
			case "rate_limit_error":
				failure.recoveryKind = protocol.ModelFailureRateLimit
			}
		}
		return failure
	}
	switch event {
	case "message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop":
	default:
		return nil // SDK ignores unknown SSE event names.
	}
	var wire struct {
		Type    string          `json:"type"`
		Index   *int            `json:"index"`
		Message json.RawMessage `json:"message"`
		Block   json.RawMessage `json:"content_block"`
		Delta   struct {
			Type         string                       `json:"type"`
			Text         *string                      `json:"text"`
			Thinking     *string                      `json:"thinking"`
			Signature    *string                      `json:"signature"`
			Citation     json.RawMessage              `json:"citation"`
			PartialJSON  *string                      `json:"partial_json"`
			StopReason   *protocol.StopReason         `json:"stop_reason"`
			StopSequence *string                      `json:"stop_sequence"`
			StopDetails  protocol.StopDetailsMetadata `json:"stop_details"`
		} `json:"delta"`
		Usage struct {
			Input    *protocol.UsageCount      `json:"input_tokens"`
			Output   *protocol.UsageCount      `json:"output_tokens"`
			Read     *protocol.UsageCount      `json:"cache_read_input_tokens"`
			Creation *protocol.UsageCount      `json:"cache_creation_input_tokens"`
			Server   *protocol.ServerToolUsage `json:"server_tool_use"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return badStream("malformed event JSON")
	}
	if wire.Type != "" && wire.Type != event {
		return badStream("event discriminator mismatch")
	}
	if s.stopped {
		return badStream("event after message_stop")
	}
	if event == "message_start" {
		if s.started {
			return badStream("duplicate message_start")
		}
		var start struct {
			ID          *string                      `json:"id"`
			Type        *protocol.ReplyKind          `json:"type"`
			Role        *protocol.Role               `json:"role"`
			Model       *string                      `json:"model"`
			Content     *[]json.RawMessage           `json:"content"`
			Usage       *protocol.TokenUsage         `json:"usage"`
			Container   protocol.ContainerMetadata   `json:"container"`
			StopDetails protocol.StopDetailsMetadata `json:"stop_details"`
		}
		if json.Unmarshal(wire.Message, &start) != nil || start.ID == nil || start.Type == nil || start.Role == nil || start.Model == nil || start.Content == nil || start.Usage == nil {
			return badStream("missing message_start fields")
		}
		s.reply = protocol.ModelReply{ID: *start.ID, Type: *start.Type, Role: *start.Role, Model: *start.Model, Content: []protocol.Block{}, Usage: start.Usage.WithSDKStreamCounts()}
		s.reply.Container = start.Container.WithSDKDatetime()
		s.reply.StopDetails = start.StopDetails
		if err := s.reply.Usage.Validate(); err != nil {
			return badStream("invalid initial usage")
		}
		if s.reply.ID == "" || s.reply.Type != protocol.ReplyMessage || s.reply.Role != protocol.RoleAssistant || s.reply.Model == "" {
			return badStream("invalid initial message identity")
		}
		for _, raw := range *start.Content {
			b, err := decodeBlock(raw)
			if err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			s.blocks = append(s.blocks, streamBlock{stopped: true, block: b})
		}
		s.started = true
		return nil
	}
	if !s.started {
		return badStream("event before message_start")
	}
	if event == "message_delta" {
		if wire.Delta.StopReason == nil || wire.Usage.Output == nil {
			return badStream("missing final delta fields")
		}
		for _, b := range s.blocks {
			if !b.stopped {
				return badStream("message_delta before block stop")
			}
		}
		s.reply.StopReason = *wire.Delta.StopReason
		s.reply.StopSequence = wire.Delta.StopSequence
		// The SDK retains start.container and only replaces non-null stop_details.
		if wire.Delta.StopDetails.Kind() != protocol.ReplyMetadataNull {
			s.reply.StopDetails = wire.Delta.StopDetails
		}
		deltaUsage := protocol.TokenUsage{OutputTokens: *wire.Usage.Output, CacheReadInputTokens: wire.Usage.Read, CacheCreationInputTokens: wire.Usage.Creation, ServerToolUse: wire.Usage.Server}
		if wire.Usage.Input != nil {
			deltaUsage.InputTokens = *wire.Usage.Input
		}
		deltaUsage = deltaUsage.WithSDKStreamCounts()
		s.reply.Usage.OutputTokens = deltaUsage.OutputTokens
		if wire.Usage.Input != nil {
			s.reply.Usage.InputTokens = deltaUsage.InputTokens
		}
		if wire.Usage.Read != nil {
			s.reply.Usage.CacheReadInputTokens = deltaUsage.CacheReadInputTokens
		}
		if wire.Usage.Creation != nil {
			s.reply.Usage.CacheCreationInputTokens = deltaUsage.CacheCreationInputTokens
		}
		if wire.Usage.Server != nil {
			s.reply.Usage.ServerToolUse = deltaUsage.ServerToolUse
		}
		s.reply.Usage = s.reply.Usage.WithSDKDefaults()
		if err := s.reply.Usage.Validate(); err != nil {
			return badStream("invalid delta usage")
		}
		s.delta = true
		return nil
	}
	if event == "message_stop" {
		if !s.delta {
			return badStream("message_stop before final delta")
		}
		for _, b := range s.blocks {
			if !b.stopped {
				return badStream("unfinished content block")
			}
			s.reply.Content = append(s.reply.Content, b.block)
		}
		s.stopped = true
		return nil
	}
	if s.delta {
		return badStream("content after final delta")
	}
	if wire.Index == nil || *wire.Index < 0 {
		return badStream("invalid content index")
	}
	i := *wire.Index
	if event == "content_block_start" {
		if i != len(s.blocks) {
			return badStream("nonsequential content index")
		}
		var block struct {
			Type      protocol.BlockKind `json:"type"`
			Text      *string            `json:"text"`
			Thinking  *string            `json:"thinking"`
			Signature *string            `json:"signature"`
			FileID    *string            `json:"file_id"`
			Data      *string            `json:"data"`
			ID        *string            `json:"id"`
			Name      *protocol.ToolName `json:"name"`
			Input     json.RawMessage    `json:"input"`
			Caller    json.RawMessage    `json:"caller"`
			Citations json.RawMessage    `json:"citations"`
		}
		if json.Unmarshal(wire.Block, &block) != nil {
			return badStream("malformed starting block")
		}
		b := streamBlock{kind: block.Type, text: &strings.Builder{}}
		switch block.Type {
		case protocol.BlockTextEditorToolResult:
			var result protocol.TextEditorToolResultBlock
			if err := json.Unmarshal(wire.Block, &result); err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			result = result.WithSDKDefaults(true)
			encoded, err := json.Marshal(result)
			if err != nil || len(encoded) > protocol.MaxWireBytes {
				return badStream("normalized text editor block exceeds protocol limit")
			}
			b.block = result.Block()
		case protocol.BlockBashExecutionToolResult:
			var result protocol.BashExecutionToolResultBlock
			if err := json.Unmarshal(wire.Block, &result); err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			result = result.WithSDKDefaults(true)
			encoded, err := json.Marshal(result)
			if err != nil || len(encoded) > protocol.MaxWireBytes {
				return badStream("normalized bash execution block exceeds protocol limit")
			}
			b.block = result.Block()
		case protocol.BlockCodeExecutionToolResult:
			var result protocol.CodeExecutionToolResultBlock
			if err := json.Unmarshal(wire.Block, &result); err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			result = result.WithSDKDefaults(true)
			encoded, err := json.Marshal(result)
			if err != nil || len(encoded) > protocol.MaxWireBytes {
				return badStream("normalized code execution block exceeds protocol limit")
			}
			b.block = result.Block()
		case protocol.BlockWebFetchToolResult:
			var result protocol.WebFetchToolResultBlock
			if err := json.Unmarshal(wire.Block, &result); err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			result = result.WithSDKDefaults(true)
			encoded, err := json.Marshal(result)
			if err != nil || len(encoded) > protocol.MaxWireBytes {
				return badStream("normalized web fetch block exceeds protocol limit")
			}
			b.block = result.Block()
		case protocol.BlockServerToolUse:
			initial, err := decodeBlock(wire.Block)
			if err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			b.block = initial
			b.input = append([]byte(nil), block.Input...)
		case protocol.BlockToolSearchToolResult:
			var result protocol.ToolSearchToolResultBlock
			if err := json.Unmarshal(wire.Block, &result); err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			result = result.WithSDKDefaults(true)
			encoded, err := json.Marshal(result)
			if err != nil || len(encoded) > protocol.MaxWireBytes {
				return badStream("normalized tool search block exceeds protocol limit")
			}
			b.block = result.Block()
		case protocol.BlockWebSearchToolResult:
			initial, err := decodeBlock(wire.Block)
			if err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			b.block = initial
		case protocol.BlockContainerUpload:
			if block.FileID == nil {
				return badStream("container upload file_id missing")
			}
			b.fileID = *block.FileID
		case protocol.BlockRedactedThinking:
			if block.Data == nil {
				return badStream("redacted thinking data missing")
			}
			b.redacted = *block.Data
		case protocol.BlockText:
			if block.Text == nil {
				return badStream("text missing")
			}
			initial, err := decodeBlock(wire.Block)
			if err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			text, _ := initial.Text()
			b.citations = text.Citations()
			encoded, err := json.Marshal(b.citations)
			if err != nil {
				return badStream("invalid citation metadata")
			}
			b.citationBytes = len(encoded)
			b.text.WriteString(*block.Text)
		case protocol.BlockThinking:
			if block.Thinking == nil || block.Signature == nil {
				return badStream("thinking fields missing")
			}
			b.text.WriteString(*block.Thinking)
			b.signature = *block.Signature
		case protocol.BlockToolUse:
			var caller *protocol.ToolCaller
			if len(block.Caller) > 0 && string(block.Caller) != "null" {
				caller = &protocol.ToolCaller{}
				if json.Unmarshal(block.Caller, caller) != nil {
					return badStream("malformed starting block")
				}
			}
			if block.ID == nil || block.Name == nil || len(block.Input) == 0 {
				return badStream("tool fields missing")
			}
			b.id = *block.ID
			b.name = *block.Name
			b.input = append([]byte(nil), block.Input...)
			b.caller = caller
		default:
			return badStream("unsupported content block")
		}
		s.blocks = append(s.blocks, b)
		return nil
	}
	if i >= len(s.blocks) || s.blocks[i].stopped {
		return badStream("content index is not open")
	}
	b := &s.blocks[i]
	if event == "content_block_stop" {
		if b.partialInput {
			encoded, err := b.parsedInput.MarshalLegacyUTF8()
			if err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			b.input = encoded
		}
		switch b.kind {
		case protocol.BlockServerToolUse:
			input, err := protocol.DecodeSDKServerToolInput(b.input)
			if err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			value, _ := b.block.ServerToolUse()
			value = value.WithInput(input)
			encoded, err := value.Block().MarshalObservationJSON()
			if err != nil || len(encoded) > protocol.MaxWireBytes {
				return badStream("normalized server tool use exceeds protocol limit")
			}
			b.block = value.Block()
		case protocol.BlockContainerUpload:
			b.block = protocol.NewStreamedContainerUploadBlock(b.fileID)
		case protocol.BlockRedactedThinking:
			b.block = protocol.NewRedactedThinkingBlock(b.redacted)
		case protocol.BlockText:
			b.block = protocol.NewStreamedTextBlock(b.text.String(), b.citations)
		case protocol.BlockThinking:
			b.block = protocol.NewThinkingBlock(b.text.String(), b.signature)
		case protocol.BlockToolUse:
			input, err := protocol.DecodeModelToolInput(b.name, b.input)
			if err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			b.block = protocol.NewToolUseWithCaller(b.id, input, b.caller)
		}
		if err := b.block.Validate(); err != nil {
			return badStream(c.clean(err.Error(), 500))
		}
		b.stopped = true
		b.input = nil
		b.parsedInput = jsonvalue.Value{}
		return nil
	}
	var progress protocol.StreamDelta
	switch wire.Delta.Type {
	case "text_delta":
		if b.kind != protocol.BlockText || wire.Delta.Text == nil {
			return badStream("invalid text delta")
		}
		b.text.WriteString(*wire.Delta.Text)
		progress = protocol.StreamDelta{Kind: protocol.DeltaText, Text: *wire.Delta.Text}
	case "citations_delta":
		if b.kind != protocol.BlockText || len(wire.Delta.Citation) == 0 {
			return badStream("invalid citation delta")
		}
		citation, err := providerCitation(wire.Delta.Citation)
		if err != nil {
			return badStream("malformed citation delta")
		}
		citations, err := b.citations.Append(citation)
		if err != nil {
			return badStream("citation delta requires null or array metadata")
		}
		encoded, err := json.Marshal(citation)
		if err != nil {
			return badStream("invalid normalized citation delta")
		}
		b.citations = citations
		b.citationBytes += len(encoded) + 1
	case "thinking_delta":
		if b.kind != protocol.BlockThinking || wire.Delta.Thinking == nil {
			return badStream("invalid thinking delta")
		}
		b.text.WriteString(*wire.Delta.Thinking)
		progress = protocol.StreamDelta{Kind: protocol.DeltaThinking, Text: *wire.Delta.Thinking}
	case "signature_delta":
		if b.kind != protocol.BlockThinking || wire.Delta.Signature == nil {
			return badStream("invalid signature delta")
		}
		b.signature = *wire.Delta.Signature
	case "input_json_delta":
		if (b.kind != protocol.BlockToolUse && b.kind != protocol.BlockServerToolUse) || wire.Delta.PartialJSON == nil {
			return badStream("invalid tool input delta")
		}
		if *wire.Delta.PartialJSON == "" {
			return nil
		}
		if !b.partialInput {
			b.input = nil
			b.partialInput = true
		}
		if len(b.input)+len(*wire.Delta.PartialJSON) > protocol.MaxWireBytes {
			return &Failure{Kind: FailureLimit, Message: "provider streamed block exceeds protocol limit"}
		}
		b.input = append(b.input, []byte(*wire.Delta.PartialJSON)...)
		parsed, err := jsonvalue.DecodePartial(string(b.input))
		if err != nil {
			return badStream("malformed partial tool input")
		}
		b.parsedInput = parsed
	default:
		return badStream("unsupported content delta")
	}
	if b.text.Len()+len(b.signature)+len(b.input)+b.citationBytes > protocol.MaxWireBytes {
		return &Failure{Kind: FailureLimit, Message: "provider streamed block exceeds protocol limit"}
	}
	if progress.Text != "" && emit != nil {
		return emit(progress)
	}
	return nil
}
