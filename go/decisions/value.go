// Package decisions models explicit judgments. It never selects or executes an action.
package decisions

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Value is a closed JSON value. Objects and arrays are detached on construction
// and access; arbitrary Go objects cannot enter decision state or criteria.
type Value struct {
	kind    ValueKind
	text    string
	boolean bool
	array   []Value
	object  map[string]Value
	keys    []string
}
type ValueKind uint8

const (
	Null ValueKind = iota
	String
	Number
	Boolean
	Array
	Object
)

func NullValue() Value                     { return Value{} }
func StringValue(s string) Value           { return Value{kind: String, text: s} }
func BoolValue(b bool) Value               { return Value{kind: Boolean, boolean: b} }
func ArrayValue(v []Value) Value           { return Value{kind: Array, array: cloneValues(v)} }
func ObjectValue(v map[string]Value) Value { return Value{kind: Object, object: cloneObject(v)} }

var numberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func NumberValue(s string) (Value, error) {
	if !numberPattern.MatchString(s) {
		return Value{}, invalid("Decision values must be finite JSON data.")
	}
	if strings.ContainsAny(s, ".eE") {
		f, e := strconv.ParseFloat(s, 64)
		if e != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return Value{}, invalid("Decision values must be finite JSON data.")
		}
		if a := math.Abs(f); a != 0 && (a < 1e-4 || a >= 1e16) {
			s = strconv.FormatFloat(f, 'e', -1, 64)
		} else {
			s = strconv.FormatFloat(f, 'f', -1, 64)
			if !strings.Contains(s, ".") {
				s += ".0"
			}
		}
	} else if s == "-0" {
		s = "0"
	}
	return Value{kind: Number, text: s}, nil
}
func (v Value) Kind() ValueKind                  { return v.kind }
func (v Value) Text() (string, bool)             { return v.text, v.kind == String }
func (v Value) Number() (string, bool)           { return v.text, v.kind == Number }
func (v Value) Bool() (bool, bool)               { return v.boolean, v.kind == Boolean }
func (v Value) Array() ([]Value, bool)           { return cloneValues(v.array), v.kind == Array }
func (v Value) Object() (map[string]Value, bool) { return cloneObject(v.object), v.kind == Object }

