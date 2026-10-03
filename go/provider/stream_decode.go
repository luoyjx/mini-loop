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

	"github.com/luoyjx/mini-loop/go/protocol"
)

// These accumulators belong to the bounded wire adapter. Partial JSON never
// reaches the protocol domain: each stopped block is decoded and validated.
type streamBlock struct {
	kind          protocol.BlockKind
	text          *strings.Builder
	signature, id string
	name          protocol.ToolName
	caller        *protocol.ToolCaller
	input         []byte
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
			Type         string               `json:"type"`
			Text         *string              `json:"text"`
			Thinking     *string              `json:"thinking"`
			Signature    *string              `json:"signature"`
			PartialJSON  *string              `json:"partial_json"`
			StopReason   *protocol.StopReason `json:"stop_reason"`
			StopSequence *string              `json:"stop_sequence"`
		} `json:"delta"`
		Usage struct {
			Input    *int `json:"input_tokens"`
			Output   *int `json:"output_tokens"`
			Read     *int `json:"cache_read_input_tokens"`
			Creation *int `json:"cache_creation_input_tokens"`
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
			ID      *string             `json:"id"`
			Type    *protocol.ReplyKind `json:"type"`
			Role    *protocol.Role      `json:"role"`
			Model   *string             `json:"model"`
			Content *[]json.RawMessage  `json:"content"`
			Usage   *struct {
				Input    *int                  `json:"input_tokens"`
				Output   *int                  `json:"output_tokens"`
				Read     *int                  `json:"cache_read_input_tokens"`
				Creation *int                  `json:"cache_creation_input_tokens"`
				Tier     *protocol.ServiceTier `json:"service_tier"`
			} `json:"usage"`
		}
		if json.Unmarshal(wire.Message, &start) != nil || start.ID == nil || start.Type == nil || start.Role == nil || start.Model == nil || start.Content == nil || start.Usage == nil || start.Usage.Input == nil || start.Usage.Output == nil {
			return badStream("missing message_start fields")
		}
		s.reply = protocol.ModelReply{ID: *start.ID, Type: *start.Type, Role: *start.Role, Model: *start.Model, Content: []protocol.Block{}, Usage: protocol.TokenUsage{InputTokens: *start.Usage.Input, OutputTokens: *start.Usage.Output, CacheReadInputTokens: start.Usage.Read, CacheCreationInputTokens: start.Usage.Creation, ServiceTier: start.Usage.Tier}}
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
		s.reply.Usage.OutputTokens = *wire.Usage.Output
		if wire.Usage.Input != nil {
			s.reply.Usage.InputTokens = *wire.Usage.Input
		}
		if wire.Usage.Read != nil {
			s.reply.Usage.CacheReadInputTokens = wire.Usage.Read
		}
		if wire.Usage.Creation != nil {
			s.reply.Usage.CacheCreationInputTokens = wire.Usage.Creation
		}
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
			Type      protocol.BlockKind   `json:"type"`
			Text      *string              `json:"text"`
			Thinking  *string              `json:"thinking"`
			Signature *string              `json:"signature"`
			ID        *string              `json:"id"`
			Name      *protocol.ToolName   `json:"name"`
			Input     json.RawMessage      `json:"input"`
			Caller    *protocol.ToolCaller `json:"caller"`
			Citations json.RawMessage      `json:"citations"`
		}
		if json.Unmarshal(wire.Block, &block) != nil {
			return badStream("malformed starting block")
		}
		b := streamBlock{kind: block.Type, text: &strings.Builder{}}
		switch block.Type {
		case protocol.BlockText:
			if block.Text == nil {
				return badStream("text missing")
			}
			if _, err := decodeBlock(wire.Block); err != nil {
				return badStream(c.clean(err.Error(), 500))
			}
			b.text.WriteString(*block.Text)
		case protocol.BlockThinking:
			if block.Thinking == nil || block.Signature == nil {
				return badStream("thinking fields missing")
			}
			b.text.WriteString(*block.Thinking)
			b.signature = *block.Signature
		case protocol.BlockToolUse:
			if block.ID == nil || block.Name == nil || len(block.Input) == 0 {
				return badStream("tool fields missing")
			}
			b.id = *block.ID
			b.name = *block.Name
			b.input = append([]byte(nil), block.Input...)
			b.caller = block.Caller
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
		switch b.kind {
		case protocol.BlockText:
			b.block = protocol.NewTextBlock(b.text.String())
		case protocol.BlockThinking:
			b.block = protocol.NewThinkingBlock(b.text.String(), b.signature)
		case protocol.BlockToolUse:
			input, err := protocol.DecodeToolInput(b.name, b.input)
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
		if b.kind != protocol.BlockToolUse || wire.Delta.PartialJSON == nil {
			return badStream("invalid tool input delta")
		}
		if !b.partialInput {
			b.input = nil
			b.partialInput = true
		}
		b.input = append(b.input, []byte(*wire.Delta.PartialJSON)...)
	default:
		return badStream("unsupported content delta")
	}
	if b.text.Len()+len(b.signature)+len(b.input) > protocol.MaxWireBytes {
		return &Failure{Kind: FailureLimit, Message: "provider streamed block exceeds protocol limit"}
	}
	if progress.Text != "" && emit != nil {
		return emit(progress)
	}
	return nil
}
