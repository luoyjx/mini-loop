package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/decisions"
)

func TestDecisionSchemaClosedVariantsAndDetachedCopies(t *testing.T) {
	original := DecisionSchema()
	if err := original.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := original.Clone()
	copy.Description = "changed description"
	questions := (*copy.InputSchema.Properties)["questions"]
	rule, ok := questions.Additional.Rule()
	if !ok {
		t.Fatal("question schema missing")
	}
	*(*rule.OneOf[0].Properties)["type"].Const = "altered"
	criteria := (*rule.OneOf[0].Properties)["criteria"]
	*criteria.MaxProperties = 1
	value, _ := criteria.Additional.Rule()
	value.Types[0] = SchemaBoolean
	denied, ok := rule.OneOf[0].Additional.Allowed()
	if !ok || denied {
		t.Fatal("unknown question properties admitted")
	}
	b, err := json.Marshal(copy)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(original)
	if string(a) == string(b) {
		t.Fatal("mutation probe did not alter copy")
	}
	c, _ := json.Marshal(DecisionSchema())
	if string(a) != string(c) {
		t.Fatal("schema aliases compiled defaults")
	}
	var decoded ToolSchema
	if err := json.Unmarshal(a, &decoded); err != nil {
		t.Fatal(err)
	}
	d, _ := json.Marshal(decoded)
	if string(a) != string(d) {
		t.Fatal("schema wire drift")
	}
	for _, raw := range []string{
		`{"type":[]}`, `{"type":["string","string"]}`, `{"type":"unknown"}`,
		`{"type":3}`, `{"type":"object","additionalProperties":null}`,
		`{"type":"object","additionalProperties":1}`, `{"type":"object","additionalProperties":{}}`,
		`{"type":"object","additionalProperties":false,"minProperties":3,"maxProperties":2}`,
		`{"type":"string","maxItems":2}`, `{"type":"array","items":{"type":"number"},"minItems":-1}`,
		`{"oneOf":[{}]}`, `{"unknown":true,"type":"string"}`,
	} {
		var schema InputSchema
		err := json.Unmarshal([]byte(raw), &schema)
		if err == nil {
			err = schema.Validate()
		}
		if err == nil {
			t.Fatal("invalid schema admitted", raw)
		}
	}
	if err := (InputSchema{Type: SchemaString, Types: []SchemaType{SchemaNumber}}).Validate(); err == nil {
		t.Fatal("two type variants admitted")
	}
	bad := InputSchema{Type: SchemaObject, Additional: &SchemaAdditional{}}
	if bad.Validate() == nil {
		t.Fatal("empty additional variant admitted")
	}
	if _, err := bad.Additional.MarshalJSON(); err == nil {
		t.Fatal("empty additional variant encoded")
	}
}

func TestDecisionRecordingProjectionMasksStructureAndCannotExecute(t *testing.T) {
	r, err := decisions.DecodeRequest([]byte(`{"state":{"private-key":["private-value",1,true,null]},"questions":{"private-id":{"type":"noul","instructions":"private-evidence"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	input := DecisionToolInput(r)
	canonical, err := input.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeToolInput(ToolDecision, []byte(canonical))
	if err != nil {
		t.Fatal(err)
	}
	again, _ := decoded.CanonicalJSON()
	if canonical != again {
		t.Fatal("decision identity drift")
	}
	masked := MapToolInputStrings(input, func(s string) string { return strings.ReplaceAll(s, "private", "hidden") })
	if masked.Validate() == nil {
		t.Fatal("recording projection executed")
	}
	if _, ok := masked.Decision(); ok {
		t.Fatal("recording projection exposed executable request")
	}
	if _, err := masked.CanonicalJSON(); err == nil {
		t.Fatal("recording acquired action identity")
	}
	copy := masked.clone()
	b, err := json.Marshal(copy)
	if err != nil || strings.Contains(string(b), "private") {
		t.Fatal(string(b), err)
	}
	if !strings.Contains(string(b), "hidden-key") || !strings.Contains(string(b), "hidden-id") {
		t.Fatal("member names were not masked")
	}
	original, _ := input.CanonicalJSON()
	if original != canonical {
		t.Fatal("recording changed live input")
	}
	second := MapToolInputStrings(copy, func(s string) string { return strings.ReplaceAll(s, "hidden", "masked") })
	b, err = json.Marshal(second)
	if err != nil || strings.Contains(string(b), "hidden") {
		t.Fatal("second projection lost detached tree", err)
	}
	var block Block
	if err := json.Unmarshal([]byte(`{"type":"tool_use","id":"d","name":"decision","input":`+canonical+`}`), &block); err != nil {
		t.Fatal(err)
	}
	if block.Validate() != nil {
		t.Fatal("decision tool block invalid")
	}
}
