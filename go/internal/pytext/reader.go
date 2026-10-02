package pytext

import (
	"bufio"
	"io"
	"unicode/utf8"
)

type UTF8Issue string

const (
	ValidUTF8           UTF8Issue = ""
	InvalidStart        UTF8Issue = "invalid start byte"
	InvalidContinuation UTF8Issue = "invalid continuation byte"
	UnexpectedEnd       UTF8Issue = "unexpected end of data"
)

// decodeRune implements maximal-subpart replacement, including one replacement
// for a truncated multibyte prefix. Go's ReadRune replaces each byte instead.
func DecodeRune(data []byte) (rune, int, UTF8Issue) {
	first := data[0]
	if first < utf8.RuneSelf {
		return rune(first), 1, ValidUTF8
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
		return utf8.RuneError, 1, InvalidStart
	}
	for i := 1; i < length; i++ {
		if i >= len(data) {
			return utf8.RuneError, i, UnexpectedEnd
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
			return utf8.RuneError, i, InvalidContinuation
		}
	}
	r, size := utf8.DecodeRune(data[:length])
	return r, size, ValidUTF8
}

// Reader follows Python's UTF-8 errors=replace and universal newline decoding.
// Its fixed lookahead bounds memory even when a producer never emits a newline.
type Reader struct{ reader *bufio.Reader }

func NewReader(reader io.Reader) *Reader { return &Reader{bufio.NewReader(reader)} }
func (reader *Reader) ReadTextRune() (rune, error) {
	data, err := reader.reader.Peek(4)
	if len(data) == 0 {
		return 0, err
	}
	if err != nil && err != io.EOF {
		return 0, err
	}
	r, size, _ := DecodeRune(data)
	_, _ = reader.reader.Discard(size)
	if r == '\r' {
		if next, _ := reader.reader.Peek(1); len(next) > 0 && next[0] == '\n' {
			_, _ = reader.reader.Discard(1)
		}
		r = '\n'
	}
	return r, nil
}