// Keys preserves decoded member order for source tie breaking. Constructed
// objects have canonical order. The mutable slice never escapes this value.
func (v Value) Keys() []string {
	if v.kind != Object {
		return nil
	}
	if v.keys != nil {
		return append([]string{}, v.keys...)
	}
	keys := make([]string, 0, len(v.object))
	for k := range v.object {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func (v Value) Clone() Value {
	v.array = cloneValues(v.array)
	v.object = cloneObject(v.object)
	v.keys = append([]string(nil), v.keys...)
	return v
}

// MapStrings detaches a JSON projection and masks strings and member names before
// escaping. Masked-key collisions retain the last member, as in source dicts.
func (v Value) MapStrings(mask func(string) string) Value {
	if mask == nil {
		return v.Clone()
	}
	switch v.kind {
	case String:
		return StringValue(mask(v.text))
	case Array:
		items := make([]Value, len(v.array))
		for i, item := range v.array {
			items[i] = item.MapStrings(mask)
		}
		return ArrayValue(items)
	case Object:
		members := make(map[string]Value, len(v.object))
		keys := make([]string, 0, len(v.object))
		for _, key := range v.Keys() {
			masked := mask(key)
			if _, exists := members[masked]; !exists {
				keys = append(keys, masked)
			}
			members[masked] = v.object[key].MapStrings(mask)
		}
		return Value{kind: Object, object: members, keys: keys}
	default:
		return v.Clone()
	}
}
func cloneValues(a []Value) []Value {
	if a == nil {
		return nil
	}
	out := make([]Value, len(a))
	for i, v := range a {
		out[i] = v.Clone()
	}
	return out
}
func cloneObject(a map[string]Value) map[string]Value {
	if a == nil {
		return nil
	}
	out := make(map[string]Value, len(a))
	for k, v := range a {
		out[k] = v.Clone()
	}
	return out
}
func (v Value) MarshalJSON() ([]byte, error) { return encodeValue(v, MaxResponseBytes) }
func (v *Value) UnmarshalJSON(b []byte) error {
	out, e := DecodeValue(b)
	if e == nil {
		*v = out
	}
	return e
}

// DecodeValue is the only open JSON boundary. Decoder tokens are immediately
// lowered into the six supported variants; no dynamic payload is retained.
func DecodeValue(b []byte) (Value, error) {
	if len(b) > MaxResponseBytes {
		return Value{}, invalid("Decision JSON exceeds the byte limit.")
	}
	if !utf8.Valid(b) || !validSurrogates(b) {
		return Value{}, invalid("Decision values must be valid JSON data.")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	v, e := readValue(d, 0)
	if e != nil {
		return Value{}, e
	}
	if _, e = d.Token(); e != io.EOF {
		return Value{}, invalid("Decision values must be valid JSON data.")
	}
	return v, nil
}
func readValue(d *json.Decoder, depth int) (Value, error) {
	if depth > 32 {
		return Value{}, invalid("Decision JSON exceeds the nesting limit.")
	}
	token, e := d.Token()
	if e != nil {
		return Value{}, invalid("Decision values must be valid JSON data.")
	}
	switch x := token.(type) {
	case nil:
		return NullValue(), nil
	case string:
		return StringValue(x), nil
	case bool:
		return BoolValue(x), nil
	case json.Number:
		return NumberValue(string(x))
	case json.Delim:
		if x == '[' {
			a := []Value{}
			for d.More() {
				v, e := readValue(d, depth+1)
				if e != nil {
					return Value{}, e
				}
				a = append(a, v)
			}
			if t, e := d.Token(); e != nil || t != json.Delim(']') {
				return Value{}, invalid("Decision values must be valid JSON data.")
			}
			return Value{kind: Array, array: a}, nil
		}
		if x == '{' {
			a := map[string]Value{}
			keys := []string{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return Value{}, invalid("Decision values must be valid JSON data.")
				}
				key, ok := k.(string)
				if !ok {
					return Value{}, invalid("Decision values must be valid JSON data.")
				}
				if _, exists := a[key]; exists {
					return Value{}, invalid("Duplicate decision JSON key.")
				}
				v, e := readValue(d, depth+1)
				if e != nil {
					return Value{}, e
				}
				a[key] = v
				keys = append(keys, key)
			}
			if t, e := d.Token(); e != nil || t != json.Delim('}') {
				return Value{}, invalid("Decision values must be valid JSON data.")
			}
			return Value{kind: Object, object: a, keys: keys}, nil
		}
	}
	return Value{}, invalid("Decision values must be valid JSON data.")
}
func validSurrogates(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '"' {
			continue
		}
		i++
		for i < len(b) && b[i] != '"' {
			if b[i] == '\\' {
				i++
				if i >= len(b) {
					return false
				}
				if b[i] == 'u' && i+4 < len(b) {
					n, e := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
					if e != nil {
						return false
					}
					i += 4
					if n >= 0xd800 && n <= 0xdbff {
						if i+6 >= len(b) || string(b[i+1:i+3]) != "\\u" {
							return false
						}
						m, e := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
						if e != nil || m < 0xdc00 || m > 0xdfff {
							return false
						}
						i += 6
					} else if n >= 0xdc00 && n <= 0xdfff {
						return false
					}
				}
			}
			i++
		}
	}
	return true
}

// encodeValue bounds writes before allocation and matches compact UTF-8 Python
// string escaping. Map order is canonical; numeric kind and float .0 survive.
func encodeValue(v Value, limit int) ([]byte, error) {
	b := make([]byte, 0, min(limit, 1024))
	var write func(Value, int) error
	appendText := func(s string) error {
		if len(s) > limit-len(b) {
			return invalid("Decision JSON exceeds the byte limit.")
		}
		b = append(b, s...)
		return nil
	}
	quote := func(s string) error {
		if !utf8.ValidString(s) {
			return invalid("Decision values must be valid JSON data.")
		}
		if e := appendText("\""); e != nil {
			return e
		}
		for _, r := range s {
			var x string
			switch r {
			case '"':
				x = `\"`
			case '\\':
				x = `\\`
			case '\b':
				x = `\b`
			case '\f':
				x = `\f`
			case '\n':
				x = `\n`
			case '\r':
				x = `\r`
			case '\t':
				x = `\t`
			default:
				if r < 32 {
					x = `\u` + "0000"
					h := strconv.FormatInt(int64(r), 16)
					x = x[:6-len(h)] + h
				} else {
					x = string(r)
				}
			}
			if e := appendText(x); e != nil {
				return e
			}
		}
		return appendText("\"")
	}
	write = func(v Value, depth int) error {
		if depth > 32 {
			return invalid("Decision JSON exceeds the nesting limit.")
		}
		switch v.kind {
		case Null:
			return appendText("null")
		case String:
			return quote(v.text)
		case Number:
			if !numberPattern.MatchString(v.text) {
				return invalid("Decision values must be finite JSON data.")
			}
			return appendText(v.text)
		case Boolean:
			return appendText(strconv.FormatBool(v.boolean))
		case Array:
			if e := appendText("["); e != nil {
				return e
			}
			for i, x := range v.array {
				if i > 0 {
					if e := appendText(","); e != nil {
						return e
					}
				}
				if e := write(x, depth+1); e != nil {
					return e
				}
			}
			return appendText("]")
		case Object:
			if e := appendText("{"); e != nil {
				return e
			}
			keys := make([]string, 0, len(v.object))
			for k := range v.object {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for i, k := range keys {
				if i > 0 {
					if e := appendText(","); e != nil {
						return e
					}
				}
				if e := quote(k); e != nil {
					return e
				}
				if e := appendText(":"); e != nil {
					return e
				}
				if e := write(v.object[k], depth+1); e != nil {
					return e
				}
			}
			return appendText("}")
		default:
			return invalid("Decision values must be finite JSON data.")
		}
	}
	if e := write(v, 0); e != nil {
		return nil, e
	}
	return b, nil
}
