// Package userresources defines owner-bound user-authored resources separately
// from agent skills. Publication and runtime activation are explicit later seams.
package userresources

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/skills"
)

const MaxSkillLines = 500

type SkillFields struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}
type ValidationCode string

const (
	InvalidName        ValidationCode = "invalid_name"
	InvalidDescription ValidationCode = "invalid_description"
	InvalidBody        ValidationCode = "invalid_body"
	UnsafeContent      ValidationCode = "unsafe_content"
)

type ValidationError struct {
	code    ValidationCode
	message string
}

func (e *ValidationError) Error() string                { return e.message }
func (e *ValidationError) Code() ValidationCode         { return e.code }
func invalid(code ValidationCode, message string) error { return &ValidationError{code, message} }

// CanonicalSkill owns normalized fields and immutable canonical UTF-8 content.
type CanonicalSkill struct {
	fields       SkillFields
	text, digest string
}

func (s CanonicalSkill) Fields() SkillFields { return s.fields }
func (s CanonicalSkill) Text() string        { return s.text }
func (s CanonicalSkill) Digest() string      { return s.digest }

var skillName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const spaces = `[\s\p{Z}\x{0085}\x{000b}\x{001c}-\x{001f}]`

var wrapper = regexp.MustCompile(`(?i)<` + spaces + `*/?` + spaces + `*skill(?:` + spaces + `|/?>)`)

// Python IGNORECASE also matches dotted/dotless I against ASCII i; Go's
// simple folding does not. Apply these aliases only while checking the tag.
var wrapperI = strings.NewReplacer("İ", "i", "ı", "i")

func containsWrapper(text string) bool { return wrapper.MatchString(wrapperI.Replace(text)) }
func pythonSpace(r rune) bool          { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) }
func lineBreak(r rune) bool {
	return r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == 0x1c || r == 0x1d || r == 0x1e || r == 0x85 || r == 0x2028 || r == 0x2029
}
func lineCount(text string) int {
	if text == "" {
		return 0
	}
	count := 0
	var previous rune
	lastBreak := false
	for _, r := range text {
		if r == '\n' && previous == '\r' {
			previous = r
			continue
		}
		lastBreak = lineBreak(r)
		if lastBreak {
			count++
		}
		previous = r
	}
	if !lastBreak {
		count++
	}
	return count
}
func NewCanonicalSkill(fields SkillFields) (CanonicalSkill, error) {
	if !utf8.ValidString(fields.Name) || !skillName.MatchString(fields.Name) {
		return CanonicalSkill{}, invalid(InvalidName, "Skill name must be lowercase kebab-case")
	}
	if len(fields.Name) > 64 {
		return CanonicalSkill{}, invalid(InvalidName, "Skill name must be at most 64 characters")
	}
	if fields.Description == "" {
		return CanonicalSkill{}, invalid(InvalidDescription, "Skill description must be non-empty")
	}
	if strings.ContainsAny(fields.Description, "\n\r") {
		return CanonicalSkill{}, invalid(InvalidDescription, "Skill description must be one line")
	}
	if utf8.RuneCountInString(fields.Description) > skills.MaxDescription {
		return CanonicalSkill{}, invalid(InvalidDescription, "Skill description must be at most 200 characters")
	}
	description := strings.TrimFunc(fields.Description, pythonSpace)
	if description == "" {
		return CanonicalSkill{}, invalid(InvalidDescription, "Skill description must be non-empty")
	}
	if strings.TrimFunc(fields.Body, pythonSpace) == "" {
		return CanonicalSkill{}, invalid(InvalidBody, "Skill body must be non-empty")
	}
	if utf8.RuneCountInString(fields.Body) > skills.MaxBody {
		return CanonicalSkill{}, invalid(InvalidBody, "Skill body must be at most 50000 characters")
	}
	if lineCount(fields.Body) > MaxSkillLines {
		return CanonicalSkill{}, invalid(InvalidBody, "Skill body must be at most 500 lines")
	}
	if strings.ContainsRune(fields.Description, '\x00') || strings.ContainsRune(fields.Body, '\x00') {
		return CanonicalSkill{}, invalid(UnsafeContent, "Skill fields must not contain NUL")
	}
	if containsWrapper(fields.Description) || containsWrapper(fields.Body) {
		return CanonicalSkill{}, invalid(UnsafeContent, "Skill content must not contain a skill wrapper")
	}
	if !utf8.ValidString(fields.Description) || !utf8.ValidString(fields.Body) {
		return CanonicalSkill{}, invalid(UnsafeContent, "Skill fields must be valid UTF-8")
	}
	body := strings.ReplaceAll(strings.ReplaceAll(fields.Body, "\r\n", "\n"), "\r", "\n")
	body = strings.TrimFunc(body, pythonSpace)
	normalized := SkillFields{fields.Name, description, body}
	text := "---\nname: " + fields.Name + "\ndescription: " + description + "\n---\n" + body + "\n"
	digest := sha256.Sum256([]byte(text))
	return CanonicalSkill{normalized, text, hex.EncodeToString(digest[:])}, nil
}

type DirectoryKey string

// OwnerDirectoryKey hashes the exact trusted identifier; it never trims or folds.
func OwnerDirectoryKey(owner string) (DirectoryKey, error) {
	if owner == "" || !utf8.ValidString(owner) {
		return "", errors.New("owner must be a non-empty UTF-8 string")
	}
	digest := sha256.Sum256([]byte(owner))
	return DirectoryKey("u-" + hex.EncodeToString(digest[:])), nil
}
