package shell

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ArgumentEncodingError is the captured CPython UTF-8/surrogateescape failure
// for one selected argv entry. It retains positions and the offending code
// point, never the command, executable path or other argument text.
type ArgumentEncodingError struct {
	argument int
	start    int
	end      int
	point    rune
}

func (*ArgumentEncodingError) ExceptionClass() string        { return "UnicodeEncodeError" }
func (err *ArgumentEncodingError) ArgumentIndex() int        { return err.argument }
func (err *ArgumentEncodingError) EncodingRange() (int, int) { return err.start, err.end }

func (err *ArgumentEncodingError) Error() string {
	if err.end == err.start+1 {
		return fmt.Sprintf("'utf-8' codec can't encode character '\\u%04x' in position %d: surrogates not allowed", err.point, err.start)
	}
	return fmt.Sprintf("'utf-8' codec can't encode characters in position %d-%d: surrogates not allowed", err.start, err.end-1)
}

// SDK text uses UTF-8 with surrogatepass for retained lone-surrogate codepoints.
// Ordinary scalar UTF-8 and unrelated native OS bytes are left intact. This is
// not a general utf8.ValidString admission policy for native embedding callers.
func surrogatepassRune(value string) (rune, bool) {
	if len(value) < 3 || value[0] != 0xed || value[1] < 0xa0 || value[1] > 0xbf || value[2]&0xc0 != 0x80 {
		return 0, false
	}
	return rune(value[0]&15)<<12 | rune(value[1]&63)<<6 | rune(value[2]&63), true
}

func encodeFilesystemArgument(value string, argument int) (string, error) {
	if utf8.ValidString(value) {
		return value, nil
	}
	var encoded strings.Builder
	last, position := 0, 0
	for offset := 0; offset < len(value); position++ {
		point, surrogate := surrogatepassRune(value[offset:])
		if !surrogate {
			_, width := utf8.DecodeRuneInString(value[offset:])
			offset += width
			continue
		}
		if point < 0xdc80 || point > 0xdcff {
			// CPython's UTF-8 encoder reports the contiguous surrogate range;
			// surrogateescape may consume an encodable prefix before retrying.
			end, cursor := position+1, offset+3
			for cursor < len(value) {
				if _, next := surrogatepassRune(value[cursor:]); !next {
					break
				}
				end++
				cursor += 3
			}
			return "", &ArgumentEncodingError{argument: argument, start: position, end: end, point: point}
		}
		if last == 0 {
			encoded.Grow(len(value))
		}
		encoded.WriteString(value[last:offset])
		encoded.WriteByte(byte(point - 0xdc00))
		offset += 3
		last = offset
	}
	if last == 0 {
		return value, nil
	}
	encoded.WriteString(value[last:])
	return encoded.String(), nil
}

// Encode the complete sandbox-selected argv before any process can start. The
// operator's slice is borrowed; a changed argument creates a detached copy.
func encodeFilesystemArgv(argv []string) ([]string, error) {
	result := argv
	copied := false
	for index, value := range argv {
		encoded, err := encodeFilesystemArgument(value, index)
		if err != nil {
			return nil, err
		}
		if encoded != value {
			if !copied {
				result = append([]string(nil), argv...)
				copied = true
			}
			result[index] = encoded
		}
	}
	return result, nil
}
