package provider

import (
	"encoding/json"
	"math"
	"math/big"
	"regexp"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// SDK union validation coerces integer fields only if the whole declared variant
// validates. Otherwise construct_type fills absent fields with null and preserves
// their original values. Unknown discriminators construct the first (char) model.
// All intermediate and returned values are closed; wire bytes stay local.
func providerCitations(raw []byte) (protocol.Citations, error) {
	if len(raw) == 0 {
		raw = []byte("null")
	}
	value, err := jsonvalue.Decode(string(raw))
	if err != nil {
		return protocol.Citations{}, err
	}
	if items, ok := value.Array(); ok {
		for i, item := range items {
			items[i] = providerCitationValue(item, true)
		}
		value = jsonvalue.ArrayValue(items)
	}
	encoded, err := value.MarshalJSON()
	if err != nil {
		return protocol.Citations{}, err
	}
	return protocol.DecodeCitations(encoded)
}

func providerCitationValue(value jsonvalue.Value, validate bool) jsonvalue.Value {
	// SDK's loose model constructor maps object entries of a malformed nested list.
	if items, ok := value.Array(); ok {
		for i, item := range items {
			if item.Kind() == jsonvalue.Object {
				items[i] = providerCitationValue(item, false)
			}
		}
		return jsonvalue.ArrayValue(items)
	}
	if value.Kind() != jsonvalue.Object {
		return value
	}
	tag, _ := value.Lookup("type")
	kind, _ := tag.Text()
	if !validate {
		kind = string(protocol.CitationChar)
	}
	required := []string{"cited_text", "type"}
	integers := []string{}
	nullable := []string{}
	optional := []string{}
	known := true
	switch protocol.CitationKind(kind) {
	case protocol.CitationChar:
		integers = []string{"document_index", "start_char_index", "end_char_index"}
		nullable = []string{"document_title", "file_id"}
		optional = []string{"file_id"}
	case protocol.CitationPage:
		integers = []string{"document_index", "start_page_number", "end_page_number"}
		nullable = []string{"document_title", "file_id"}
		optional = []string{"file_id"}
	case protocol.CitationContentBlock:
		integers = []string{"document_index", "start_block_index", "end_block_index"}
		nullable = []string{"document_title", "file_id"}
		optional = []string{"file_id"}
	case protocol.CitationWebSearch:
		required = append(required, "encrypted_index", "url")
		nullable = []string{"title"}
	case protocol.CitationSearchResult:
		required = append(required, "source")
		integers = []string{"search_result_index", "start_block_index", "end_block_index"}
		nullable = []string{"title"}
	default:
		known = false
		integers = []string{"document_index", "start_char_index", "end_char_index"}
		nullable = []string{"document_title", "file_id"}
		optional = []string{"file_id"}
	}
	fields := make([]jsonvalue.Field, 0, len(value.Keys())+len(required)+len(integers)+len(nullable))
	converted := make(map[string]jsonvalue.Value, len(integers))
	valid := known && validate
	for _, key := range required {
		v, present := value.Lookup(key)
		_, text := v.Text()
		valid = valid && present && text
	}
	for _, key := range nullable {
		v, present := value.Lookup(key)
		isOptional := false
		for _, name := range optional {
			isOptional = isOptional || key == name
		}
		_, text := v.Text()
		valid = valid && ((present && (text || v.Kind() == jsonvalue.Null)) || (!present && isOptional))
	}
	for _, key := range integers {
		v, present := value.Lookup(key)
		integer, ok := citationInteger(v)
		valid = valid && present && ok
		converted[key] = integer
	}
	for _, key := range value.Keys() {
		v, _ := value.Lookup(key)
		if replacement, ok := converted[key]; ok && valid {
			v = replacement
		}
		fields = append(fields, jsonvalue.Field{Name: key, Value: v})
	}
	names := append(append(append([]string{}, required...), integers...), nullable...)
	for _, key := range names {
		if _, present := value.Lookup(key); !present {
			fields = append(fields, jsonvalue.Field{Name: key, Value: jsonvalue.NullValue()})
		}
	}
	return jsonvalue.ObjectValue(fields)
}

var citationIntegerText = regexp.MustCompile(`^[+-]?[0-9]+(?:_[0-9]+)*(?:\.0+)?$`)

func citationInteger(value jsonvalue.Value) (jsonvalue.Value, bool) {
	if value.Kind() == jsonvalue.Integer {
		return value, true
	}
	if boolean, ok := value.Bool(); ok {
		if boolean {
			return jsonvalue.IntegerValue(1), true
		}
		return jsonvalue.IntegerValue(0), true
	}
	var integer *big.Int
	if number, ok := value.Float(); ok {
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return jsonvalue.Value{}, false
		}
		integer = new(big.Int)
		_, accuracy := new(big.Float).SetFloat64(number).Int(integer)
		if accuracy != big.Exact {
			return jsonvalue.Value{}, false
		}
	} else if text, ok := value.Text(); ok {
		text = strings.TrimSpace(text)
		if !citationIntegerText.MatchString(text) {
			return jsonvalue.Value{}, false
		}
		text, _, _ = strings.Cut(text, ".")
		var valid bool
		integer, valid = new(big.Int).SetString(strings.ReplaceAll(text, "_", ""), 10)
		if !valid {
			return jsonvalue.Value{}, false
		}
	} else {
		return jsonvalue.Value{}, false
	}
	result, err := jsonvalue.Decode(integer.String())
	return result, err == nil
}

func providerCitation(raw []byte) (protocol.Citation, error) {
	value, err := jsonvalue.Decode(string(raw))
	if err != nil {
		return protocol.Citation{}, err
	}
	encoded, err := providerCitationValue(value, true).MarshalJSON()
	if err != nil {
		return protocol.Citation{}, err
	}
	var citation protocol.Citation
	err = json.Unmarshal(encoded, &citation)
	return citation, err
}
