package userresources

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/memory"
)

const PersonalSkillDraftSchema = "mini-loop.personal-skill-draft/v1"

type CandidateDecision string

const (
	CandidateCreate CandidateDecision = "create"
	CandidateSkip   CandidateDecision = "skip"
)

type CandidateCode string

const (
	CandidateMalformedJSON      CandidateCode = "malformed_json"
	CandidateInvalidSchema      CandidateCode = "invalid_schema"
	CandidateSensitiveOutput    CandidateCode = "sensitive_output"
	CandidateInvalidDecision    CandidateCode = "invalid_decision"
	CandidateInvalidEvidence    CandidateCode = "invalid_evidence"
	CandidateInvalidSkip        CandidateCode = "invalid_skip"
	CandidateInvalidSkill       CandidateCode = "invalid_skill"
	CandidateMaskingUnavailable CandidateCode = "masking_unavailable"
)

type CandidateError struct{ code CandidateCode }

func (e *CandidateError) Error() string       { return string(e.code) }
func (e *CandidateError) Code() CandidateCode { return e.code }
func candidateError(code CandidateCode) error { return &CandidateError{code: code} }

// SkillCandidate retains only validated source fields, before canonical newline
// normalization. Evidence access is detached. It conveys no publication authority.
type SkillCandidate struct {
	decision          CandidateDecision
	description, body string
	evidence          []int
}

func (value SkillCandidate) Decision() CandidateDecision { return value.decision }
func (value SkillCandidate) Description() string         { return value.description }
func (value SkillCandidate) Body() string                { return value.body }
func (value SkillCandidate) EvidenceIndexes() []int      { return append([]int{}, value.evidence...) }

// ParseSkillCandidate follows source validation order. RawMessage maps/arrays
// are transient JSON boundaries and never enter a retained candidate or draft.
func ParseSkillCandidate(raw, name string, messageCount int, masker memory.Masker) (result SkillCandidate, err error) {
	defer func() {
		if recover() != nil {
			result = SkillCandidate{}
			err = candidateError(CandidateMaskingUnavailable)
		}
	}()
	if !utf8.ValidString(raw) {
		return SkillCandidate{}, candidateError(CandidateMalformedJSON)
	}
	wire, valid := candidateJSONLexemes(pytext.Strip(raw))
	if !valid || !json.Valid(wire) {
		return SkillCandidate{}, candidateError(CandidateMalformedJSON)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(wire, &fields) != nil || len(fields) != 5 {
		return SkillCandidate{}, candidateError(CandidateInvalidSchema)
	}
	for _, key := range []string{"schema", "decision", "description", "body", "evidence_indexes"} {
		if _, ok := fields[key]; !ok {
			return SkillCandidate{}, candidateError(CandidateInvalidSchema)
		}
	}
	if masker != nil && candidateMasked(wire, masker) {
		return SkillCandidate{}, candidateError(CandidateSensitiveOutput)
	}
	schema, ok := candidateString(fields["schema"])
	if !ok || schema != PersonalSkillDraftSchema {
		return SkillCandidate{}, candidateError(CandidateInvalidSchema)
	}
	decision, _ := candidateString(fields["decision"])
	if decision != string(CandidateCreate) && decision != string(CandidateSkip) {
		return SkillCandidate{}, candidateError(CandidateInvalidDecision)
	}
	description, descriptionOK := candidateString(fields["description"])
	body, bodyOK := candidateString(fields["body"])
	if !descriptionOK || !bodyOK {
		return SkillCandidate{}, candidateError(CandidateInvalidSchema)
	}
	var indexes []json.RawMessage
	evidenceJSON := strings.TrimSpace(string(fields["evidence_indexes"]))
	if !strings.HasPrefix(evidenceJSON, "[") || json.Unmarshal([]byte(evidenceJSON), &indexes) != nil {
		return SkillCandidate{}, candidateError(CandidateInvalidEvidence)
	}
	for _, index := range indexes {
		value := strings.TrimSpace(string(index))
		// Python ints exclude booleans and float/exponent lexemes, including 0.0.
		if value == "" || (value[0] != '-' && (value[0] < '0' || value[0] > '9')) || strings.ContainsAny(value, ".eE") {
			return SkillCandidate{}, candidateError(CandidateInvalidEvidence)
		}
	}
	if decision == string(CandidateSkip) {
		if description != "" || body != "" || len(indexes) != 0 {
			return SkillCandidate{}, candidateError(CandidateInvalidSkip)
		}
		return SkillCandidate{decision: CandidateSkip}, nil
	}
	if len(indexes) == 0 {
		return SkillCandidate{}, candidateError(CandidateInvalidEvidence)
	}
	evidence := make([]int, len(indexes))
	for i, index := range indexes {
		number, e := strconv.Atoi(strings.TrimSpace(string(index)))
		if e != nil {
			return SkillCandidate{}, candidateError(CandidateInvalidEvidence)
		}
		evidence[i] = number
	}
	seen := make(map[int]bool, len(evidence))
	for _, index := range evidence {
		if index < 0 || index >= messageCount || seen[index] {
			return SkillCandidate{}, candidateError(CandidateInvalidEvidence)
		}
		seen[index] = true
	}
	if _, e := NewCanonicalSkill(SkillFields{Name: name, Description: description, Body: body}); e != nil {
		return SkillCandidate{}, candidateError(CandidateInvalidSkill)
	}
	return SkillCandidate{decision: CandidateCreate, description: description, body: body, evidence: evidence}, nil
}

func candidateString(raw json.RawMessage) (string, bool) {
	var value string
	b := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(b, "\"") {
		return "", false
	}
	err := json.Unmarshal([]byte(b), &value)
	return value, err == nil
}

// candidateMasked inspects decoded strings and keys recursively, including
// ill-typed nested fields, before type validation. Duplicate keys are last-wins.
func candidateMasked(raw json.RawMessage, masker memory.Masker) bool {
	b := strings.TrimSpace(string(raw))
	switch b[0] {
	case '"':
		value, _ := candidateString(raw)
		return masker.MaskText(value) != value
	case '{':
		var values map[string]json.RawMessage
		_ = json.Unmarshal(raw, &values)
		for key, value := range values {
			if masker.MaskText(key) != key || candidateMasked(value, masker) {
				return true
			}
		}
	case '[':
		var values []json.RawMessage
		_ = json.Unmarshal(raw, &values)
		for _, value := range values {
			if candidateMasked(value, masker) {
				return true
			}
		}
	}
	return false
}
