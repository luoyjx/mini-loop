package workspace

import (
	"bufio"
	"fmt"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"io"
)

type utf8Issue string

const (
	validUTF8           utf8Issue = ""
	invalidStart        utf8Issue = "invalid start byte"
	invalidContinuation utf8Issue = "invalid continuation byte"
	unexpectedEnd       utf8Issue = "unexpected end of data"
)

// Decode through the shared Python UTF-8 replacement contract.
func decodeRune(data []byte) (rune, int, utf8Issue) {
	r, size, issue := pytext.DecodeRune(data)
	return r, size, utf8Issue(issue)
}

type textReader struct{ reader *bufio.Reader }

func (reader *textReader) readRune() (rune, error) {
	return reader.readTextRune(false)
}

func (reader *textReader) readStrictRune() (rune, error) {
	return reader.readTextRune(true)
}

func (reader *textReader) readTextRune(strict bool) (rune, error) {
	data, err := reader.reader.Peek(4)
	if len(data) == 0 {
		return 0, err
	}
	if err != nil && err != io.EOF {
		return 0, err
	}
	r, size, issue := decodeRune(data)
	if strict && issue != validUTF8 {
		return 0, fmt.Errorf("invalid UTF-8: %s", issue)
	}
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
