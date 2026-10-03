package skills

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"strings"
)

//go:embed builtin/code_review/SKILL.md
var builtinCodeReview string

// NewBuiltinCatalog is immutable compiled data. The checked-in source asset is
// pinned against the Python deployment catalogue by differential tests.
func NewBuiltinCatalog(ctx context.Context) (*Catalog, error) {
	source, err := readSourceReader(ctx, strings.NewReader(builtinCodeReview))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(source.body))
	entry := Entry{source.name, source.description, source.body, "<builtin>/code_review/SKILL.md", hex.EncodeToString(digest[:]), source.digest}
	catalog := EmptyCatalog()
	catalog.builtin = true
	catalog.ordered = []Entry{entry}
	catalog.entries[entry.Name] = entry
	return catalog, nil
}
