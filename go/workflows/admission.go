package workflows

import (
	"encoding/json"
	"errors"
	"math"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// DefinitionCaps is operator policy, independent of model-authored budgets.
type DefinitionCaps struct {
	MaxConcurrentAgents int     `json:"max_concurrent_agents"`
	MaxAgents           int     `json:"max_agents"`
	MaxRounds           int     `json:"max_rounds"`
	WallTimeSeconds     float64 `json:"wall_time_seconds"`
}

func DefaultDefinitionCaps() DefinitionCaps {
	return DefinitionCaps{HardMaxConcurrentAgents, HardMaxAgentsPerRun, 4, 900}
}

// DefinitionAdmission captures policy by value; callers cannot widen it later.
// Admission validates content and budgets, but does not authorize execution.
type DefinitionAdmission struct{ caps DefinitionCaps }

func NewDefinitionAdmission(caps DefinitionCaps) (*DefinitionAdmission, error) {
	if caps.MaxConcurrentAgents < 1 || caps.MaxConcurrentAgents > HardMaxConcurrentAgents ||
		caps.MaxAgents < caps.MaxConcurrentAgents || caps.MaxAgents > HardMaxAgentsPerRun ||
		caps.MaxRounds < 1 || caps.WallTimeSeconds <= 0 ||
		math.IsNaN(caps.WallTimeSeconds) || math.IsInf(caps.WallTimeSeconds, 0) {
		return nil, errors.New("invalid workflow definition process policy")
	}
	return &DefinitionAdmission{caps: caps}, nil
}

// AdmittedDefinition retains immutable normalized content and the source policy
// digest. Neither its identity nor its digest is a trusted live RunContext.
type AdmittedDefinition struct {
	definition Definition
	policyHash Digest
}

func (a AdmittedDefinition) Definition() Definition     { return a.definition }
func (a AdmittedDefinition) PolicySnapshotHash() Digest { return a.policyHash }

func (a *DefinitionAdmission) Admit(input Value) (AdmittedDefinition, error) {
	if a == nil {
		return AdmittedDefinition{}, errors.New("workflow definition admission is required")
	}
	if input.Kind() != jsonvalue.Object {
		return AdmittedDefinition{}, ErrDefinition
	}
	fields := make([]jsonvalue.Field, 0, len(input.Keys())+1)
	for _, key := range input.Keys() {
		switch key {
		case "definition_hash", "definition_id", "revision", "parent_revision", "source", "source_version":
			continue
		}
		value, _ := input.Lookup(key)
		fields = append(fields, jsonvalue.Field{Name: key, Value: value})
	}
	fields = append(fields, jsonvalue.Field{Name: "source", Value: jsonvalue.TextValue(string(Dynamic))})
	data, err := jsonvalue.ObjectValue(fields).MarshalJSON()
	if err != nil {
		return AdmittedDefinition{}, err
	}
	definition, err := DecodeDefinition(data)
	if err != nil {
		return AdmittedDefinition{}, err
	}
	if err := ValidateDefinition(definition); err != nil {
		return AdmittedDefinition{}, err
	}
	var wire definitionWire
	data, err = definition.MarshalJSON()
	if err != nil {
		return AdmittedDefinition{}, err
	}
	if err := decodeStrict(data, &wire); err != nil {
		return AdmittedDefinition{}, err
	}
	checks := []struct {
		exceeded bool
		field    string
	}{
		{wire.Budget.MaxConcurrentAgents > a.caps.MaxConcurrentAgents, "max_concurrent_agents"},
		{wire.Budget.MaxAgents > a.caps.MaxAgents, "max_agents"},
		{wire.Budget.MaxRounds > a.caps.MaxRounds, "max_rounds"},
		{wire.Budget.WallTimeSeconds > a.caps.WallTimeSeconds, "wall_time_seconds"},
	}
	for _, check := range checks {
		if check.exceeded {
			return AdmittedDefinition{}, invalid(DefinitionFailure, "workflow "+check.field+" exceeds the process policy")
		}
	}
	// Named wire fields lower to the existing closed JSON sum at the hash boundary.
	encoded, err := json.Marshal(a.caps)
	if err != nil {
		return AdmittedDefinition{}, err
	}
	capValue, err := jsonvalue.Decode(string(encoded))
	if err != nil {
		return AdmittedDefinition{}, err
	}
	policy, _ := definition.Data().Lookup("policy")
	fields = []jsonvalue.Field{{Name: "policy", Value: policy}}
	for _, key := range capValue.Keys() {
		value, _ := capValue.Lookup(key)
		if key == "wall_time_seconds" {
			value = jsonvalue.FloatValue(a.caps.WallTimeSeconds)
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: value})
	}
	hash, err := ContentHash(jsonvalue.ObjectValue(fields), "wfpolicy")
	if err != nil {
		return AdmittedDefinition{}, err
	}
	return AdmittedDefinition{definition: definition, policyHash: hash}, nil
}
