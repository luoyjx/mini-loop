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

func (FakeProvider) Complete(ctx context.Context, messages []protocol.Message) (ModelReply, error) {
	if err := ctx.Err(); err != nil {
		return ModelReply{}, err
	}
	if len(messages) == 0 {
		return ModelReply{}, fmt.Errorf("fake provider requires a message")
	}
	last := messages[len(messages)-1]
	if prompt, ok := last.Content.Plain(); ok {
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		promptRunes := []rune(prompt)
		if len(promptRunes) > 60 {
			prompt = string(promptRunes[:60])
		}
		return ModelReply{
			Content: []protocol.Block{
				protocol.NewTextBlock("Working on it."),
				protocol.NewBashUse("toolu_1", "echo handled: "+prompt),
			},
			StopReason: StopToolUse,
		}, nil
	}
	blocks, ok := last.Content.Blocks()
	if !ok {
		return ModelReply{}, fmt.Errorf("fake provider cannot read last message")
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
	return ModelReply{
		Content:    []protocol.Block{protocol.NewTextBlock("Done. Tool said: " + resultText)},
		StopReason: StopEndTurn,
	}, nil
}
