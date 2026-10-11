package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTranscriptSearchSchemaAndClosedInputMatchSource(t *testing.T) {
	var source struct{ Schema ToolSchema }
	data, err := os.ReadFile("../testdata/python-transcript-search.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(TranscriptSearchSchema(), source.Schema) {
		t.Fatal("Source schema differs")
	}
	if _, ok := DefaultToolSchema(ToolTranscriptSearch); ok {
		t.Fatal("search became default")
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"query":null}`, `{"query":1}`, `{"query":"x","session_id":"foreign"}`, `{"query":"x","owner":"foreign"}`} {
		if _, err := DecodeToolInput(ToolTranscriptSearch, []byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	input, err := DecodeToolInput(ToolTranscriptSearch, []byte(`{"query":"文 secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	value, ok := input.TranscriptSearch()
	if !ok || value.Query != "文 secret" {
		t.Fatal(value, ok)
	}
	canonical, err := input.CanonicalJSON()
	if err != nil || canonical != `{"query":"文 secret"}` {
		t.Fatal(canonical, err)
	}
	masked := MapToolInputStrings(input, func(text string) string { return strings.ReplaceAll(text, "secret", "masked") })
	maskedValue, _ := masked.TranscriptSearch()
	if maskedValue.Query != "文 masked" || value.Query != "文 secret" {
		t.Fatal("mask altered live input")
	}
}
