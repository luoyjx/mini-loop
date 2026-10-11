package protocol

import (
	"encoding/json"
	"math/big"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTokenArtifactSchemaAndClosedPagination(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-efficiency-recovery.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct{ Schema ToolSchema }
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ReadTokenArtifactSchema(), source.Schema) {
		t.Fatal("Source schema differs")
	}
	if _, ok := DefaultToolSchema(ToolReadTokenArtifact); ok {
		t.Fatal("recovery became default")
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"raw_ref":null}`, `{"raw_ref":1}`, `{"raw_ref":"x","session_id":"foreign"}`, `{"raw_ref":"x","offset":true}`, `{"raw_ref":"x","limit":1.5}`} {
		if _, err = DecodeToolInput(ToolReadTokenArtifact, []byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	for _, raw := range []string{`{"raw_ref":"x"}`, `{"limit":null,"offset":null,"raw_ref":"x"}`, `{"limit":5,"offset":2,"raw_ref":"x"}`} {
		input, e := DecodeToolInput(ToolReadTokenArtifact, []byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		canonical, e := input.CanonicalJSON()
		if e != nil || canonical != raw {
			t.Fatal(canonical, raw, e)
		}
		projected := MapToolInputStrings(input, func(text string) string { return strings.ReplaceAll(text, "x", "masked") })
		p, e := projected.CanonicalJSON()
		if e != nil || p != strings.ReplaceAll(raw, `"x"`, `"masked"`) {
			t.Fatal("mask/null/canonical drift", p, e)
		}
	}
	huge := new(big.Int).Exp(big.NewInt(10), big.NewInt(100), nil)
	input := ReadTokenArtifactToolInput(ReadTokenArtifactInput{RawRef: "x", Offset: huge, Limit: big.NewInt(5)})
	huge.SetInt64(0)
	value, _ := input.ReadTokenArtifact()
	value.Offset.SetInt64(2)
	value.Limit.SetInt64(0)
	fresh, _ := input.ReadTokenArtifact()
	if len(fresh.Offset.String()) != 101 || fresh.Limit.Int64() != 5 {
		t.Fatal("pagination aliases caller state")
	}
	masked := MapToolInputStrings(input, func(s string) string { return s })
	v, _ := masked.ReadTokenArtifact()
	v.Offset.SetInt64(3)
	original, _ := input.ReadTokenArtifact()
	if len(original.Offset.String()) != 101 {
		t.Fatal("recording input aliases source")
	}
}
