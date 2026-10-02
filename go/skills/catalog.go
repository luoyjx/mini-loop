package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

const MaxBody = 50_000
const MaxDescription = 200
const MaxCatalogue = 8_000
const MaxProblems = 50

type ProblemKind string

const (
	ProblemEscape      ProblemKind = "path_escape"
	ProblemUnreadable  ProblemKind = "unreadable"
	ProblemName        ProblemKind = "invalid_name"
	ProblemDuplicate   ProblemKind = "duplicate_name"
	ProblemBody        ProblemKind = "body_truncated"
	ProblemDescription ProblemKind = "description_truncated"
	ProblemCatalogue   ProblemKind = "catalogue_omitted"
	ProblemChanged     ProblemKind = "source_changed"
)

type Problem struct {
	Kind    ProblemKind
	Message string
	Count   int
}

type Entry struct {
	Name, Description, Body, Path string
	Digest, SourceDigest          string
}

type Catalog struct {
	ordered                      []Entry
	entries                      map[string]Entry
	mu                           sync.Mutex
	problems                     []Problem
	problemTotal, problemDropped uint64
}

func EmptyCatalog() *Catalog { return &Catalog{entries: make(map[string]Entry)} }

func NewCatalog(ctx context.Context, directory string) (*Catalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if directory == "" {
		return nil, errors.New("skills directory is required")
	}
	catalog := EmptyCatalog()
	if info, err := os.Stat(directory); os.IsNotExist(err) || err == nil && !info.IsDir() {
		return catalog, nil
	}
	root, err := workspace.ResolvePath(directory)
	if err != nil {
		return nil, err
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Python rglob suppresses directory enumeration errors and does not
		// follow directory symlinks. Individual discovered files report errors.
		if walkErr != nil {
			return nil
		}
		if entry.Name() == "SKILL.md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, canonicalPath := range paths {
		relative, err := filepath.Rel(root, canonicalPath)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(directory, relative)
		resolved, err := workspace.ResolvePath(path)
		if err != nil || !withinRoot(root, resolved) {
			catalog.report(ProblemEscape, fmt.Sprintf("%s: refused, skill file resolves outside its source root", path))
			continue
		}
		source, err := readSource(ctx, path)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			catalog.report(ProblemUnreadable, fmt.Sprintf("%s: unreadable, %s", path, readErrorName(err)))
			continue
		}
		name := source.name
		if !source.namePresent {
			name = filepath.Base(filepath.Dir(path))
		}
		// Python's catalogue regex uses match and permits one final newline;
		// its serve-time fullmatch remains stricter.
		if !validName(strings.TrimSuffix(name, "\n")) {
			diagnostic := pytext.Repr(name)
			if source.nameTruncated {
				diagnostic += " [name truncated]"
			}
			catalog.report(ProblemName, fmt.Sprintf("%s: refused, %s is not a valid skill name", path, diagnostic))
			continue
		}
		if previous, ok := catalog.entries[name]; ok {
			catalog.report(ProblemDuplicate, fmt.Sprintf("%s: ignored, %s is already defined by %s", path, pytext.Repr(name), previous.Path))
			continue
		}
		if source.bodyLength > MaxBody {
			catalog.report(ProblemBody, fmt.Sprintf("%s: body truncated from %s to 50,000 characters", path, grouped(source.bodyLength)))
			source.body += "\n[skill truncated]"
		}
		description := source.description
		if source.descriptionLength > MaxDescription {
			catalog.report(ProblemDescription, fmt.Sprintf("%s: description truncated from %s to 200 characters", path, grouped(source.descriptionLength)))
			description += "..."
		}
		if !source.descriptionPresent {
			description = "-"
		}
		bodyDigest := sha256.Sum256([]byte(source.body))
		entry := Entry{name, description, source.body, path, hex.EncodeToString(bodyDigest[:]), source.digest}
		catalog.entries[name] = entry
		catalog.ordered = append(catalog.ordered, entry)
	}
	return catalog, nil
}

func withinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
func validName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && (i == 0 || r != '.' && r != '_' && r != '-') {
			return false
		}
	}
	return true
}
func grouped(value int) string {
	text := fmt.Sprint(value)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}
