package userresources

import (
	"strconv"
	"strings"
)

// Python's JSON decoder accepts nonfinite numeric constants; candidate validation
// treats every float as an invalid field/index. Lowering these constants to null
// preserves those outcomes without admitting a nonfinite native domain value.
// Python's default integer digit ceiling is pinned by the source contract corpus.
const candidateIntegerDigits = 4300

func candidateJSONLexemes(raw string) ([]byte, bool) {
	var out strings.Builder
	for i := 0; i < len(raw); {
		if raw[i] == '"' {
			start := i
			i++
			for i < len(raw) && raw[i] != '"' {
				if raw[i] == '\\' {
					i++
					if i >= len(raw) {
						return nil, false
					}
					if raw[i] == 'u' {
						if i+4 >= len(raw) {
							return nil, false
						}
						value, err := strconv.ParseUint(raw[i+1:i+5], 16, 16)
						if err != nil {
							return nil, false
						}
						i += 4
						if value >= 0xd800 && value <= 0xdbff {
							if i+6 >= len(raw) || raw[i+1:i+3] != "\\u" {
								return nil, false
							}
							next, err := strconv.ParseUint(raw[i+3:i+7], 16, 16)
							if err != nil || next < 0xdc00 || next > 0xdfff {
								return nil, false
							}
							i += 6
						} else if value >= 0xdc00 && value <= 0xdfff {
							return nil, false
						}
					}
				}
				i++
			}
			if i >= len(raw) {
				return nil, false
			}
			i++
			out.WriteString(raw[start:i])
			continue
		}
		if strings.ContainsRune(" \t\r\n{}[]:,", rune(raw[i])) {
			out.WriteByte(raw[i])
			i++
			continue
		}
		start := i
		for i < len(raw) && !strings.ContainsRune(" \t\r\n{}[]:,\"", rune(raw[i])) {
			i++
		}
		word := raw[start:i]
		if word == "NaN" || word == "Infinity" || word == "-Infinity" {
			out.WriteString("null")
			continue
		}
		if digits := strings.TrimPrefix(word, "-"); len(digits) > candidateIntegerDigits && !strings.ContainsAny(digits, ".eE") {
			integer := true
			for _, value := range digits {
				if value < '0' || value > '9' {
					integer = false
					break
				}
			}
			if integer {
				return nil, false
			}
		}
		out.WriteString(word)
	}
	return []byte(out.String()), true
}
