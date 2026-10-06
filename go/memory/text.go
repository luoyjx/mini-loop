package memory

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func memoryKey(owner OwnerID, name string) string {
	digest := sha256.New()
	var size [8]byte
	for _, value := range []string{string(owner), name} {
		binary.BigEndian.PutUint64(size[:], uint64(len([]byte(value))))
		digest.Write(size[:])
		digest.Write([]byte(value))
	}
	return hex.EncodeToString(digest.Sum(nil))
}
func lineBreak(r rune) bool {
	return r == '\n' || r == '\r' || r == '\v' || r == '\f' || r == '\x1c' || r == '\x1d' || r == '\x1e' || r == '\u0085' || r == '\u2028' || r == '\u2029'
}
func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return nil
	}
	var lines []string
	start := 0
	for i, r := range text {
		if lineBreak(r) {
			lines = append(lines, text[start:i])
			start = i + utf8.RuneLen(r)
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}
func header(value string) string { return pytext.Strip(strings.Join(splitLines(value), " ")) }
func slug(name string) string {
	var out strings.Builder
	separator := false
	for _, r := range pytext.Lower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if separator && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	result := out.String()
	if len(result) > MaxSlug {
		result = result[:MaxSlug]
	}
	if result == "" || result == "memory" {
		result = "memory-" + hash(name)[:8]
	}
	return result
}
func origin(value Origin, fallback Origin) Origin {
	normalized := Origin(header(string(value)))
	switch normalized {
	case Explicit, AutoExtracted, Consolidated, Imported:
		return normalized
	}
	return fallback
}
func grouped(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func belongs(record Record, owner OwnerID) bool {
	if record.OwnerKey != "" {
		return record.OwnerKey == hash(string(owner))
	}
	return record.Owner == string(owner)
}
func parseRecord(filename, text string) Record {
	record := Record{File: filename, Name: strings.TrimSuffix(filename, ".md"), Owner: "anonymous", Scope: UserScope, Origin: Imported, Type: Project, Body: text}
	if !strings.HasPrefix(text, "---\n") {
		return record
	}
	remainder := text[4:]
	end := strings.Index(remainder, "\n---\n")
	if end < 0 {
		return record
	}
	metadata := make(map[string]string)
	for _, line := range splitLines(remainder[:end]) {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			metadata[pytext.Strip(key)] = pytext.Strip(value)
		}
	}
	record.Body = pytext.Strip(remainder[end+5:])
	if value, ok := metadata["name"]; ok {
		record.Name = value
	}
	if value, ok := metadata["owner"]; ok {
		record.Owner = value
	}
	record.Description = metadata["description"]
	if value, ok := metadata["type"]; ok {
		record.Type = Type(value)
	}
	record.Origin = origin(Origin(metadata["origin"]), Imported)
	key := pytext.Lower(metadata["owner_key"])
	if len(key) == 64 && strings.Trim(key, "0123456789abcdef") == "" {
		record.OwnerKey = key
	}
	return record
}
func terms(query string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, term := range strings.FieldsFunc(pytext.Lower(query), func(r rune) bool { return !(unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-') }) {
		if utf8.RuneCountInString(term) >= 2 && !seen[term] {
			seen[term] = true
			result = append(result, term)
		}
	}
	return result
}
