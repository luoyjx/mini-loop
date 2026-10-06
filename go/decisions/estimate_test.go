package decisions

import (
	"strings"
	"testing"
)

func TestEstimateBoundaryOrderAndProjectionIsolation(t *testing.T) {
	q, _ := NewChoice(StringValue("Choose?"), map[string]Value{"a": NullValue(), "z": NullValue()})
	r, _ := NewRequest(StringValue(""), map[string]Question{"q": q})
	if _, err := Estimate(Request{}, "served", nil, nil); err == nil {
		t.Fatal("zero request admitted")
	}
	result, err := Estimate(r, "served", []byte(`{"distributions":{"q":{"z":0.5,"a":0.5}}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	choice, _ := result.Answers()["q"].Choice()
	if choice.Choice != "z" {
		t.Fatal("response tie order lost")
	}
	for _, raw := range []string{`{"distributions":null}`, `{"distributions":{"q":null}}`, `{"distributions":{"other":{"a":1,"z":0}}}`, strings.Repeat("x", MaxLLMResponseBytes+1)} {
		if _, err := Estimate(r, "served", []byte(raw), nil); err == nil {
			t.Fatal("bad distribution admitted")
		}
	}
	for _, raw := range []string{`{"q":NaN}`, `{"q":Infinity}`, `{"q":-Infinity}`} {
		if !nonfiniteJSON([]byte(raw)) {
			t.Fatal("nonfinite classification lost")
		}
	}
	for _, raw := range []string{`{"q":"NaN"}`, `{"q":"escaped\\\" Infinity"}`, `{"q":NaNx}`} {
		if nonfiniteJSON([]byte(raw)) {
			t.Fatal("string or invalid token classified as numeric constant")
		}
	}
	v, err := DecodeValue([]byte(`{"first":"secret","second":["secret",true,null,1]}`))
	if err != nil {
		t.Fatal(err)
	}
	masked := v.MapStrings(func(s string) string {
		if s == "first" || s == "second" {
			return "collapsed"
		}
		return strings.ReplaceAll(s, "secret", "hidden")
	})
	fields, _ := masked.Object()
	if len(fields) != 1 {
		t.Fatal("masked key collision retained duplicates")
	}
	array, ok := fields["collapsed"].Array()
	if !ok || len(array) != 4 {
		t.Fatal("last masked member did not win")
	}
	raw, _ := masked.MarshalJSON()
	if strings.Contains(string(raw), "secret") {
		t.Fatal("projection leaked string")
	}
	original, _ := v.MapStrings(nil).MarshalJSON()
	if !strings.Contains(string(original), "secret") {
		t.Fatal("projection changed original")
	}
}
