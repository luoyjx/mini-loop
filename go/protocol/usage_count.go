package protocol

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type UsageCountKind uint8

const (
	UsageInteger UsageCountKind = iota
	UsageFloat
	UsageBoolean
	UsageText
	UsageNull
	UsageArray
	UsageObject
)

// UsageCount is an immutable observation of an SDK count field. Structured and
// text variants are observable but never acquire numeric metering authority.
// All recognized fields enter this named, closed union rather than open JSON.
type UsageCount struct {
	kind    UsageCountKind
	integer int
	number  float64
	boolean bool
	text    string
	items   []UsageCount
	members []usageCountMember
}
type usageCountMember struct {
	name  string
	value UsageCount
}

func IntegerUsageCount(value int) UsageCount  { return UsageCount{integer: value} }
func (count UsageCount) Kind() UsageCountKind { return count.kind }
func (count UsageCount) Integer() (int, bool) { return count.integer, count.kind == UsageInteger }
func (count UsageCount) Metered() int {
	switch count.kind {
	case UsageInteger:
		return count.integer
	case UsageFloat:
		return int(count.number)
	case UsageBoolean:
		if count.boolean {
			return 1
		}
	}
	return 0
}
func (count UsageCount) Validate() error {
	_, err := count.MarshalJSON()
	return err
}
func (count UsageCount) jsonValue() jsonvalue.Value {
	switch count.kind {
	case UsageInteger:
		return jsonvalue.IntegerValue(int64(count.integer))
	case UsageFloat:
		return jsonvalue.FloatValue(count.number)
	case UsageBoolean:
		return jsonvalue.BoolValue(count.boolean)
	case UsageText:
		return jsonvalue.TextValue(count.text)
	case UsageNull:
		return jsonvalue.NullValue()
	case UsageArray:
		values := make([]jsonvalue.Value, len(count.items))
		for i, v := range count.items {
			values[i] = v.jsonValue()
		}
		return jsonvalue.ArrayValue(values)
	case UsageObject:
		fields := make([]jsonvalue.Field, len(count.members))
		for i, v := range count.members {
			fields[i] = jsonvalue.Field{Name: v.name, Value: v.value.jsonValue()}
		}
		return jsonvalue.ObjectValue(fields)
	}
	return jsonvalue.FloatValue(math.NaN())
}
func (count UsageCount) MarshalJSON() ([]byte, error) { return count.jsonValue().MarshalJSON() }
func usageCountFromValue(value jsonvalue.Value) (UsageCount, error) {
	var count UsageCount
	switch value.Kind() {
	case jsonvalue.Integer:
		raw, _ := value.Integer()
		n, err := strconv.Atoi(raw)
		if err != nil {
			return count, errors.New("usage integer exceeds native count range")
		}
		count.integer = n
	case jsonvalue.Float:
		n, _ := value.Float()
		limit := float64(^uint(0) >> 1)
		if math.IsNaN(n) || math.IsInf(n, 0) || n >= limit || n < -limit {
			return count, errors.New("usage float exceeds native metering range")
		}
		count.kind, count.number = UsageFloat, n
	case jsonvalue.Boolean:
		count.kind = UsageBoolean
		count.boolean, _ = value.Bool()
	case jsonvalue.Text:
		count.kind = UsageText
		count.text, _ = value.Text()
	case jsonvalue.Null:
		count.kind = UsageNull
	case jsonvalue.Array:
		count.kind = UsageArray
		values, _ := value.Array()
		count.items = make([]UsageCount, len(values))
		for i, v := range values {
			next, err := usageCountFromValue(v)
			if err != nil {
				return count, err
			}
			count.items[i] = next
		}
	case jsonvalue.Object:
		count.kind = UsageObject
		for _, key := range value.Keys() {
			v, _ := value.Lookup(key)
			next, err := usageCountFromValue(v)
			if err != nil {
				return count, err
			}
			count.members = append(count.members, usageCountMember{key, next})
		}
	}
	return count, nil
}
func (count *UsageCount) UnmarshalJSON(data []byte) error {
	value, err := jsonvalue.Decode(string(data))
	if err != nil {
		return err
	}
	next, err := usageCountFromValue(value)
	if err != nil {
		return err
	}
	*count = next
	return nil
}
func (count UsageCount) MapStrings(mask func(string) string) UsageCount {
	if mask == nil {
		return count
	}
	switch count.kind {
	case UsageText:
		count.text = mask(count.text)
	case UsageArray:
		next := make([]UsageCount, len(count.items))
		for i, value := range count.items {
			next[i] = value.MapStrings(mask)
		}
		count.items = next
	case UsageObject:
		next := make([]usageCountMember, len(count.members))
		for i, value := range count.members {
			next[i] = usageCountMember{mask(value.name), value.value.MapStrings(mask)}
		}
		count.members = next
	}
	return count
}

// sdkInteger is the tested scalar normalization used by SDK integer fields.
// Fractional/structured/null observations cannot satisfy a required integer.
func (count UsageCount) sdkInteger() (UsageCount, bool) {
	switch count.kind {
	case UsageInteger:
		return count, true
	case UsageBoolean:
		return IntegerUsageCount(count.Metered()), true
	case UsageFloat:
		if math.Trunc(count.number) == count.number {
			return IntegerUsageCount(int(count.number)), true
		}
	case UsageText:
		raw := strings.TrimSpace(count.text)
		integer, fraction, decimal := strings.Cut(raw, ".")
		if decimal {
			if fraction == "" {
				return count, false
			}
			for i := range fraction {
				if fraction[i] != '0' {
					return count, false
				}
			}
			raw = integer
		}
		// SDK integer strings allow separators only between integer digits.
		// Checking decimal zeroes before conversion avoids float rounding.
		for i := range raw {
			if raw[i] == '_' && (i == 0 || i+1 == len(raw) || raw[i-1] < '0' || raw[i-1] > '9' || raw[i+1] < '0' || raw[i+1] > '9') {
				return count, false
			}
		}
		raw = strings.ReplaceAll(raw, "_", "")
		if n, err := strconv.Atoi(raw); err == nil {
			return IntegerUsageCount(n), true
		}
	}
	return count, false
}
