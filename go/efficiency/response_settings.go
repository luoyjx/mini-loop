package efficiency

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

type ResponseMetadata struct{ Key, Value string }

func (m ResponseMetadata) MarshalJSON() ([]byte, error) {
	return json.Marshal([2]string{m.Key, m.Value})
}
func (m *ResponseMetadata) UnmarshalJSON(data []byte) error {
	var pair []string
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return errors.New("metadata entries must be two-item tuples")
	}
	m.Key, m.Value = pair[0], pair[1]
	return nil
}

type ResponseSettingsConfig struct {
	Instructions    []string           `json:"instructions"`
	MaxOutputTokens *big.Int           `json:"max_output_tokens"`
	Verbosity       *string            `json:"verbosity"`
	Metadata        []ResponseMetadata `json:"metadata"`
}

func (c ResponseSettingsConfig) clone() ResponseSettingsConfig {
	c.Instructions = append([]string{}, c.Instructions...)
	c.MaxOutputTokens = copyInteger(c.MaxOutputTokens)
	c.Verbosity = copyText(c.Verbosity)
	c.Metadata = append([]ResponseMetadata{}, c.Metadata...)
	return c
}

type StableResponseSettings struct {
	config ResponseSettingsConfig
	valid  bool
}

func NewResponseSettings(c ResponseSettingsConfig) (StableResponseSettings, error) {
	if len(c.Instructions) > 16 {
		return StableResponseSettings{}, errors.New("instructions must contain at most 16 entries")
	}
	total := 0
	for _, instruction := range c.Instructions {
		n := jsonvalue.RuneCount(instruction)
		if n > 2048 {
			return StableResponseSettings{}, errors.New("instruction entries must be at most 2048 characters")
		}
		total += n
	}
	if total > 8192 {
		return StableResponseSettings{}, errors.New("instructions must total at most 8192 characters")
	}
	if len(c.Metadata) > 32 {
		return StableResponseSettings{}, errors.New("metadata must contain at most 32 entries")
	}
	seen := map[string]bool{}
	for _, pair := range c.Metadata {
		n := jsonvalue.RuneCount(pair.Key)
		if n == 0 || n > 128 {
			return StableResponseSettings{}, errors.New("metadata keys must be 1-128 characters")
		}
		if jsonvalue.RuneCount(pair.Value) > 512 {
			return StableResponseSettings{}, errors.New("metadata values must be at most 512 characters")
		}
		if seen[pair.Key] {
			return StableResponseSettings{}, errors.New("metadata keys must be unique")
		}
		seen[pair.Key] = true
	}
	if c.MaxOutputTokens != nil && c.MaxOutputTokens.Sign() <= 0 {
		return StableResponseSettings{}, errors.New("max_output_tokens must be positive when set")
	}
	if c.Verbosity != nil && (jsonvalue.RuneCount(*c.Verbosity) == 0 || jsonvalue.RuneCount(*c.Verbosity) > 64) {
		return StableResponseSettings{}, errors.New("verbosity must be 1-64 characters when set")
	}
	return StableResponseSettings{c.clone(), true}, nil
}
func DefaultResponseSettings() StableResponseSettings {
	s, _ := NewResponseSettings(ResponseSettingsConfig{})
	return s
}
func (s StableResponseSettings) Snapshot() (ResponseSettingsConfig, error) {
	if !s.valid {
		return ResponseSettingsConfig{}, errors.New("settings must be StableRequestSettings")
	}
	return s.config.clone(), nil
}
func (s StableResponseSettings) MarshalJSON() ([]byte, error) {
	c, e := s.Snapshot()
	if e != nil {
		return nil, e
	}
	return json.Marshal(c)
}
func pythonTuple(items []string) string {
	if len(items) == 0 {
		return "()"
	}
	joined := strings.Join(items, ", ")
	if len(items) == 1 {
		joined += ","
	}
	return "(" + joined + ")"
}
func (s StableResponseSettings) representation() (string, error) {
	c, err := s.Snapshot()
	if err != nil {
		return "", err
	}
	instructions := make([]string, 0, len(c.Instructions))
	for _, instruction := range c.Instructions {
		instructions = append(instructions, pytext.Repr(instruction))
	}
	metadata := make([]string, 0, len(c.Metadata))
	for _, pair := range c.Metadata {
		metadata = append(metadata, "("+pytext.Repr(pair.Key)+", "+pytext.Repr(pair.Value)+")")
	}
	limit, verbosity := "None", "None"
	if c.MaxOutputTokens != nil {
		limit = c.MaxOutputTokens.String()
	}
	if c.Verbosity != nil {
		verbosity = pytext.Repr(*c.Verbosity)
	}
	return "StableRequestSettings(instructions=" + pythonTuple(instructions) + ", max_output_tokens=" + limit + ", verbosity=" + verbosity + ", metadata=" + pythonTuple(metadata) + ")", nil
}
func (s StableResponseSettings) equal(other StableResponseSettings) bool {
	a, b := s.config, other.config
	if len(a.Instructions) != len(b.Instructions) || len(a.Metadata) != len(b.Metadata) {
		return false
	}
	for i := range a.Instructions {
		if a.Instructions[i] != b.Instructions[i] {
			return false
		}
	}
	for i := range a.Metadata {
		if a.Metadata[i] != b.Metadata[i] {
			return false
		}
	}
	if (a.MaxOutputTokens == nil) != (b.MaxOutputTokens == nil) || (a.Verbosity == nil) != (b.Verbosity == nil) {
		return false
	}
	if a.MaxOutputTokens != nil && a.MaxOutputTokens.Cmp(b.MaxOutputTokens) != 0 {
		return false
	}
	return a.Verbosity == nil || *a.Verbosity == *b.Verbosity
}

