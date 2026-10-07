package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

var ErrMetric = errors.New("benchmark metric must be a finite JSON number or boolean")

const integerDigitLimit = 4300

type numberKind uint8

const (
	integerNumber numberKind = iota
	floatingNumber
	booleanNumber
)

// Number is an immutable closed union: exact integer, finite double or boolean.
// Python accepts bool as a numeric measurement. Nil pointers express absence.
// The zero value is integer zero. Arbitrary Go values cannot enter this type.
type Number struct {
	kind    numberKind
	integer *big.Int
	decimal float64
	boolean bool
}

func Integer(value int64) Number { return BigInteger(big.NewInt(value)) }
func BigInteger(value *big.Int) Number {
	if value == nil {
		return Number{}
	}
	return Number{integer: new(big.Int).Set(value)}
}
func Boolean(value bool) Number { return Number{kind: booleanNumber, boolean: value} }
func Float(value float64) (Number, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return Number{}, ErrMetric
	}
	return Number{kind: floatingNumber, decimal: value}, nil
}
func (n Number) exactInteger() *big.Int {
	if n.kind == booleanNumber {
		if n.boolean {
			return big.NewInt(1)
		}
		return new(big.Int)
	}
	if n.integer == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(n.integer)
}
func (n Number) Int64() (int64, bool) {
	if n.kind != integerNumber {
		return 0, false
	}
	integer := n.exactInteger()
	return integer.Int64(), integer.IsInt64()
}
func (n Number) BigInt() (*big.Int, bool) { return n.exactInteger(), n.kind == integerNumber }
func (n Number) Bool() (bool, bool)       { return n.boolean, n.kind == booleanNumber }
func (n Number) Float64() float64 {
	if n.kind == floatingNumber {
		return n.decimal
	}
	f, _ := new(big.Float).SetInt(n.exactInteger()).Float64()
	return f
}
func (n Number) MarshalJSON() ([]byte, error) {
	switch n.kind {
	case integerNumber:
		text := n.exactInteger().String()
		if len(strings.TrimPrefix(text, "-")) > integerDigitLimit {
			return nil, ErrMetric
		}
		return []byte(text), nil
	case booleanNumber:
		return []byte(strconv.FormatBool(n.boolean)), nil
	case floatingNumber:
		if math.IsNaN(n.decimal) || math.IsInf(n.decimal, 0) {
			return nil, ErrMetric
		}
		return []byte(floatText(n.decimal)), nil
	default:
		return nil, ErrMetric
	}
}
func (n *Number) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if !json.Valid(raw) {
		return ErrMetric
	}
	var value Number
	switch {
	case bytes.Equal(raw, []byte("true")):
		value = Boolean(true)
	case bytes.Equal(raw, []byte("false")):
		value = Boolean(false)
	case bytes.ContainsAny(raw, ".eE"):
		f, err := strconv.ParseFloat(string(raw), 64)
		if err != nil {
			return ErrMetric
		}
		value, err = Float(f)
		if err != nil {
			return err
		}
	default:
		if len(strings.TrimPrefix(string(raw), "-")) > integerDigitLimit {
			return ErrMetric
		}
		integer, ok := new(big.Int).SetString(string(raw), 10)
		if !ok {
			return ErrMetric
		}
		value = BigInteger(integer)
	}
	*n = value
	return nil
}
func floatText(value float64) string {
	if a := math.Abs(value); a != 0 && (a < 1e-4 || a >= 1e16) {
		return strconv.FormatFloat(value, 'e', -1, 64)
	}
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(text, ".") {
		text += ".0"
	}
	return text
}
func (n Number) rational() *big.Rat {
	if n.kind != floatingNumber {
		return new(big.Rat).SetInt(n.exactInteger())
	}
	return new(big.Rat).SetFloat64(n.decimal)
}
func add(a, b Number) (Number, error) {
	if a.kind == floatingNumber || b.kind == floatingNumber {
		return Float(a.Float64() + b.Float64())
	}
	return BigInteger(new(big.Int).Add(a.exactInteger(), b.exactInteger())), nil
}
func midpoint(a, b Number) (Number, error) {
	if a.kind == floatingNumber || b.kind == floatingNumber {
		return Float((a.Float64() + b.Float64()) / 2)
	}
	sum := new(big.Int).Add(a.exactInteger(), b.exactInteger())
	f, _ := new(big.Rat).SetFrac(sum, big.NewInt(2)).Float64()
	return Float(f)
}

// Decimal rounding uses the exact IEEE-754 value and ties to even. Rounding
// x*10^digits first would discard information at Python's decimal boundaries.
func roundDecimal(value float64, digits int) float64 {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	x := new(big.Rat).SetFloat64(value)
	x.Mul(x, new(big.Rat).SetInt(scale))
	sign := x.Sign()
	x.Abs(x)
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(x.Num(), x.Denom(), rem)
	cmp := new(big.Int).Lsh(rem, 1).Cmp(x.Denom())
	if cmp > 0 || (cmp == 0 && q.Bit(0) == 1) {
		q.Add(q, big.NewInt(1))
	}
	if sign < 0 {
		q.Neg(q)
	}
	f, _ := new(big.Rat).SetFrac(q, scale).Float64()
	if f == 0 {
		return math.Copysign(0, value)
	}
	return f
}
