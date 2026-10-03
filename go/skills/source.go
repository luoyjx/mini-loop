package skills

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

var invalidUTF8 = errors.New("skill source is not valid UTF-8")

// boundedText retains a prefix and the actual character count. Trimmed fields
// discard leading and trailing Python whitespace without retaining an enormous
// line or skill body. Source hashing still consumes every normalized character.
type boundedText struct {
	limit           int
	trimmed         bool
	prefix          []rune
	count, trailing int
}

func (text *boundedText) add(r rune) {
	space := pytext.IsSpace(r)
	if text.trimmed && text.count == 0 && space {
		return
	}
	text.count++
	if len(text.prefix) < text.limit {
		text.prefix = append(text.prefix, r)
	}
	if space {
		text.trailing++
	} else {
		text.trailing = 0
	}
}
func (text boundedText) length() int {
	if text.trimmed {
		return text.count - text.trailing
	}
	return text.count
}
func (text boundedText) value() string {
	return string(text.prefix[:min(len(text.prefix), text.length())])
}

type metadataLine struct {
	length     int
	delimiter  []rune
	colon      bool
	key, value boundedText
}

func newMetadataLine() metadataLine {
	return metadataLine{key: boundedText{limit: 11, trimmed: true}, value: boundedText{limit: 2048, trimmed: true}}
}
func (line *metadataLine) add(r rune) {
	line.length++
	if len(line.delimiter) < 3 {
		line.delimiter = append(line.delimiter, r)
	}
	if !line.colon && r == ':' {
		line.colon = true
		return
	}
	if line.colon {
		line.value.add(r)
	} else {
		line.key.add(r)
	}
}

type sourceRecord struct {
	name               string
	namePresent        bool
	nameTruncated      bool
	description        string
	descriptionPresent bool
	descriptionLength  int
	body               string
	bodyLength         int
	digest             string
}

func (source *sourceRecord) acceptMetadata(line metadataLine) {
	if !line.colon || line.key.length() > 11 {
		return
	}
	switch line.key.value() {
	case "name":
		source.name, source.namePresent = line.value.value(), true
		source.nameTruncated = line.value.length() > len([]rune(source.name))
	case "description":
		source.description = string([]rune(line.value.value())[:min(MaxDescription, line.value.length())])
		source.descriptionPresent, source.descriptionLength = true, line.value.length()
	}
}

func metadataBreak(r rune) bool {
	return r == '\v' || r == '\f' || r >= 0x1c && r <= 0x1e || r == 0x85 || r == 0x2028 || r == 0x2029
}

// readSource follows Python read_text's strict UTF-8 and universal-newline
// semantics. It retains bounded prefixes, not the full source, while hashing
// all text. The frontmatter parser intentionally matches the small line-based
// Python parser rather than interpreting general YAML.
func readSource(ctx context.Context, path string) (sourceRecord, error) {
	handle, err := os.Open(path)
	if err != nil {
		return sourceRecord{}, err
	}
	defer handle.Close()
	return readSourceReader(ctx, handle)
}

func readSourceReader(ctx context.Context, source io.Reader) (sourceRecord, error) {
	reader := bufio.NewReader(source)
	digest := sha256.New()
	hashed := bufio.NewWriter(digest)
	full := boundedText{limit: MaxBody}
	body := boundedText{limit: MaxBody, trimmed: true}
	var result sourceRecord
	opening := true
	metadata, closed := false, false
	line := newMetadataLine()
	metaLines := 0
	skipLF := false
	count := 0
	for {
		if count%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return sourceRecord{}, err
			}
		}
		r, size, err := reader.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return sourceRecord{}, err
		}
		if r == utf8.RuneError && size == 1 {
			return sourceRecord{}, invalidUTF8
		}
		if skipLF && r == '\n' {
			skipLF = false
			continue
		}
		skipLF = r == '\r'
		if skipLF {
			r = '\n'
		}
		count++
		if _, err := hashed.WriteRune(r); err != nil {
			return sourceRecord{}, err
		}
		full.add(r)
		if closed {
			body.add(r)
			continue
		}
		if !opening && !metadata {
			continue
		}
		if r != '\n' {
			line.add(r)
			// Python splitlines accepts these separators in metadata, while
			// the frontmatter delimiters themselves require ASCII newlines.
			if metadata && metadataBreak(r) {
				result.acceptMetadata(line)
				reset := newMetadataLine()
				line.colon, line.key, line.value = reset.colon, reset.key, reset.value
			}
			continue
		}
		delimiter := line.length == 3 && string(line.delimiter) == "---"
		if opening {
			opening, metadata = false, delimiter
		} else if metaLines > 0 && delimiter {
			metadata, closed = false, true
		} else {
			result.acceptMetadata(line)
			metaLines++
		}
		line = newMetadataLine()
	}
	if err := ctx.Err(); err != nil {
		return sourceRecord{}, err
	}
	if err := hashed.Flush(); err != nil {
		return sourceRecord{}, err
	}
	if closed {
		result.body, result.bodyLength = body.value(), body.length()
	} else {
		result = sourceRecord{body: full.value(), bodyLength: full.length()}
	}
	result.digest = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}