type ResponseContextConfig struct {
	Task                 string                 `json:"task"`
	Settings             StableResponseSettings `json:"settings"`
	ProviderCapabilities []string               `json:"provider_capabilities"`
	BudgetTokens         *big.Int               `json:"budget_tokens"`
	ConciseRequested     bool                   `json:"concise_requested"`
	AppliedBy            []string               `json:"applied_by"`
}
type ResponseContext struct {
	config ResponseContextConfig
	valid  bool
}

func DefaultResponseContext(task string) ResponseContextConfig {
	return ResponseContextConfig{Task: task, Settings: DefaultResponseSettings()}
}
func NewResponseContext(c ResponseContextConfig) (ResponseContext, error) {
	if _, e := c.Settings.Snapshot(); e != nil {
		return ResponseContext{}, e
	}
	if c.BudgetTokens != nil && c.BudgetTokens.Sign() <= 0 {
		return ResponseContext{}, errors.New("budget_tokens must be positive when set")
	}
	c.ProviderCapabilities = append([]string{}, c.ProviderCapabilities...)
	c.BudgetTokens = copyInteger(c.BudgetTokens)
	c.AppliedBy = deduplicateMarkers(c.AppliedBy)
	return ResponseContext{c, true}, nil
}
func (c ResponseContext) Snapshot() (ResponseContextConfig, error) {
	if !c.valid {
		return ResponseContextConfig{}, errors.New("uninitialized response context")
	}
	out := c.config
	out.ProviderCapabilities = append([]string{}, out.ProviderCapabilities...)
	out.BudgetTokens = copyInteger(out.BudgetTokens)
	out.AppliedBy = append([]string{}, out.AppliedBy...)
	return out, nil
}

type ResponsePlanConfig struct {
	Settings StableResponseSettings
	Warnings []string
}
type ResponsePlan struct {
	config ResponsePlanConfig
	valid  bool
}

func NewResponsePlan(c ResponsePlanConfig) (ResponsePlan, error) {
	if _, e := c.Settings.Snapshot(); e != nil {
		return ResponsePlan{}, errors.New("response policy settings must be StableRequestSettings")
	}
	if e := validateWarnings(c.Warnings); e != nil {
		return ResponsePlan{}, e
	}
	c.Warnings = append([]string{}, c.Warnings...)
	return ResponsePlan{c, true}, nil
}
func (p ResponsePlan) Snapshot() (ResponsePlanConfig, error) {
	if !p.valid {
		return ResponsePlanConfig{}, errors.New("response policy returned the wrong result type")
	}
	c := p.config
	c.Warnings = append([]string{}, c.Warnings...)
	return c, nil
}
func minimumOutputLimit(values ...*big.Int) *big.Int {
	var result *big.Int
	for _, v := range values {
		if v != nil && (result == nil || v.Cmp(result) < 0) {
			result = copyInteger(v)
		}
	}
	return result
}
