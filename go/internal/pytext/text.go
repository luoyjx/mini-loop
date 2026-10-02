package pytext

import (
	"fmt"
	"strings"
	"unicode"
)

// IsSpace includes the four information separators accepted by Python str.strip.
func IsSpace(r rune) bool       { return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f }
func Strip(value string) string { return strings.TrimFunc(value, IsSpace) }

// Repr matches Python's ordinary diagnostic quoting; skill identifiers themselves
// are restricted to ASCII, and never use this quoting as an authority boundary.
func Repr(value string) string {
	quote := '\''
	if strings.ContainsRune(value, '\'') && !strings.ContainsRune(value, '"') {
		quote = '"'
	}
	var out strings.Builder
	out.WriteRune(quote)
	for _, r := range value {
		switch {
		case r == quote || r == '\\':
			out.WriteRune('\\')
			out.WriteRune(r)
		case r == '\n':
			out.WriteString("\\n")
		case r == '\r':
			out.WriteString("\\r")
		case r == '\t':
			out.WriteString("\\t")
		case !unicode.IsPrint(r):
			if r <= 0xff {
				fmt.Fprintf(&out, "\\x%02x", r)
			} else if r <= 0xffff {
				fmt.Fprintf(&out, "\\u%04x", r)
			} else {
				fmt.Fprintf(&out, "\\U%08x", r)
			}
		default:
			out.WriteRune(r)
		}
	}
	out.WriteRune(quote)
	return out.String()
}
