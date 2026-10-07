package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/problems"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type parsedFile struct {
	modified, size int64
	record         Record
	readable       bool
}

// Store owns a serialized process-local cache. File transactions and replacement
// of multiple memories are not cross-process transactions.
type Store struct {
	root               string
	masker             Masker
	permit             chan struct{}
	lifecycle          chan struct{}
	dirty              bool
	parsed             map[string]parsedFile
	problems           []Problem
	problemOccurrences problems.Log
}

func NewStore(ctx context.Context, root string, masker Masker) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := workspace.ResolvePath(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(resolved, 0700); err != nil {
		return nil, err
	}
	store := &Store{root: resolved, masker: masker, permit: make(chan struct{}, 1), lifecycle: make(chan struct{}, 1), parsed: make(map[string]parsedFile)}
	store.permit <- struct{}{}
	store.lifecycle <- struct{}{}
	return store, nil
}
func (store *Store) acquire(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-store.permit:
	}
	if err := ctx.Err(); err != nil {
		store.release()
		return err
	}
	return nil
}
func (store *Store) release() { store.permit <- struct{}{} }
func (store *Store) report(message string) {
	appendDiagnostic(&store.problemOccurrences, message)
	for i := range store.problems {
		if store.problems[i].Message == message {
			store.problems[i].Count++
			return
		}
	}
	if len(store.problems) == MaxProblems {
		store.problems = store.problems[1:]
	}
	store.problems = append(store.problems, Problem{message, 1})
}
func (store *Store) Problems(ctx context.Context) ([]Problem, error) {
	if err := store.acquire(ctx); err != nil {
		return nil, err
	}
	defer store.release()
	return append([]Problem{}, store.problems...), nil
}
func (store *Store) mask(text string) string {
	if store.masker != nil {
		return store.masker.MaskText(text)
	}
	return text
}
func atomicText(ctx context.Context, path, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".memory-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.WriteString(text); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return err
	}
	// Match the source's best-effort directory fsync after atomic replacement.
	if directory, err := os.Open(filepath.Dir(path)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
func (store *Store) Write(ctx context.Context, owner OwnerID, input Input) (string, error) {
	if err := store.acquire(ctx); err != nil {
		return "", err
	}
	defer store.release()
	return store.write(ctx, owner, input, nil)
}
func (store *Store) write(ctx context.Context, owner OwnerID, input Input, attributed *problems.Log) (string, error) {
	for _, field := range []string{input.Name, input.Description, input.Body, string(owner), string(input.Type), string(input.Origin)} {
		if !utf8.ValidString(field) {
			return "", errors.New("memory fields must be valid UTF-8")
		}
	}
	kind := input.Type
	switch kind {
	case User, Feedback, Project, Reference:
	default:
		kind = Project
	}
	name := header(input.Name)
	if name == "" {
		name = "memory"
	}
	description := header(input.Description)
	ownerDisplay := header(string(owner))
	if ownerDisplay == "" {
		ownerDisplay = "anonymous"
	}
	normalized := slug(name)
	body := input.Body
	runes := []rune(body)
	if len(runes) > MaxBody {
		message := fmt.Sprintf("%s: body truncated from %s to 32,000", normalized, grouped(len(runes)))
		store.report(message)
		if attributed != nil {
			appendDiagnostic(attributed, message)
		}
		body = string(runes[:MaxBody]) + "\n[memory truncated]"
	}
	text := fmt.Sprintf("---\nname: %s\ndescription: %s\ntype: %s\nscope: user\nowner_key: %s\nowner: %s\norigin: %s\n---\n\n%s\n", name, description, kind, hash(string(owner)), ownerDisplay, origin(input.Origin, Explicit), body)
	filename := normalized + ".md"
	if owner != "anonymous" {
		filename = "u-" + memoryKey(owner, name) + "-" + normalized + ".md"
	}
	delete(store.parsed, filename)
	if err := atomicText(ctx, filepath.Join(store.root, filename), store.mask(text)); err != nil {
		return "", err
	}
	candidates := []string{normalized + ".md"}
	if strings.HasPrefix(normalized, "memory-") {
		candidates = append(candidates, "memory.md")
	}
	for _, legacy := range candidates {
		if legacy == filename {
			continue
		}
		if _, err := os.Stat(filepath.Join(store.root, legacy)); os.IsNotExist(err) {
			continue
		}
		prior, ok, err := store.parse(ctx, legacy)
		if err != nil {
			return "", err
		}
		if ok && belongs(prior, owner) && prior.Name == name {
			if err := os.Remove(filepath.Join(store.root, legacy)); err != nil && !os.IsNotExist(err) {
				return "", err
			}
			delete(store.parsed, legacy)
		}
	}
	store.dirty = true
	return fmt.Sprintf("Remembered '%s' (%s)", name, kind), nil
}
func (store *Store) parse(ctx context.Context, name string) (Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, false, err
	}
	path := filepath.Join(store.root, name)
	stat, statErr := os.Stat(path)
	if statErr == nil {
		if cached, ok := store.parsed[name]; ok && cached.modified == stat.ModTime().UnixNano() && cached.size == stat.Size() {
			return cached.record, cached.readable, nil
		}
	}
	data, err := os.ReadFile(path)
	readable := err == nil && utf8.Valid(data)
	var record Record
	if readable {
		record = parseRecord(name, strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"))
	} else {
		kind := "OSError"
		if err == nil {
			kind = "UnicodeDecodeError"
		} else if os.IsNotExist(err) {
			kind = "FileNotFoundError"
		} else if statErr == nil && stat.IsDir() {
			kind = "IsADirectoryError"
		} else if os.IsPermission(err) {
			kind = "PermissionError"
		}
		store.report("unreadable memory " + name + ": " + kind)
	}
	if statErr == nil {
		store.parsed[name] = parsedFile{stat.ModTime().UnixNano(), stat.Size(), record, readable}
	}
	return record, readable, nil
}
func (store *Store) files() ([]string, error) {
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".md") && entry.Name() != "MEMORY.md" {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}
func (store *Store) list(ctx context.Context, owner *OwnerID) ([]Record, error) {
	names, err := store.files()
	if err != nil {
		return nil, err
	}
	items := []Record{}
	for _, name := range names {
		record, ok, err := store.parse(ctx, name)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if strings.ToLower(name) == "memory.md" && strings.HasPrefix(record.Body, "# Memory index") {
			continue
		}
		if owner != nil {
			if !belongs(record, *owner) {
				continue
			}
			if record.OwnerKey != "" {
				record.Owner = string(*owner)
			}
		}
		items = append(items, record)
	}
	return items, nil
}
func (store *Store) List(ctx context.Context, owner *OwnerID) ([]Record, error) {
	if err := store.acquire(ctx); err != nil {
		return nil, err
	}
	defer store.release()
	return store.list(ctx, owner)
}
func (store *Store) rebuild(ctx context.Context) error {
	items, err := store.list(ctx, nil)
	if err != nil {
		return err
	}
	lines := []string{"# Memory index\n"}
	for _, m := range items {
		lines = append(lines, fmt.Sprintf("- [%s](%s) — %s", m.Name, m.File, m.Description))
	}
	return atomicText(ctx, filepath.Join(store.root, "MEMORY.md"), store.mask(strings.Join(lines, "\n")+"\n"))
}
func (store *Store) flush(ctx context.Context) error {
	if !store.dirty {
		return nil
	}
	if err := store.rebuild(ctx); err != nil {
		return err
	}
	store.dirty = false
	return nil
}
func (store *Store) Flush(ctx context.Context) error {
	if err := store.acquire(ctx); err != nil {
		return err
	}
	defer store.release()
	return store.flush(ctx)
}
func (store *Store) Index(ctx context.Context, owner *OwnerID) (string, error) {
	if err := store.acquire(ctx); err != nil {
		return "", err
	}
	defer store.release()
	if err := store.flush(ctx); err != nil {
		return "", err
	}
	items, err := store.list(ctx, owner)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "(no memories yet)", nil
	}
	var lines []string
	for _, m := range items {
		lines = append(lines, fmt.Sprintf("  - %s [%s]: %s", m.Name, m.Type, m.Description))
	}
	rendered := strings.Join(lines, "\n")
	runes := []rune(rendered)
	if len(runes) > MaxIndex {
		kept := string(runes[:MaxIndex])
		if i := strings.LastIndex(kept, "\n"); i >= 0 {
			kept = kept[:i]
		}
		rendered = fmt.Sprintf("%s\n  ... %d memories total; index truncated. Use `recall` with a query to search the rest.", kept, len(items))
	}
	return rendered, nil
}
func (store *Store) Search(ctx context.Context, owner *OwnerID, query string, limit int) ([]Record, error) {
	if err := store.acquire(ctx); err != nil {
		return nil, err
	}
	defer store.release()
	if err := store.flush(ctx); err != nil {
		return nil, err
	}
	items, err := store.list(ctx, owner)
	if err != nil {
		return nil, err
	}
	if query != "" {
		type scored struct {
			record Record
			score  int
		}
		var scores []scored
		words := terms(query)
		for _, m := range items {
			haystack := pytext.Lower(m.Name + " " + m.Description + " " + m.Body)
			score := 0
			for _, term := range words {
				score += strings.Count(haystack, term)
			}
			if score > 0 {
				scores = append(scores, scored{m, score})
			}
		}
		sort.SliceStable(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
		items = []Record{}
		for _, s := range scores {
			items = append(items, s.record)
		}
	}
	if limit < 0 {
		limit = len(items) + limit
		if limit < 0 {
			limit = 0
		}
	}
	if limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}
func (store *Store) ReplaceAll(ctx context.Context, owner *OwnerID, memories []Input, defaultOrigin Origin) error {
	return store.replaceAll(ctx, owner, memories, defaultOrigin, nil)
}
func (store *Store) replaceAll(ctx context.Context, owner *OwnerID, memories []Input, defaultOrigin Origin, attributed *problems.Log) error {
	if err := store.acquire(ctx); err != nil {
		return err
	}
	defer store.release()
	names, err := store.files()
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		remove := owner == nil
		if owner != nil {
			m, ok, err := store.parse(ctx, name)
			if err != nil {
				return err
			}
			remove = ok && belongs(m, *owner)
		}
		if remove {
			if err := os.Remove(filepath.Join(store.root, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
			delete(store.parsed, name)
		}
	}
	replacementOwner := OwnerID("anonymous")
	if owner != nil {
		replacementOwner = *owner
	}
	for _, input := range memories {
		input.Origin = origin(input.Origin, origin(defaultOrigin, Imported))
		if _, err := store.write(ctx, replacementOwner, input, attributed); err != nil {
			return err
		}
	}
	if err := store.rebuild(ctx); err != nil {
		return err
	}
	store.dirty = false
	return nil
}
