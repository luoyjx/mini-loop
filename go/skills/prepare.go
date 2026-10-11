package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	ErrPreparedSource  = errors.New("prepared skill source is invalid")
	ErrSkillExists     = errors.New("a skill with this name already exists")
	ErrSkillPathExists = errors.New("a skill already uses this source path")
)

// WithSourceDocument prepares a detached catalogue before an operator commits
// its file. It performs no filesystem operations and grants no publication or
// owner authority. Callers must validate canonical content, screen secrets and
// bind the trusted path separately. Serving still verifies the full source digest.
func (catalog *Catalog) WithSourceDocument(ctx context.Context, path, document string) (*Catalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Bound raw bytes as well as the parser's normalized character counts.
	const maxDocumentBytes = 4 * (MaxBody + MaxDescription + 64 + 512)
	if catalog == nil || catalog.builtin || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "SKILL.md" || strings.ContainsRune(path, 0) || !utf8.ValidString(path) || len(document) > maxDocumentBytes {
		return nil, ErrPreparedSource
	}
	source, err := readSourceReader(ctx, strings.NewReader(document))
	if err != nil {
		return nil, err
	}
	if !source.namePresent || source.nameTruncated || !validName(source.name) || !source.descriptionPresent || source.description == "" || source.descriptionLength > MaxDescription || source.bodyLength == 0 || source.bodyLength > MaxBody {
		return nil, ErrPreparedSource
	}
	if _, exists := catalog.entries[source.name]; exists {
		return nil, ErrSkillExists
	}
	for _, entry := range catalog.ordered {
		if entry.Path == path {
			return nil, ErrSkillPathExists
		}
	}
	bodyDigest := sha256.Sum256([]byte(source.body))
	entry := Entry{source.name, source.description, source.body, path, hex.EncodeToString(bodyDigest[:]), source.digest}
	prepared := EmptyCatalog()
	prepared.ordered = append(prepared.ordered, catalog.ordered...)
	prepared.ordered = append(prepared.ordered, entry)
	sort.Slice(prepared.ordered, func(i, j int) bool { return prepared.ordered[i].Path < prepared.ordered[j].Path })
	for _, item := range prepared.ordered {
		prepared.entries[item.Name] = item
	}
	catalog.mu.Lock()
	prepared.problems = append(prepared.problems, catalog.problems...)
	prepared.problemTotal, prepared.problemDropped = catalog.problemTotal, catalog.problemDropped
	catalog.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return prepared, nil
}
