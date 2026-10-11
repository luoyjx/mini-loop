package jsonvalue

import "testing"

func TestLegacyDefaultJSONMatchesPythonSpacingAndHistoricalScalars(t *testing.T) {
	for _, example := range []struct{ input, output string }{
		{`[]`, `[]`}, {`{}`, `{}`},
		{`[1,{"text":"界","n":NaN,"s":"\ud800"}]`, `[1, {"text": "\u754c", "n": NaN, "s": "\ud800"}]`},
		{`{"z":[true,false,null,1.0],"a":{"x":Infinity,"y":-Infinity}}`, `{"z": [true, false, null, 1.0], "a": {"x": Infinity, "y": -Infinity}}`},
		{`"quote \" π"`, `"quote \" \u03c0"`},
	} {
		value, err := Decode(example.input)
		if err != nil {
			t.Fatal(err)
		}
		out, err := AppendLegacyDefault(value)
		if err != nil || string(out) != example.output {
			t.Fatal(string(out), example.output, err)
		}
	}
}

func TestClosedPythonTruthAndContentProjection(t *testing.T) {
	for _, row := range []struct {
		json, text string
		truth      bool
	}{
		{`null`, "None", false}, {`false`, "False", false}, {`true`, "True", true},
		{`0`, "0", false}, {`-0`, "0", false}, {`-0.0`, "-0.0", false}, {`1e-7`, "1e-07", true},
		{`""`, "", false}, {`"0"`, "0", true}, {`[]`, "[]", false}, {`{}`, "{}", false},
		{`[null,false,true]`, "[None, False, True]", true},
		{`{"quote'":"\n","s":"\ud800","new":"\ud83e\udee8"}`, `{"quote'": '\n', 's': '\ud800', 'new': '\U0001fae8'}`, true},
		{`NaN`, "nan", true}, {`Infinity`, "inf", true}, {`-Infinity`, "-inf", true},
	} {
		value, err := Decode(row.json)
		if err != nil {
			t.Fatal(err)
		}
		if value.Truth() != row.truth || value.PythonString() != row.text {
			t.Fatal(row.json, value.Truth(), value.PythonString())
		}
	}
}
func TestLegacyIndentAndSurrogatePrefix(t *testing.T) {
	value, err := Decode(`[1,{"text":"界","n":NaN,"s":"\ud800"}]`)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := AppendLegacyIndent(value)
	expected := "[\n  1,\n  {\n    \"text\": \"\\u754c\",\n    \"n\": NaN,\n    \"s\": \"\\ud800\"\n  }\n]"
	if err != nil || string(rendered) != expected {
		t.Fatal(string(rendered), err)
	}
	surrogate, err := Decode(`"\ud800"`)
	if err != nil {
		t.Fatal(err)
	}
	text, _ := surrogate.Text()
	if prefix := TextPrefix("😀"+text+"last", 2); prefix != "😀"+text || RuneCount(prefix) != 2 {
		t.Fatal("lost surrogatepass prefix")
	}
	if TextPrefix("short", 10) != "short" || TextPrefix("short", 0) != "" {
		t.Fatal("prefix boundary")
	}
	for _, json := range []string{`[]`, `{}`} {
		value, _ := Decode(json)
		out, err := AppendLegacyIndent(value)
		if err != nil || string(out) != json {
			t.Fatal(string(out), err)
		}
	}
}
