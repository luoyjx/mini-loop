package traceview

import (
	"math"
	"math/big"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// IntegerCount is an immutable, unbounded integer. Its zero value is zero.
// Decimal text remains private and canonical; mutable big.Int values are local.
type IntegerCount struct{ decimal string }

func (c IntegerCount) String() string {
	if c.decimal == "" {
		return "0"
	}
	return c.decimal
}
func (c IntegerCount) Add(other IntegerCount) IntegerCount {
	a, _ := new(big.Int).SetString(c.String(), 10)
	b, _ := new(big.Int).SetString(other.String(), 10)
	return IntegerCount{new(big.Int).Add(a, b).String()}
}
func (c IntegerCount) comma() (string, error) {
	text := c.String()
	start := 0
	if strings.HasPrefix(text, "-") {
		start = 1
	}
	if len(text)-start > 4300 {
		return "", jsonvalue.ErrInteger
	}
	for i := len(text) - 3; i > start; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text, nil
}
func count(value jsonvalue.Value) (IntegerCount, error) {
	if !value.Truth() {
		return IntegerCount{}, nil
	}
	integer, err := value.PythonInteger()
	if err != nil {
		return IntegerCount{}, err
	}
	return IntegerCount{integer.String()}, nil
}

// MetricValue preserves a closed historical value until the render-time int()
// boundary. Token counts, in contrast, have already crossed that boundary.
type MetricValue struct{ value jsonvalue.Value }

func (m MetricValue) counter() (IntegerCount, error) { return count(m.value) }

type Metrics struct {
	ModelCalls, ToolCalls, ToolErrors, Errors MetricValue
	InputTokens, OutputTokens                 IntegerCount
	source                                    jsonvalue.Value
}

func newMetrics(value jsonvalue.Value) Metrics {
	metric := func(name string) MetricValue { v, _ := value.Lookup(name); return MetricValue{v} }
	return Metrics{ModelCalls: metric("model_calls"), ToolCalls: metric("tool_calls"),
		ToolErrors: metric("tool_errors"), Errors: metric("errors"), source: value}
}
func (m Metrics) MarshalArchiveJSON() ([]byte, error) {
	fields := make([]jsonvalue.Field, 0, len(m.source.Keys())+2)
	for _, name := range m.source.Keys() {
		v, _ := m.source.Lookup(name)
		fields = append(fields, jsonvalue.Field{Name: name, Value: v})
	}
	for _, token := range []struct {
		name  string
		count IntegerCount
	}{{"input_tokens", m.InputTokens}, {"output_tokens", m.OutputTokens}} {
		v, err := jsonvalue.Decode(token.count.String())
		if err != nil {
			return nil, err
		}
		fields = append(fields, jsonvalue.Field{Name: token.name, Value: v})
	}
	return jsonvalue.AppendLegacy(nil, jsonvalue.ObjectValue(fields))
}

// metricMap lowers dict(value or {}) at the decoder boundary. Python scalar
// key equality is resolved before JSON stringification of historical pair keys.
func metricMap(value jsonvalue.Value) (jsonvalue.Value, error) {
	if !value.Truth() {
		return jsonvalue.ObjectValue(nil), nil
	}
	if value.Kind() == jsonvalue.Object {
		return value, nil
	}
	items, ok := value.Array()
	if !ok {
		return jsonvalue.Value{}, ErrDocument
	}
	type pair struct{ key, value jsonvalue.Value }
	pairs := make([]pair, 0, len(items))
	positions := make(map[string]int)
	for _, item := range items {
		var key, child jsonvalue.Value
		switch item.Kind() {
		case jsonvalue.Array:
			entry, _ := item.Array()
			if len(entry) != 2 {
				return jsonvalue.Value{}, ErrDocument
			}
			key, child = entry[0], entry[1]
		case jsonvalue.Object:
			keys := item.Keys()
			if len(keys) != 2 {
				return jsonvalue.Value{}, ErrDocument
			}
			key, child = jsonvalue.TextValue(keys[0]), jsonvalue.TextValue(keys[1])
		case jsonvalue.Text:
			text, _ := item.Text()
			if jsonvalue.RuneCount(text) != 2 {
				return jsonvalue.Value{}, ErrDocument
			}
			first := jsonvalue.TextPrefix(text, 1)
			key, child = jsonvalue.TextValue(first), jsonvalue.TextValue(text[len(first):])
		default:
			return jsonvalue.Value{}, ErrDocument
		}
		fingerprint, err := metricKey(key)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if i, exists := positions[fingerprint]; exists {
			pairs[i].value = child
		} else {
			positions[fingerprint] = len(pairs)
			pairs = append(pairs, pair{key, child})
		}
	}
	fields := make([]jsonvalue.Field, 0, len(pairs))
	for _, p := range pairs {
		name, text := p.key.Text()
		if !text {
			encoded, err := jsonvalue.AppendLegacy(nil, p.key)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			name = string(encoded)
		}
		fields = append(fields, jsonvalue.Field{Name: name, Value: p.value})
	}
	return jsonvalue.ObjectValue(fields), nil
}
func metricKey(value jsonvalue.Value) (string, error) {
	if text, ok := value.Text(); ok {
		return "text:" + text, nil
	}
	if value.Kind() == jsonvalue.Null {
		return "null", nil
	}
	if number, ok := value.Float(); ok {
		if math.IsNaN(number) {
			return "nan", nil
		} // JSON decoder constants share Source identity.
		if math.IsInf(number, 1) {
			return "+inf", nil
		}
		if math.IsInf(number, -1) {
			return "-inf", nil
		}
		return "number:" + new(big.Rat).SetFloat64(number).RatString(), nil
	}
	if value.Kind() == jsonvalue.Integer || value.Kind() == jsonvalue.Boolean {
		integer, err := value.PythonInteger()
		if err != nil {
			return "", err
		}
		return "number:" + new(big.Rat).SetInt(integer).RatString(), nil
	}
	return "", ErrDocument
}

// MarshalJSON keeps ordinary JSON strict; archival callers explicitly select
// MarshalArchiveJSON when historical nonfinite values must remain observable.
func (m Metrics) MarshalJSON() ([]byte, error) {
	encoded, err := m.MarshalArchiveJSON()
	if err != nil {
		return nil, err
	}
	value, err := jsonvalue.Decode(string(encoded))
	if err != nil {
		return nil, err
	}
	return value.MarshalUTF8()
}
func (c IntegerCount) MarshalJSON() ([]byte, error) {
	text := c.String()
	if len(strings.TrimPrefix(text, "-")) > 4300 {
		return nil, jsonvalue.ErrInteger
	}
	return []byte(text), nil
}
func (m MetricValue) MarshalJSON() ([]byte, error) { return m.value.MarshalUTF8() }
