package pytext

import (
	"math/big"
	"strings"
)

// Python Unicode 14.0.0 decimal blocks, generated with unicodedata.decimal.
// Pinning the table avoids accepting digit blocks added by a newer Go runtime.
var decimalZeros = [...]rune{
	0x30,
	0x660,
	0x6F0,
	0x7C0,
	0x966,
	0x9E6,
	0xA66,
	0xAE6,
	0xB66,
	0xBE6,
	0xC66,
	0xCE6,
	0xD66,
	0xDE6,
	0xE50,
	0xED0,
	0xF20,
	0x1040,
	0x1090,
	0x17E0,
	0x1810,
	0x1946,
	0x19D0,
	0x1A80,
	0x1A90,
	0x1B50,
	0x1BB0,
	0x1C40,
	0x1C50,
	0xA620,
	0xA8D0,
	0xA900,
	0xA9D0,
	0xA9F0,
	0xAA50,
	0xABF0,
	0xFF10,
	0x104A0,
	0x10D30,
	0x11066,
	0x110F0,
	0x11136,
	0x111D0,
	0x112F0,
	0x11450,
	0x114D0,
	0x11650,
	0x116C0,
	0x11730,
	0x118E0,
	0x11950,
	0x11C50,
	0x11D50,
	0x11DA0,
	0x16A60,
	0x16AC0,
	0x16B50,
	0x1D7CE,
	0x1D7D8,
	0x1D7E2,
	0x1D7EC,
	0x1D7F6,
	0x1E140,
	0x1E2F0,
	0x1E950,
	0x1FBF0,
}

// DecimalDigit uses Python's pinned Unicode decimal blocks.
func DecimalDigit(r rune) (byte, bool) {
	for _, zero := range decimalZeros {
		if r >= zero && r < zero+10 {
			return byte('0' + r - zero), true
		}
	}
	return 0, false
}

// Decimal parses a nonempty decimal-only counter, without signs or separators.
func Decimal(value string) (*big.Int, bool) {
	if value == "" {
		return nil, false
	}
	var normalized strings.Builder
	for _, r := range value {
		d, ok := DecimalDigit(r)
		if !ok {
			return nil, false
		}
		normalized.WriteByte(d)
	}
	return new(big.Int).SetString(normalized.String(), 10)
}

// Space matches Python str.split/strip, including the four ASCII separators.
func Space(r rune) bool {
	return r >= 0x09 && r <= 0x0d || r >= 0x1c && r <= 0x20 || r == 0x85 || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000
}
