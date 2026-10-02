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

func (FakeProvider) Complete(ctx context.Context, messages []protocol.Message) (protocol.ModelReply, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	if len(messages) == 0 {
		return protocol.ModelReply{}, fmt.Errorf("fake provider requires a message")
	}
	last := messages[len(messages)-1]
	if prompt, ok := last.Content.Plain(); ok {
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		promptRunes := []rune(prompt)
		if len(promptRunes) > 60 {
			prompt = string(promptRunes[:60])
		}
		return fakeReply([]protocol.Block{
			protocol.NewTextBlock("Working on it."),
			protocol.NewBashUse("toolu_1", "echo handled: "+prompt),
		}, protocol.StopToolUse), nil
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
	return fakeReply([]protocol.Block{protocol.NewTextBlock("Done. Tool said: " + resultText)}, protocol.StopEndTurn), nil
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
