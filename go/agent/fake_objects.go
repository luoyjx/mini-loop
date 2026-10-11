package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// FakeObjectProvider projects the fake's typed replies as Python's non-SDK
// objects enter Agent._content_payload: tool objects retain id/name/input, while
// caller metadata is absent. Raw FakeProvider replies keep the client wire view,
// where caller=null is present. Explicit dictionary/SDK simulations use that raw
// view instead. This adapter does not alter real-provider caller semantics.
type FakeObjectProvider struct{ provider *FakeProvider }

func (p *FakeProvider) ObjectView() *FakeObjectProvider { return &FakeObjectProvider{provider: p} }

func (p *FakeObjectProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if p == nil || p.provider == nil {
		return protocol.ModelReply{}, errors.New("fake object provider requires a client")
	}
	reply, err := p.provider.Complete(ctx, request)
	if err != nil {
		return reply, err
	}
	for i, block := range reply.Content {
		if tool, ok := block.ToolUse(); ok {
			reply.Content[i] = protocol.NewToolUseWithoutCaller(tool.ID, tool.Input)
		}
	}
	return reply, nil
}
