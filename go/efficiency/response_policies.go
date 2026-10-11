package efficiency

import (
	"context"
	"errors"
	"math/big"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type PassthroughResponsePlanner struct{}

func responseDescriptor(id string, lossiness Lossiness) Descriptor {
	c := DefaultDescriptor(id, "1", ResponsePolicy)
	c.Lossiness = lossiness
	d, e := NewDescriptor(c)
	if e != nil {
		panic("invalid builtin response descriptor")
	}
	return d
}
func (PassthroughResponsePlanner) Descriptor() Descriptor {
	return responseDescriptor("passthrough-response", Lossless)
}
func (PassthroughResponsePlanner) Plan(ctx context.Context, c ResponseContext) (ResponsePlan, error) {
	if e := ctx.Err(); e != nil {
		return ResponsePlan{}, e
	}
	s, e := c.Snapshot()
	if e != nil {
		return ResponsePlan{}, e
	}
	return NewResponsePlan(ResponsePlanConfig{Settings: s.Settings})
}

type ConciseResponseConfig struct {
	RequireOptIn    bool     `json:"require_opt_in"`
	MaxOutputTokens *big.Int `json:"max_output_tokens"`
	Instruction     string   `json:"instruction"`
}

func DefaultConciseResponseConfig() ConciseResponseConfig {
	return ConciseResponseConfig{RequireOptIn: true, MaxOutputTokens: big.NewInt(1200), Instruction: "Answer concisely. Remove filler, hedging, repetition, and tool-call narration. Preserve negation, code, commands, errors, numbers, units, paths, technical terms, and security or destructive-action warnings exactly enough to act on."}
}

type ConciseResponsePlanner struct {
	config ConciseResponseConfig
	valid  bool
}

func NewConciseResponsePlanner(c ConciseResponseConfig) (ConciseResponsePlanner, error) {
	if c.MaxOutputTokens != nil && c.MaxOutputTokens.Sign() <= 0 {
		return ConciseResponsePlanner{}, errors.New("max_output_tokens must be positive when set")
	}
	if pytext.Strip(c.Instruction) == "" {
		return ConciseResponsePlanner{}, errors.New("concise response instruction cannot be empty")
	}
	c.MaxOutputTokens = copyInteger(c.MaxOutputTokens)
	return ConciseResponsePlanner{c, true}, nil
}
func (ConciseResponsePlanner) Descriptor() Descriptor {
	return responseDescriptor("concise-response", Lossy)
}
func (p ConciseResponsePlanner) Plan(ctx context.Context, c ResponseContext) (ResponsePlan, error) {
	if e := ctx.Err(); e != nil {
		return ResponsePlan{}, e
	}
	if !p.valid {
		return ResponsePlan{}, &ComponentFault{Class: "ValueError", Cause: errors.New("uninitialized concise policy")}
	}
	s, e := c.Snapshot()
	if e != nil {
		return ResponsePlan{}, e
	}
	if p.config.RequireOptIn && !s.ConciseRequested {
		return NewResponsePlan(ResponsePlanConfig{Settings: s.Settings})
	}
	settings, e := s.Settings.Snapshot()
	if e != nil {
		return ResponsePlan{}, e
	}
	included := false
	for _, instruction := range settings.Instructions {
		if instruction == p.config.Instruction {
			included = true
		}
	}
	if !included {
		settings.Instructions = append(settings.Instructions, p.config.Instruction)
	}
	settings.MaxOutputTokens = minimumOutputLimit(settings.MaxOutputTokens, p.config.MaxOutputTokens, s.BudgetTokens)
	verbosity := "concise"
	settings.Verbosity = &verbosity
	metadata := []ResponseMetadata{}
	for _, pair := range settings.Metadata {
		if pair.Key != "response_policy" {
			metadata = append(metadata, pair)
		}
	}
	settings.Metadata = append(metadata, ResponseMetadata{"response_policy", "concise-response"})
	next, e := NewResponseSettings(settings)
	if e != nil {
		return ResponsePlan{}, &ComponentFault{Class: "ValueError", Cause: e}
	}
	return NewResponsePlan(ResponsePlanConfig{Settings: next})
}
