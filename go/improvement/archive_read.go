package improvement

import (
	"context"
	"errors"
	"os"
	"strings"
	"unicode/utf8"
)

const MaxListed = 200

// Nil Limit selects the source default. Explicit zero/negative limits select one.
// Owner is supplied by the trusted caller; nil is an operator-wide query.
type ArchiveQuery struct {
	Owner *ArchiveOwnerID
	Limit *int
}

var ErrArchiveEncoding = errors.New("archive is not valid UTF-8")
var ErrArchiveOwnerShape = errors.New("owner-filtered archive row is not an object")

// List reads newest first, retains unknown historical data, skips malformed JSON
// lines and counts only accepted rows. Like source, IO errors mean an empty index;
// invalid UTF-8, integer limits, recursion and scoped non-object rows abort.
// It takes no append lock and applies no additional masking to historical rows.
func (a *Archive) List(ctx context.Context, query ArchiveQuery) ([]ArchiveValue, error) {
	if a == nil || a.newID == nil || a.now == nil {
		return nil, errors.New("archive is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit := MaxListed
	if query.Limit != nil {
		limit = *query.Limit
	}
	if limit < 1 {
		limit = 1
	}
	data, err := os.ReadFile(a.root + "/archive.jsonl")
	if err != nil {
		return []ArchiveValue{}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, ErrArchiveEncoding
	}
	lines := strings.FieldsFunc(string(data), func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == 0x1c || r == 0x1d || r == 0x1e || r == 0x85 || r == 0x2028 || r == 0x2029
	})
	rows := []ArchiveValue{}
	for i := len(lines) - 1; i >= 0 && len(rows) < limit; i-- {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := decodeArchiveValue(lines[i])
		if errors.Is(err, errArchiveSyntax) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if query.Owner != nil {
			if value.Kind() != ArchiveObject {
				return nil, ErrArchiveOwnerShape
			}
			owner, ok := value.Lookup("owner")
			text, textOK := owner.Text()
			if !ok || !textOK || text != string(*query.Owner) {
				continue
			}
		}
		rows = append(rows, value)
	}
	return rows, nil
}
