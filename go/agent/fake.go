package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// FakeGeneration is the responder-owned content; the fake client stamps usage,
// served model, signatures and message identity from the actual request/call.
type FakeGeneration struct {
	Content    []protocol.Block
	StopReason protocol.StopReason
}
type FakeResponder interface {
	Respond(context.Context, protocol.ModelRequest) (FakeGeneration, error)
}

// Configuration is captured at construction. Responders shared across sessions
// must be concurrency-safe. Delay is explicit; no environment is read.
type FakeProviderConfig struct {
	Responder                  FakeResponder
	Thinking                   *bool
	Delay                      time.Duration
	NonStreamingCeiling        *int
	DisableNonStreamingCeiling bool
}

// FakeProvider has one call sequence per client. Use a pointer, including the
// zero value (&FakeProvider{}), and do not copy it after use. The zero value
// matches Python's default responder, thinking enabled, zero delay, 8192 ceiling.
type FakeProvider struct {
	calls          atomic.Uint64
	responder      FakeResponder
	thinking       *bool
	delay          time.Duration
	ceiling        *int
	disableCeiling bool
}

func NewFakeProvider(config FakeProviderConfig) *FakeProvider {
	return &FakeProvider{responder: config.Responder, thinking: clonePointer(config.Thinking),
		delay: config.Delay, ceiling: clonePointer(config.NonStreamingCeiling), disableCeiling: config.DisableNonStreamingCeiling}
}
func (p *FakeProvider) Calls() uint64 { return p.calls.Load() }
func (p *FakeProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	return p.generate(ctx, request, false)
}
func (p *FakeProvider) generate(ctx context.Context, request protocol.ModelRequest, streaming bool) (protocol.ModelReply, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	if len(request.Cache.Messages)+boolCount(request.Cache.System != nil) > 4 {
		return protocol.ModelReply{}, fmt.Errorf("request exceeds four cache breakpoints")
	}
	ceiling := 8192
	if p.ceiling != nil {
		ceiling = *p.ceiling
	}
	if !streaming && !p.disableCeiling && request.MaxTokens > ceiling {
		return protocol.ModelReply{}, fmt.Errorf("Streaming is required for operations that may take longer than 10 minutes. See https://github.com/anthropics/anthropic-sdk-python#long-requests for more details")
	}
	var ordinal uint64
	if !streaming {
		ordinal = p.calls.Add(1)
		if p.delay != 0 {
			timer := time.NewTimer(p.delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return protocol.ModelReply{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
	var generation FakeGeneration
	var err error
	if p.responder != nil {
		generation, err = p.responder.Respond(ctx, request.Clone())
	} else {
		generation, err = defaultFakeGeneration(request)
	}
	if err != nil {
		return protocol.ModelReply{}, err
	}
	if streaming {
		ordinal = p.calls.Add(1)
	}
	signatureOrdinal := ordinal
	if streaming {
		signatureOrdinal--
	} // Python builds stream thinking before incrementing calls.
	blocks := append([]protocol.Block(nil), generation.Content...)
	if p.thinking == nil || *p.thinking {
		blocks = append([]protocol.Block{protocol.NewThinkingBlock("considering the request", fmt.Sprintf("sig_fake_%08d", signatureOrdinal))}, blocks...)
	}
	reply := fakeReply(blocks, generation.StopReason)
	count, err := FakePromptTokens(request)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	reply.ID, reply.Model, reply.Usage.InputTokens = fmt.Sprintf("msg_fake_%06d", ordinal), request.Model, count
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	return reply, reply.Validate()
}
func defaultFakeGeneration(request protocol.ModelRequest) (FakeGeneration, error) {
	messages := request.Messages
	if len(request.Tools) == 0 {
		return FakeGeneration{[]protocol.Block{protocol.NewTextBlock(fmt.Sprintf("[summary of %d message(s)]", len(messages)))}, protocol.StopEndTurn}, nil
	}
	last := messages[len(messages)-1]
	if prompt, ok := last.Content.Plain(); ok {
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		promptRunes := []rune(prompt)
		if len(promptRunes) > 60 {
			prompt = string(promptRunes[:60])
		}
		return FakeGeneration{[]protocol.Block{protocol.NewTextBlock("Working on it."), protocol.NewBashUse("toolu_1", "echo handled: "+prompt)}, protocol.StopToolUse}, nil
	}
	blocks, ok := last.Content.Blocks()
	if !ok {
		return FakeGeneration{}, fmt.Errorf("fake provider cannot read last message")
	}
	resultText := ""
	for _, block := range blocks {
		if result, ok := block.ToolResult(); ok {
			resultText = result.Content
			break
		}
	}
	resultRunes := []rune(resultText)
	if len(resultRunes) > 200 {
		resultText = string(resultRunes[:200])
	}
	return FakeGeneration{[]protocol.Block{protocol.NewTextBlock("Done. Tool said: " + resultText)}, protocol.StopEndTurn}, nil
}

// Streaming returns an explicit transport view over this same client's sequence.
// A direct FakeProvider does not implicitly opt a session into streaming.
func (p *FakeProvider) Streaming() *FakeStreamingProvider { return &FakeStreamingProvider{p} }

type FakeStreamingProvider struct{ provider *FakeProvider }

func (p *FakeStreamingProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	return p.provider.Complete(ctx, request)
}
func (p *FakeStreamingProvider) CompleteStream(ctx context.Context, request protocol.ModelRequest, emit func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	reply, err := p.provider.generate(ctx, request, true)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	for _, block := range reply.Content {
		kind, text := protocol.DeltaText, ""
		if value, ok := block.Text(); ok {
			text = value.Text
		}
		if value, ok := block.Thinking(); ok {
			kind, text = protocol.DeltaThinking, value.Thinking
		}
		runes := []rune(text)
		step := max(1, len(runes)/3)
		for start := 0; start < len(runes); start += step {
			if err := ctx.Err(); err != nil {
				return protocol.ModelReply{}, err
			}
			if emit != nil {
				if err := emit(protocol.StreamDelta{Kind: kind, Text: string(runes[start:min(start+step, len(runes))])}); err != nil {
					return protocol.ModelReply{}, err
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	return reply, nil
}

// Count the complete provider payload, including system and schemas, with the
// Python fake's deliberately distinct ASCII/non-ASCII approximation.
func FakePromptTokens(request protocol.ModelRequest) (int, error) {
	wire, err := request.Wire()
	if err != nil {
		return 0, err
	}
	messages, err := protocol.PythonJSON(wire.Messages, false, false)
	if err != nil {
		return 0, err
	}
	system, err := protocol.PythonJSON(wire.System, false, false)
	if err != nil {
		return 0, err
	}
	tools, err := protocol.PythonJSON(wire.Tools, false, false)
	if err != nil {
		return 0, err
	}
	payload := "[" + messages + ", " + system + ", " + tools + "]"
	ascii, wide := 0, 0
	for _, r := range payload {
		if r < 128 {
			ascii++
		} else {
			wide++
		}
	}
	return ascii/4 + wide + 8, nil
}

func fakeReply(blocks []protocol.Block, reason protocol.StopReason) protocol.ModelReply {
	zeroRead, zeroCreation := 0, 0
	tier := protocol.ServiceTier("standard")
	return protocol.ModelReply{
		ID: "msg_fake", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant,
		Model: "fake-model", Content: blocks, StopReason: reason,
		Usage: protocol.TokenUsage{
			InputTokens: 0, OutputTokens: len(blocks),
			CacheReadInputTokens: &zeroRead, CacheCreationInputTokens: &zeroCreation,
			ServiceTier: &tier,
		},
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
