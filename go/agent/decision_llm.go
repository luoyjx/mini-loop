package agent

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type DecisionLLMConfig struct {
	Timeout         time.Duration
	MaxOutputTokens int
}

func (config DecisionLLMConfig) normalized() (DecisionLLMConfig, error) {
	if config.Timeout < 0 || config.MaxOutputTokens < 0 {
		return config, errors.New("decision timeout and token limit cannot be negative")
	}
	if config.Timeout == 0 {
		config.Timeout = DecisionToolTimeout
	}
	if config.MaxOutputTokens == 0 {
		config.MaxOutputTokens = 4096
	}
	return config, nil
}

// LLMDecisionProvider pins immutable dependencies and explicit run provenance.
// Each evaluation gets an independent child with no parent history/tools/cache,
// transport stream, token meter, recovery model or mutable session state.
type LLMDecisionProvider struct {
	provider  Provider
	recovery  Recovery
	model     string
	config    DecisionLLMConfig
	label     string
	depth     int
	sessionID SessionID
	owner     OwnerID
	workspace string
	run       RunContext
	secrets   TextMasker
	limiter   *ConcurrencyLimiter
	events    *sessionEvents
}

func NewLLMDecisionProvider(parent *Session, run RunContext, config DecisionLLMConfig) (*LLMDecisionProvider, error) {
	if parent == nil || parent.provider == nil || parent.events == nil || parent.recovery == nil {
		return nil, errors.New("LLM decision requires a bound parent")
	}
	config, err := config.normalized()
	if err != nil {
		return nil, err
	}
	if run.MessageID() == "" {
		run, err = DefaultRunContext()
		if err != nil {
			return nil, err
		}
	}
	return &LLMDecisionProvider{provider: parent.provider, recovery: parent.recovery, model: parent.model, config: config, label: parent.label + "/decision", depth: parent.depth + 1, sessionID: parent.id, owner: parent.owner, workspace: parent.workspace, run: run.clone(), secrets: parent.secrets, limiter: parent.modelLimiter, events: parent.events}, nil
}

// This adapter deliberately exposes Complete only. Complete-response validation
// happens inside the recovery call so partial replies cannot be continued.
type decisionCompleteProvider struct{ provider Provider }

func (p decisionCompleteProvider) Complete(ctx context.Context, request protocol.ModelRequest) (reply protocol.ModelReply, err error) {
	defer func() {
		if recover() != nil {
			reply = protocol.ModelReply{}
			err = decisions.LLMFailure(false)
		}
	}()
	reply, err = p.provider.Complete(ctx, request)
	if err != nil {
		return protocol.ModelReply{}, err
	}
	if err := completeDecisionResponse(reply); err != nil {
		return protocol.ModelReply{}, err
	}
	return reply, nil
}
func completeDecisionResponse(reply protocol.ModelReply) error {
	if reply.StopReason != protocol.StopEndTurn {
		return decisions.ValidationFailure("Decision response did not complete")
	}
	if len(reply.Content) == 0 {
		return decisions.ValidationFailure("Decision response has no content")
	}
	for _, block := range reply.Content {
		if block.Kind() != protocol.BlockText && block.Kind() != protocol.BlockThinking && block.Kind() != protocol.BlockRedactedThinking {
			return decisions.ValidationFailure("Decision response contains unsupported content")
		}
	}
	return nil
}

func (p *LLMDecisionProvider) Evaluate(ctx context.Context, request decisions.Request) (decisions.Result, error) {
	if p == nil || p.provider == nil || p.events == nil {
		return decisions.Result{}, errors.New("uninitialized LLM decision provider")
	}
	request, err := maskedDecisionRequest(request, p.secrets)
	if err != nil {
		return decisions.Result{}, err
	}
	run, err := p.run.DerivePeerAgent(p.label)
	if err != nil {
		return decisions.Result{}, err
	}
	catalog, err := NewToolCatalog()
	if err != nil {
		return decisions.Result{}, err
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		return decisions.Result{}, err
	}
	child, err := NewSessionWithGate(p.sessionID, p.owner, decisionCompleteProvider{p.provider}, gate, ModeReadonly, p.workspace, 1)
	if err != nil {
		return decisions.Result{}, err
	}
	child.label, child.depth, child.currentRun = p.label, p.depth, run
	child.recovery, child.model, child.cachePolicy, child.modelLimiter = p.recovery, p.model, NullCachePolicy{}, p.limiter
	child.secrets, child.events.secrets, child.events.parent = p.secrets, p.secrets, p.events
	child.events.setScope(EventScope{Label: p.label, Depth: p.depth, RunContext: run})
	body, err := protocol.PythonJSON(request, false, false)
	if err != nil {
		return decisions.Result{}, err
	}
	system := decisions.LLMSystem
	modelRequest := protocol.ModelRequest{Model: p.model, MaxTokens: p.config.MaxOutputTokens, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent(body)}}, System: &system, Tools: []protocol.ToolSchema{}, Purpose: protocol.PurposeDecision}
	callContext, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()
	reply, err := child.completeModel(callContext, modelRequest, nil)
	if err == nil {
		err = completeDecisionResponse(reply)
	}
	if err != nil {
		if ctx.Err() != nil {
			return decisions.Result{}, ctx.Err()
		}
		if callContext.Err() == context.DeadlineExceeded {
			return decisions.Result{}, decisions.LLMFailure(true)
		}
		var contract *decisions.Error
		if errors.As(err, &contract) {
			return decisions.Result{}, contract
		}
		return decisions.Result{}, decisions.LLMFailure(false)
	}
	var text strings.Builder
	for _, block := range reply.Content {
		if v, ok := block.Text(); ok {
			if !utf8.ValidString(v.Text) {
				return decisions.Result{}, decisions.ValidationFailure("Decision response contains invalid text")
			}
			if len(v.Text) > decisions.MaxLLMResponseBytes-text.Len() {
				return decisions.Result{}, decisions.ValidationFailure("Decision response exceeds the size limit")
			}
			text.WriteString(v.Text)
		}
	}
	usage := decisions.TokenUsage{InputTokens: int64(reply.Usage.InputTokens), OutputTokens: int64(reply.Usage.OutputTokens)}
	return decisions.Estimate(request, reply.Model, []byte(text.String()), &usage)
}
