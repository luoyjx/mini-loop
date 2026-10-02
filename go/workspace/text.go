package workspace

import (
	"bufio"
	"fmt"
	"io"
	"unicode/utf8"
)

type utf8Issue string

const (
	validUTF8           utf8Issue = ""
	invalidStart        utf8Issue = "invalid start byte"
	invalidContinuation utf8Issue = "invalid continuation byte"
	unexpectedEnd       utf8Issue = "unexpected end of data"
)

// decodeRune implements maximal-subpart replacement, including one replacement
// for a truncated multibyte prefix. Go's ReadRune replaces each byte instead.
func decodeRune(data []byte) (rune, int, utf8Issue) {
	first := data[0]
	if first < utf8.RuneSelf {
		return rune(first), 1, validUTF8
	}
	length := 0
	switch {
	case first >= 0xc2 && first <= 0xdf:
		length = 2
	case first >= 0xe0 && first <= 0xef:
		length = 3
	case first >= 0xf0 && first <= 0xf4:
		length = 4
	default:
		return utf8.RuneError, 1, invalidStart
	}
	for i := 1; i < length; i++ {
		if i >= len(data) {
			return utf8.RuneError, i, unexpectedEnd
		}
		low, high := byte(0x80), byte(0xbf)
		if i == 1 {
			switch first {
			case 0xe0:
				low = 0xa0
			case 0xed:
				high = 0x9f
			case 0xf0:
				low = 0x90
			case 0xf4:
				high = 0x8f
			}
		}
		if data[i] < low || data[i] > high {
			return utf8.RuneError, i, invalidContinuation
		}
	}
	r, size := utf8.DecodeRune(data[:length])
	return r, size, validUTF8
}

type textReader struct{ reader *bufio.Reader }

func (reader *textReader) readRune() (rune, error) {
	data, err := reader.reader.Peek(4)
	if len(data) == 0 {
		return 0, err
	}
	if err != nil && err != io.EOF {
		return 0, err
	}
	r, size, _ := decodeRune(data)
	_, _ = reader.reader.Discard(size)
	if r == '\r' {
		if next, _ := reader.reader.Peek(1); len(next) > 0 && next[0] == '\n' {
			_, _ = reader.reader.Discard(1)
		}
		r = '\n'
	}
	return r, nil
}

func validateUTF8(data []byte) error {
	for offset := 0; offset < len(data); {
		_, size, issue := decodeRune(data[offset:])
		if issue != validUTF8 {
			if size == 1 {
				return fmt.Errorf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", data[offset], offset, issue)
			}
			return fmt.Errorf("'utf-8' codec can't decode bytes in position %d-%d: %s", offset, offset+size-1, issue)
		}
		offset += size
	}
	return nil
}
