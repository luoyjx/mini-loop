// Package punycode implements raw RFC 3492 payloads, without an xn-- prefix.
//
// This implementation was independently written from the algorithms in
// https://www.rfc-editor.org/rfc/rfc3492#section-6, rather than copied from a
// third-party implementation. CPython 3.11.3's raw codec is a differential
// reference. Unlike that codec's unrestricted Python strings, this package
// accepts Unicode scalar strings only and bounds its quadratic work. IDNA
// validation, normalization, casing, and DNS length rules belong to its caller.
package punycode

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	// MaxCodePoints bounds both encoder input and decoder output. This is larger
	// than the caller's Source IDNA label bound of 254 code points.
	MaxCodePoints = 4096
	// MaxPayloadBytes bounds ASCII encoder output and decoder input; it is not
	// the DNS label length limit.
	MaxPayloadBytes = 32768

	base        uint64 = 36
	tmin        uint64 = 1
	tmax        uint64 = 26
	skew        uint64 = 38
	damp        uint64 = 700
	initialBias uint64 = 72
	initialN    uint64 = 128
	maxInteger  uint64 = ^uint64(0)
)

var (
	ErrInvalidUTF8    = errors.New("punycode: input is not valid UTF-8")
	ErrInvalidPayload = errors.New("punycode: malformed ASCII payload")
	ErrInvalidScalar  = errors.New("punycode: decoded value is not a Unicode scalar")
	ErrOverflow       = errors.New("punycode: integer overflow")
	ErrLimit          = errors.New("punycode: work limit exceeded")
)

// Encode returns the raw, lowercase-digit Punycode payload for text. ASCII
// characters, including case, punctuation, and controls, are copied literally;
// an all-ASCII nonempty input therefore ends in a delimiter. No IDNA policy is
// applied. text must be valid UTF-8 and contain at most MaxCodePoints scalars.
func Encode(text string) (string, error) {
	if len(text) > utf8.UTFMax*MaxCodePoints {
		return "", ErrLimit
	}
	if !utf8.ValidString(text) {
		return "", ErrInvalidUTF8
	}
	if utf8.RuneCountInString(text) > MaxCodePoints {
		return "", ErrLimit
	}
	points := []rune(text)
	output := make([]byte, 0, len(text)+1)
	for _, point := range points {
		if point < rune(initialN) {
			output = append(output, byte(point))
		}
	}
	basic := len(output)
	if basic != 0 {
		output = append(output, '-')
	}
	n, delta, bias := initialN, uint64(0), initialBias
	for handled := basic; handled < len(points); {
		minimum := uint64(utf8.MaxRune) + 1
		for _, point := range points {
			value := uint64(point)
			if value >= n && value < minimum {
				minimum = value
			}
		}
		var ok bool
		delta, ok = addProduct(delta, minimum-n, uint64(handled+1))
		if !ok {
			return "", ErrOverflow
		}
		n = minimum
		for _, point := range points {
			value := uint64(point)
			if value < n {
				if delta == maxInteger {
					return "", ErrOverflow
				}
				delta++
			}
			if value != n {
				continue
			}
			q := delta
			for k := base; ; k += base {
				t := threshold(k, bias)
				if q < t {
					break
				}
				if len(output) == MaxPayloadBytes {
					return "", ErrLimit
				}
				output = append(output, encodeDigit(t+(q-t)%(base-t)))
				q = (q - t) / (base - t)
				if k > maxInteger-base {
					return "", ErrOverflow
				}
			}
			if len(output) == MaxPayloadBytes {
				return "", ErrLimit
			}
			output = append(output, encodeDigit(q))
			bias = adapt(delta, uint64(handled+1), handled == basic)
			delta = 0
			handled++
		}
		if delta == maxInteger {
			return "", ErrOverflow
		}
		delta++
		n++ // At most MaxRune+1, after the final non-basic scalar.
	}
	return string(output), nil
}

// Decode decodes a raw ASCII Punycode payload. Extended digits are ASCII case
// insensitive; the basic prefix before the last delimiter retains its case.
// As in CPython's raw codec, empty input and a lone delimiter both decode to an
// empty string. Surrogates and values above U+10FFFF are rejected. Input and
// output are limited by MaxPayloadBytes and MaxCodePoints respectively.
func Decode(payload string) (string, error) {
	if len(payload) > MaxPayloadBytes {
		return "", ErrLimit
	}
	for i := 0; i < len(payload); i++ {
		if payload[i] >= utf8.RuneSelf {
			return "", ErrInvalidPayload
		}
	}
	var output []rune
	position := 0
	if split := strings.LastIndexByte(payload, '-'); split >= 0 {
		if split > MaxCodePoints {
			return "", ErrLimit
		}
		output = make([]rune, split)
		for i := range output {
			output[i] = rune(payload[i])
		}
		position = split + 1
	}
	n, index, bias := initialN, uint64(0), initialBias
	for position < len(payload) {
		if len(output) == MaxCodePoints {
			return "", ErrLimit
		}
		oldIndex, weight := index, uint64(1)
		for k := base; ; k += base {
			if position == len(payload) {
				return "", ErrInvalidPayload
			}
			digit, ok := decodeDigit(payload[position])
			if !ok {
				return "", ErrInvalidPayload
			}
			position++
			index, ok = addProduct(index, digit, weight)
			if !ok {
				return "", ErrOverflow
			}
			t := threshold(k, bias)
			if digit < t {
				break
			}
			if weight > maxInteger/(base-t) || k > maxInteger-base {
				return "", ErrOverflow
			}
			weight *= base - t
		}
		count := uint64(len(output) + 1)
		bias = adapt(index-oldIndex, count, oldIndex == 0)
		increment := index / count
		if increment > uint64(utf8.MaxRune)-n {
			return "", ErrInvalidScalar
		}
		n += increment
		if n >= 0xd800 && n <= 0xdfff {
			return "", ErrInvalidScalar
		}
		index %= count
		insertion := int(index)
		output = append(output, 0)
		copy(output[insertion+1:], output[insertion:])
		output[insertion] = rune(n)
		index++
	}
	return string(output), nil
}

func threshold(k, bias uint64) uint64 {
	if k <= bias {
		return tmin
	}
	if k-bias >= tmax {
		return tmax
	}
	return k - bias
}

func adapt(delta, count uint64, first bool) uint64 {
	if first {
		delta /= damp
	} else {
		delta /= 2
	}
	// count is positive. Division above ensures the sum cannot overflow, even
	// when count is one and the unscaled delta is maxInteger.
	delta += delta / count
	k := uint64(0)
	for delta > (base-tmin)*tmax/2 {
		delta /= base - tmin
		k += base
	}
	return k + (base-tmin+1)*delta/(delta+skew)
}

func addProduct(value, factor, weight uint64) (uint64, bool) {
	if weight != 0 && factor > (maxInteger-value)/weight {
		return 0, false
	}
	return value + factor*weight, true
}

func encodeDigit(digit uint64) byte {
	if digit < 26 {
		return byte('a' + digit)
	}
	return byte('0' + digit - 26)
}

func decodeDigit(digit byte) (uint64, bool) {
	switch {
	case digit >= 'a' && digit <= 'z':
		return uint64(digit - 'a'), true
	case digit >= 'A' && digit <= 'Z':
		return uint64(digit - 'A'), true
	case digit >= '0' && digit <= '9':
		return uint64(digit-'0') + 26, true
	default:
		return 0, false
	}
}