func readErrorName(err error) string {
	if errors.Is(err, invalidUTF8) {
		return "UnicodeDecodeError"
	}
	if os.IsNotExist(err) {
		return "FileNotFoundError"
	}
	if errors.Is(err, syscall.EISDIR) {
		return "IsADirectoryError"
	}
	if os.IsPermission(err) {
		return "PermissionError"
	}
	return "OSError"
}
func (catalog *Catalog) report(kind ProblemKind, message string) {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	catalog.problemTotal++
	for i := range catalog.problems {
		if catalog.problems[i].Message == message {
			catalog.problems[i].Count++
			return
		}
	}
	if len(catalog.problems) == MaxProblems {
		catalog.problemDropped++
		copy(catalog.problems, catalog.problems[1:])
		catalog.problems = catalog.problems[:len(catalog.problems)-1]
	}
	catalog.problems = append(catalog.problems, Problem{kind, message, 1})
}
func (catalog *Catalog) Problems() []Problem {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	return append([]Problem{}, catalog.problems...)
}
func (catalog *Catalog) Entries() []Entry { return append([]Entry{}, catalog.ordered...) }

type ProblemStatistics struct{ Total, Dropped uint64 }

func (catalog *Catalog) ProblemStatistics() ProblemStatistics {
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	return ProblemStatistics{catalog.problemTotal, catalog.problemDropped}
}

func (catalog *Catalog) Descriptions() string {
	if len(catalog.ordered) == 0 {
		return "(no skills available)"
	}
	lines, used, dropped := []string{}, 0, 0
	for _, entry := range catalog.ordered {
		line := "  - " + entry.Name + ": " + entry.Description
		length := utf8.RuneCountInString(line)
		if used+length > MaxCatalogue {
			dropped++
			continue
		}
		lines = append(lines, line)
		used += length + 1
	}
	if dropped > 0 {
		lines = append(lines, fmt.Sprintf("  [%d more skill(s) omitted; catalogue is full]", dropped))
		catalog.report(ProblemCatalogue, fmt.Sprintf("%d skill(s) omitted from the catalogue at 8,000 characters", dropped))
	}
	return strings.Join(lines, "\n")
}

func (catalog *Catalog) Load(ctx context.Context, input protocol.LoadSkillInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if input.Name == "" {
		return "", errors.New("Skill name must be a non-empty valid identifier")
	}
	scope := ""
	if input.Scope != nil {
		scope = strings.ToLower(pytext.Strip(string(*input.Scope)))
	}
	name := input.Name
	prefix, remainder, separator := strings.Cut(name, ":")
	if separator && (prefix == "agent" || prefix == "user") {
		if input.Scope != nil && scope != prefix {
			return "", fmt.Errorf("Skill %s selects source %s, which conflicts with scope %s", pytext.Repr(name), pytext.Repr(prefix), pytext.Repr(scope))
		}
		scope, name = prefix, remainder
	}
	if !validName(name) {
		return "", errors.New("Skill name must match [A-Za-z0-9][A-Za-z0-9._-]{0,63}")
	}
	if scope == "user" {
		return "", errors.New("User-scoped skills are unavailable in this session")
	}
	if scope != "" && scope != "agent" {
		return "", fmt.Errorf("Unknown skill scope %s. Expected one of: agent, user", pytext.Repr(string(*input.Scope)))
	}
	entry, exists := catalog.entries[name]
	if !exists {
		return "", fmt.Errorf("Unknown skill '%s'. Available: %s", name, catalog.available())
	}
	current, err := readSource(ctx, entry.Path)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		catalog.report(ProblemUnreadable, fmt.Sprintf("%s: %s refused at load; the file became unreadable after cataloguing (%s)", entry.Path, pytext.Repr(name), readErrorName(err)))
		return "", fmt.Errorf("skill %s was catalogued at session start but its file is now missing or unreadable; refusing to serve instructions that can no longer be audited", pytext.Repr(name))
	}
	if current.digest != entry.SourceDigest {
		catalog.report(ProblemChanged, fmt.Sprintf("%s: %s refused at load; the file changed after cataloguing (source digest mismatch)", entry.Path, pytext.Repr(name)))
		return "", fmt.Errorf("skill %s changed on disk after it was catalogued; refusing to serve instructions nobody audited. Restart the session to catalogue the new version.", pytext.Repr(name))
	}
	return "<skill name=\"" + name + "\">\n" + entry.Body + "\n</skill>", nil
}
func (catalog *Catalog) available() string {
	if len(catalog.ordered) == 0 {
		return "(none)"
	}
	used, kept := 0, []string{}
	for _, entry := range catalog.ordered {
		added := utf8.RuneCountInString(entry.Name)
		if len(kept) > 0 {
			added += 2
		}
		if used+added > MaxCatalogue-256 {
			break
		}
		kept = append(kept, entry.Name)
		used += added
	}
	rendered := strings.Join(kept, ", ")
	if dropped := len(catalog.ordered) - len(kept); dropped > 0 {
		rendered += fmt.Sprintf(", ... (%d more omitted)", dropped)
	}
	return rendered
}
