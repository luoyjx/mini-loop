package jsonvalue

import (
	"errors"
	"math"
	"math/big"
)

var ErrNumericComparison = errors.New("archive values cannot be compared as Python numbers")

func numericKind(kind Kind) bool { return kind == Integer || kind == Float || kind == Boolean }

func exactNumber(value Value) *big.Rat {
	switch value.kind {
	case Integer:
		n, _ := new(big.Int).SetString(value.text, 10)
		return new(big.Rat).SetInt(n)
	case Boolean:
		if value.boolean {
			return big.NewRat(1, 1)
		}
		return big.NewRat(0, 1)
	default:
		return new(big.Rat).SetFloat64(value.number)
	}
}

// PythonNumberCompare keeps integer/float/bool comparisons exact. NaN is
// unordered; strings and containers are not coerced into numbers.
func (value Value) PythonNumberCompare(other Value) (comparison int, ordered bool, err error) {
	if !numericKind(value.kind) || !numericKind(other.kind) {
		return 0, false, ErrNumericComparison
	}
	if (value.kind == Float && math.IsNaN(value.number)) || (other.kind == Float && math.IsNaN(other.number)) {
		return 0, false, nil
	}
	aInf, bInf := 0, 0
	if value.kind == Float && math.IsInf(value.number, 0) {
		if value.number > 0 {
			aInf = 1
		} else {
			aInf = -1
		}
	}
	if other.kind == Float && math.IsInf(other.number, 0) {
		if other.number > 0 {
			bInf = 1
		} else {
			bInf = -1
		}
	}
	if aInf != 0 || bInf != 0 {
		if aInf < bInf {
			return -1, true, nil
		}
		if aInf > bInf {
			return 1, true, nil
		}
		return 0, true, nil
	}
	return exactNumber(value).Cmp(exactNumber(other)), true, nil
}

// PythonEqual compares inert JSON data structurally. Numeric leaves follow
// Python, so 1, 1.0 and true compare equal; object insertion order is irrelevant.
func (value Value) PythonEqual(other Value) bool {
	if numericKind(value.kind) && numericKind(other.kind) {
		comparison, ordered, _ := value.PythonNumberCompare(other)
		return ordered && comparison == 0
	}
	if value.kind != other.kind {
		return false
	}
	switch value.kind {
	case Null:
		return true
	case Text:
		return value.text == other.text
	case Array:
		if len(value.items) != len(other.items) {
			return false
		}
		for index, item := range value.items {
			if !item.PythonEqual(other.items[index]) {
				return false
			}
		}
		return true
	case Object:
		if len(value.members) != len(other.members) {
			return false
		}
		for _, field := range value.members {
			matched, present := other.Lookup(field.name)
			if !present || !field.value.PythonEqual(matched) {
				return false
			}
		}
		return true
	}
	return false
}
