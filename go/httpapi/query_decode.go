package httpapi

import (
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// These are the scalar observations read by the owned HTTP handlers. Unknown
// parameters do not need retained maps, slices or decoded values.
type scalarQueryParameters struct {
	AccessToken scalarQueryValue
	Limit       scalarQueryValue
	Envelope    scalarQueryValue
	SessionID   scalarQueryValue
	Format      scalarQueryValue
	Epoch       scalarQueryValue
}

func parseScalarQuery(raw string) scalarQueryParameters {
	var query scalarQueryParameters
	if raw == "" {
		return query
	}
	for raw != "" {
		var pair string
		pair, raw, _ = strings.Cut(raw, "&")
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		selected := scalarQueryValue{Text: value, Present: true}
		switch decodeQueryComponent(key) {
		case "access_token":
			query.AccessToken = selected
		case "limit":
			query.Limit = selected
		case "envelope":
			query.Envelope = selected
		case "session_id":
			query.SessionID = selected
		case "format":
			query.Format = selected
		case "epoch":
			query.Epoch = selected
		}
	}
	// Decode only the final values after selection. Decoding is total and has no
	// validation or effects; malformed escapes remain literal text.
	query.AccessToken = query.AccessToken.decoded()
	query.Limit = query.Limit.decoded()
	query.Envelope = query.Envelope.decoded()
	query.SessionID = query.SessionID.decoded()
	query.Format = query.Format.decoded()
	query.Epoch = query.Epoch.decoded()
	return query
}

func (value scalarQueryValue) decoded() scalarQueryValue {
	if value.Present {
		value.Text = decodeQueryComponent(value.Text)
	}
	return value
}

func queryHex(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

// Starlette first decodes ASGI bytes as Latin-1. CPython unquote then decodes
// percent-unescaped ASCII spans as UTF-8 with maximal-subpart replacement;
// literal non-ASCII characters delimit those spans and retain Latin-1 values.
func decodeQueryComponent(raw string) string {
	needsDecode := false
	for index := 0; index < len(raw); index++ {
		if raw[index] == '+' || raw[index] == '%' || raw[index] >= utf8.RuneSelf {
			needsDecode = true
			break
		}
	}
	if !needsDecode {
		return raw
	}
	var result strings.Builder
	result.Grow(len(raw))
	var scratch [128]byte
	for index := 0; index < len(raw); {
		if raw[index] >= utf8.RuneSelf {
			result.WriteRune(rune(raw[index]))
			index++
			continue
		}
		end := index
		for end < len(raw) && raw[end] < utf8.RuneSelf {
			end++
		}
		span := raw[index:end]
		if !strings.Contains(span, "%") {
			for offset := 0; offset < len(span); offset++ {
				value := span[offset]
				if value == '+' {
					value = ' '
				}
				result.WriteByte(value)
			}
		} else {
			octets := scratch[:0]
			for offset := 0; offset < len(span); offset++ {
				value := span[offset]
				if value == '%' && offset+2 < len(span) {
					high, highOK := queryHex(span[offset+1])
					low, lowOK := queryHex(span[offset+2])
					if highOK && lowOK {
						value = high<<4 | low
						offset += 2
					}
				} else if value == '+' {
					value = ' '
				}
				octets = append(octets, value)
			}
			for len(octets) > 0 {
				value, size, _ := pytext.DecodeRune(octets)
				result.WriteRune(value)
				octets = octets[size:]
			}
		}
		index = end
	}
	return result.String()
}
