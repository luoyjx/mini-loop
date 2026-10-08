package workflows

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestAdmissionMatchesActualPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Name  string
			Input string `json:"input_json"`
			Caps  DefinitionCaps
			validationOutcome
			Output     string `json:"output_json"`
			PolicyHash Digest `json:"policy_hash"`
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-workflow-admission.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Rows {
		t.Run(row.Name, func(t *testing.T) {
			admission, err := NewDefinitionAdmission(row.Caps)
			if err != nil {
				t.Fatal(err)
			}
			input := parsedValidationValue(t, row.Input)
			before, err := input.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			got, err := admission.Admit(input)
			validationMatches(t, err, row.validationOutcome)
			if err != nil {
				return
			}
			want, err := CanonicalJSON(parsedValidationValue(t, row.Output))
			if err != nil {
				t.Fatal(err)
			}
			output, err := CanonicalJSON(got.Definition().Data())
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != string(want) {
				t.Fatalf("definition\ngot %s\nwant %s", output, want)
			}
			if got.PolicySnapshotHash() != row.PolicyHash {
				t.Fatalf("policy digest got %s want %s", got.PolicySnapshotHash(), row.PolicyHash)
			}
			after, err := input.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("admission mutated caller input")
			}
		})
	}
}

func TestAdmissionPolicyIsCapturedAndIdentityIsRuntimeOwned(t *testing.T) {
	caps := DefaultDefinitionCaps()
	admission, err := NewDefinitionAdmission(caps)
	if err != nil {
		t.Fatal(err)
	}
	caps.MaxRounds = 100
	input := parsedValidationValue(t, `{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}],"budget":{"max_rounds":5}}`)
	_, err = admission.Admit(input)
	validationMatches(t, err, validationOutcome{"WorkflowValidationError", "workflow max_rounds exceeds the process policy"})
	base := `{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`
	plain, err := admission.Admit(parsedValidationValue(t, base))
	if err != nil {
		t.Fatal(err)
	}
	forged, err := admission.Admit(parsedValidationValue(t, `{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}],"revision":"forged","source":"repo","definition_hash":"forged"}`))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Definition().Hash() != forged.Definition().Hash() || plain.Definition().Revision() != forged.Definition().Revision() || plain.PolicySnapshotHash() != forged.PolicySnapshotHash() {
		t.Fatal("immutable admission identity changed with forged metadata")
	}
	if _, err := admission.Admit(parsedValidationValue(t, `null`)); err == nil {
		t.Fatal("null admitted")
	}
	var absent *DefinitionAdmission
	if _, err := absent.Admit(parsedValidationValue(t, base)); err == nil {
		t.Fatal("nil policy admitted")
	}
}

func TestAdmissionRejectsInvalidOperatorCaps(t *testing.T) {
	for _, caps := range []DefinitionCaps{
		{}, {0, 32, 4, 900}, {5, 32, 4, 900}, {4, 3, 4, 900}, {4, 33, 4, 900},
		{4, 32, 0, 900}, {4, 32, 4, 0}, {4, 32, 4, -1}, {4, 32, 4, math.NaN()}, {4, 32, 4, math.Inf(1)},
	} {
		if _, err := NewDefinitionAdmission(caps); err == nil {
			t.Fatalf("invalid policy admitted: %+v", caps)
		}
	}
}
