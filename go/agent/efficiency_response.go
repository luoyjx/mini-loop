package agent

import (
	"context"
	"math/big"
	"strings"

	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func latestResponseTask(messages []protocol.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != protocol.RoleUser {
			continue
		}
		if text, ok := message.Content.Plain(); ok {
			return text
		}
		blocks, _ := message.Content.Blocks()
		parts := []string{}
		for _, block := range blocks {
			if text, ok := block.Text(); ok {
				parts = append(parts, text.Text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return ""
}

// Response planning happens before cache annotation. The provider-neutral policy
// contributes instructions and a tighter output limit only, as in Source Agent.
func (s *Session) planResponseRequest(ctx context.Context, request protocol.ModelRequest) (protocol.ModelRequest, error) {
	if s.tokenEfficiency == nil || request.Purpose != protocol.PurposeAgentTurn {
		return request, nil
	}
	request = request.Clone()
	if request.MaxTokens == 0 {
		request.MaxTokens = s.maxTokens
	}
	cap := big.NewInt(int64(request.MaxTokens))
	settings, err := efficiency.NewResponseSettings(efficiency.ResponseSettingsConfig{MaxOutputTokens: cap})
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	c := efficiency.DefaultResponseContext(latestResponseTask(request.Messages))
	c.Settings, c.BudgetTokens, c.ConciseRequested = settings, cap, s.conciseResponse
	input, err := efficiency.NewResponseContext(c)
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	outcome, err := s.tokenEfficiency.PlanResponse(ctx, input)
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	output, err := outcome.Snapshot()
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	for _, receipt := range output.Receipts {
		metrics := optimizationMetrics(receipt)
		s.events.append(SessionEvent{kind: EventOptimizationReceipt, optimizationReceipt: metrics})
	}
	result, err := output.Context.Settings.Snapshot()
	if err != nil {
		return protocol.ModelRequest{}, err
	}
	if len(result.Instructions) > 0 {
		addition := strings.Join(result.Instructions, "\n\n")
		if request.System != nil {
			addition = *request.System + "\n\n" + addition
		}
		request.System = &addition
	}
	if result.MaxOutputTokens != nil && result.MaxOutputTokens.Cmp(cap) < 0 {
		// The positive candidate is smaller than the native int request limit.
		request.MaxTokens = int(result.MaxOutputTokens.Int64())
	}
	return request, nil
}
