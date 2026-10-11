package jsonvalue

import (
	"errors"
	"math"
	"math/big"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

var ErrIntegerConversion = errors.New("archive value cannot be converted to a Python integer")

// PythonInteger implements int(value), including pinned Unicode decimal text,
// underscores, binary-float truncation and the source decimal digit limit. The
// caller owns the returned integer; no mutable big.Int enters a stored Value.
func (v Value) PythonInteger() (*big.Int, error) {
	switch v.Kind() {
	case Integer:
		n, ok := new(big.Int).SetString(v.text, 10)
		if ok {
			return n, nil
		}
	case Boolean:
		if v.boolean {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	case Float:
		if math.IsNaN(v.number) || math.IsInf(v.number, 0) {
			return nil, ErrIntegerConversion
		}
		n, _ := new(big.Float).SetFloat64(v.number).Int(nil)
		return n, nil
	case Text:
		text := strings.TrimFunc(v.text, func(r rune) bool { return pytext.Space(r) && !(r >= 0x1c && r <= 0x1f) })
		negative := false
		if strings.HasPrefix(text, "+") || strings.HasPrefix(text, "-") {
			negative = text[0] == '-'
			text = text[1:]
		}
		var digits strings.Builder
		previousDigit := false
		for _, r := range text {
			if r == '_' {
				if !previousDigit {
					return nil, ErrIntegerConversion
				}
				previousDigit = false
				continue
			}
			digit, ok := pytext.DecimalDigit(r)
			if !ok {
				return nil, ErrIntegerConversion
			}
			digits.WriteByte(digit)
			previousDigit = true
		}
		if !previousDigit {
			return nil, ErrIntegerConversion
		}
		if digits.Len() > integerDigitLimit {
			return nil, ErrInteger
		}
		n, ok := new(big.Int).SetString(digits.String(), 10)
		if ok {
			if negative {
				n.Neg(n)
			}
			return n, nil
		}
	}
	return nil, ErrIntegerConversion
}
