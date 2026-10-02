package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// FakeProvider mirrors the default Python fake model's one-bash-then-summary
// shape. It is deterministic and needs no network or credentials.
type FakeProvider struct{}

func (FakeProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	if err := request.Validate(); err != nil {
		return protocol.ModelReply{}, err
	}
	if len(request.Cache.Messages)+boolCount(request.Cache.System != nil) > 4 {
		return protocol.ModelReply{}, fmt.Errorf("request exceeds four cache breakpoints")
	}
	if request.MaxTokens > 8192 {
		return protocol.ModelReply{}, fmt.Errorf("Streaming is required for operations that may take longer than 10 minutes. See https://github.com/anthropics/anthropic-sdk-python#long-requests for more details")
	}
	messages := request.Messages
	finish := func(blocks []protocol.Block, reason protocol.StopReason) (protocol.ModelReply, error) {
		reply := fakeReply(blocks, reason)
		count, err := FakePromptTokens(request)
		if err != nil {
			return protocol.ModelReply{}, err
		}
		reply.Usage.InputTokens, reply.Model = count, request.Model
		return reply, nil
	}
	if len(request.Tools) == 0 {
		return finish([]protocol.Block{protocol.NewTextBlock(fmt.Sprintf("[summary of %d message(s)]", len(messages)))}, protocol.StopEndTurn)
	}
	last := messages[len(messages)-1]
	if prompt, ok := last.Content.Plain(); ok {
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		promptRunes := []rune(prompt)
		if len(promptRunes) > 60 {
			prompt = string(promptRunes[:60])
		}
		return finish([]protocol.Block{
			protocol.NewTextBlock("Working on it."),
			protocol.NewBashUse("toolu_1", "echo handled: "+prompt),
		}, protocol.StopToolUse)
	}
	blocks, ok := last.Content.Blocks()
	if !ok {
		return protocol.ModelReply{}, fmt.Errorf("fake provider cannot read last message")
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
	return finish([]protocol.Block{protocol.NewTextBlock("Done. Tool said: " + resultText)}, protocol.StopEndTurn)
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
