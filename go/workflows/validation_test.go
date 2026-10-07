package workflows

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type validationOutcome struct{ Error, Detail string }
type schemaRecipe struct {
	Schema string `json:"schema_json"`
	Value  string `json:"value_json"`
	validationOutcome
}

func validationMatches(t *testing.T, err error, want validationOutcome) {
	t.Helper()
	got := validationOutcome{}
	if err != nil {
		var failure *ValidationError
		if !errors.As(err, &failure) {
			t.Fatal(err)
		}
		got.Error = string(failure.Kind)
		got.Detail = failure.Detail
	}
	if got != want {
		t.Fatalf("validation got %+v, want %+v", got, want)
	}
}
func parsedValidationValue(t *testing.T, raw string) Value {
	t.Helper()
	value, err := jsonvalue.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestValidationMatchesActualPython(t *testing.T) {
	var fixture struct {
		Definitions []struct {
			Name  string
			Input string `json:"input_json"`
			validationOutcome
		}
		Schemas, Values []schemaRecipe
		Submissions     []struct {
			Kind   string
			Schema string `json:"schema_json"`
			Value  string `json:"value_json"`
			Output []ArtifactSnapshot
			validationOutcome
		}
		Verification []struct {
			Value  string `json:"value_json"`
			Status VerificationStatus
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "testdata", "python-workflow-validation.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Definitions {
		t.Run("definition/"+row.Name, func(t *testing.T) {
			d, err := DecodeDefinition([]byte(row.Input))
			if err != nil {
				t.Fatal(err)
			}
			before := d.Hash()
			validationMatches(t, ValidateDefinition(d), row.validationOutcome)
			if d.Hash() != before {
				t.Fatal("validation changed definition identity")
			}
		})
	}
	for i, row := range fixture.Schemas {
		t.Run("schema/"+string(rune('a'+i)), func(t *testing.T) {
			validationMatches(t, ValidateSchema(parsedValidationValue(t, row.Schema)), row.validationOutcome)
		})
	}
	for i, row := range fixture.Values {
		t.Run("value/"+string(rune('a'+i)), func(t *testing.T) {
			validationMatches(t, ValidateValue(parsedValidationValue(t, row.Schema), parsedValidationValue(t, row.Value)), row.validationOutcome)
		})
	}
	for _, row := range fixture.Submissions {
		t.Run("submission/"+row.Kind, func(t *testing.T) {
			value := parsedValidationValue(t, row.Value)
			submission := ReturnArtifact(value)
			if row.Kind == "wrong-tool" {
				submission = NewArtifactSubmission(value, "other")
			}
			var pointer *ArtifactSubmission
			if row.Kind != "unstructured" {
				pointer = &submission
			}
			artifact, err := ArtifactFromSubmission(pointer, ArtifactBinding{Run: "run", Node: "bound", Attempt: "attempt", Schema: parsedValidationValue(t, row.Schema)})
			validationMatches(t, err, row.validationOutcome)
			if err == nil {
				if len(row.Output) != 1 {
					t.Fatal("missing source artifact")
				}
				got := artifact.Snapshot()
				got.ArtifactID = "<artifact>"
				got.CreatedAt = 0
				if !reflect.DeepEqual(got, row.Output[0]) {
					t.Fatalf("artifact %+v want %+v", got, row.Output[0])
				}
			}
		})
	}
	for _, row := range fixture.Verification {
		if got := VerificationFromValue(parsedValidationValue(t, row.Value)); got != row.Status {
			t.Fatal(row.Value, got, row.Status)
		}
	}
}
