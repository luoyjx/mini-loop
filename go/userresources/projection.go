package userresources

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const MaxProjectionChars = 40000

// ProjectionLabel is display data. Masking may redact it; it grants no role.
type ProjectionLabel string
type ProjectedText struct {
	Content             string          `json:"content"`
	Role                ProjectionLabel `json:"role"`
	maskedKeys          bool
	contentKey, roleKey string
}

// MarshalJSON keeps recursive source masking of the two fixed display keys.
// The temporary map is a JSON boundary only; no dynamic payload is retained.
func (message ProjectedText) MarshalJSON() ([]byte, error) {
	contentKey, roleKey := "content", "role"
	if message.maskedKeys {
		contentKey, roleKey = message.contentKey, message.roleKey
	}
	fields := map[string]string{roleKey: string(message.Role)}
	fields[contentKey] = message.Content
	return json.Marshal(fields)
}

type AuthenticatedText struct {
	Role    protocol.Role
	Content string
}
type SkillProjection struct {
	CompactedHistoryExcluded bool            `json:"compacted_history_excluded"`
	Coverage                 DraftCoverage   `json:"coverage"`
	Messages                 []ProjectedText `json:"messages"`
	Omitted                  int             `json:"omitted"`
}
type ProjectionOptions struct {
	MaxChars                 int
	PriorOmitted             int
	CompactedHistoryExcluded bool
}

func DefaultProjectionOptions() ProjectionOptions {
	return ProjectionOptions{MaxChars: MaxProjectionChars}
}

func projectionCasePattern(pattern string) string {
	// Python IGNORECASE adds dotted/dotless i to ASCII i and to [a-z].
	pattern = strings.ReplaceAll(pattern, "[a-z0-9_-]", "[a-z0-9_İı-]")
	return "(?i)" + strings.ReplaceAll(pattern, "i", "[iİı]")
}

var projectionMemory = regexp.MustCompile(`(?s)\A` + spaces + `*<memory_context>` + spaces + `*\n.*\n</memory_context>` + spaces + `*\n*`)
var projectionInterjection = regexp.MustCompile("(?s)" + projectionCasePattern(`\A`+spaces+`*<user_interjection>`+spaces+`*\n?(.*?)\n?`+spaces+`*</user_interjection>`+spaces+`*\z`))
var projectionInjectedTag = regexp.MustCompile(projectionCasePattern(`\A` + spaces + `*(?:<runtime-state(?:` + spaces + `[^>]*)?>|<task_notification(?:` + spaces + `[^>]*)?>|<team_inbox(?:` + spaces + `[^>]*)?>|<workflow(?:[-_][a-z0-9_-]+)?(?:` + spaces + `[^>]*)?>|\[error\])`))
var projectionInjectedBracket = regexp.MustCompile(projectionCasePattern(`\A` + spaces + `*(?:\[(?:scheduled` + spaces + `+)?cron|\[goal` + spaces + `+round|\[turn` + spaces + `+interrupted|\[stopped|\[context` + spaces + `+compressed|\[snipped)`))
var projectionGap = regexp.MustCompile(projectionCasePattern(`\A` + spaces + `*(?:\[context` + spaces + `+compressed|\[snipped)`))

func projectionBoundary(pattern *regexp.Regexp, text string) bool {
	match := pattern.FindStringIndex(text)
	if match == nil {
		return false
	}
	if match[1] == len(text) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(text[match[1]:])
	return !pytext.IsWord(next)
}

func cleanProjectionText(text string) (string, bool) {
	if strings.HasPrefix(strings.TrimLeftFunc(text, pythonSpace), "<memory_context>") {
		match := projectionMemory.FindStringIndex(text)
		if match == nil {
			return "", false
		}
		text = text[match[1]:]
	}
	if match := projectionInterjection.FindStringSubmatch(text); match != nil {
		text = match[1]
	}
	if projectionInjectedTag.MatchString(text) || projectionBoundary(projectionInjectedBracket, text) {
		return "", false
	}
	text = pytext.Strip(text)
	return text, text != ""
}

func maskProjectionText(message ProjectedText, masker memory.Masker) (ProjectedText, error) {
	if !utf8.ValidString(message.Content) || !utf8.ValidString(string(message.Role)) {
		return ProjectedText{}, draftError(DraftInvalidPreview, 422)
	}
	if masker != nil {
		message.Content = masker.MaskText(message.Content)
		message.Role = ProjectionLabel(masker.MaskText(string(message.Role)))
		message.maskedKeys = true
		message.contentKey = masker.MaskText("content")
		message.roleKey = masker.MaskText("role")
	}
	if !utf8.ValidString(message.Content) || !utf8.ValidString(string(message.Role)) || !utf8.ValidString(message.contentKey) || !utf8.ValidString(message.roleKey) {
		return ProjectedText{}, draftError(DraftInvalidPreview, 422)
	}
	return message, nil
}

func boundProjection(messages []ProjectedText, masker memory.Masker, options ProjectionOptions, coverage DraftCoverage) (SkillProjection, error) {
	if options.MaxChars <= 0 {
		return SkillProjection{}, draftError(DraftInvalidPreview, 422)
	}
	limit := min(options.MaxChars, MaxProjectionChars)
	masked := make([]ProjectedText, len(messages))
	costs := make([]int, len(messages))
	for i, message := range messages {
		message, err := maskProjectionText(message, masker)
		if err != nil {
			return SkillProjection{}, err
		}
		masked[i] = message
		serialized, err := protocol.PythonJSON(message, false, true)
		if err != nil {
			return SkillProjection{}, draftError(DraftInvalidPreview, 422)
		}
		costs[i] = utf8.RuneCountInString(serialized)
	}
	used, start, omitted := 2, len(masked), options.PriorOmitted
	for i := len(masked) - 1; i >= 0; i-- {
		cost := costs[i]
		if start < len(masked) {
			cost++
		}
		if used+cost > limit {
			omitted += i + 1
			break
		}
		start = i
		used += cost
	}
	if omitted != 0 {
		coverage = DraftCoverage(string(coverage) + "_tail")
	}
	return SkillProjection{Messages: append([]ProjectedText{}, masked[start:]...), Coverage: coverage, Omitted: omitted, CompactedHistoryExcluded: options.CompactedHistoryExcluded}, nil
}

// ProjectAuthenticatedText accepts only an already admitted provenance ledger.
// It preserves nonempty text verbatim; it does not grant provenance itself.
func ProjectAuthenticatedText(messages []AuthenticatedText, masker memory.Masker, options ProjectionOptions) (SkillProjection, error) {
	projected := make([]ProjectedText, 0, len(messages))
	for _, message := range messages {
		if (message.Role == protocol.RoleUser || message.Role == protocol.RoleAssistant) && message.Content != "" {
			projected = append(projected, ProjectedText{Content: message.Content, Role: ProjectionLabel(message.Role)})
		}
	}
	return boundProjection(projected, masker, options, AuthenticatedTurns)
}

// ProjectSessionText is the source legacy current-epoch projection. It cannot
// establish HTTP provenance; user block arrays are discarded as protocol data.
func ProjectSessionText(messages []protocol.Message, masker memory.Masker, maxChars int) (SkillProjection, error) {
	projected := make([]ProjectedText, 0, len(messages))
	gap := false
	for _, message := range messages {
		if message.Role != protocol.RoleUser && message.Role != protocol.RoleAssistant {
			continue
		}
		cleaned := ""
		if plain, ok := message.Content.Plain(); ok {
			gap = gap || projectionBoundary(projectionGap, plain)
			cleaned, _ = cleanProjectionText(plain)
		} else if message.Role == protocol.RoleAssistant {
			blocks, _ := message.Content.Blocks()
			parts := []string{}
			for _, block := range blocks {
				text, ok := block.Text()
				if !ok {
					continue
				}
				gap = gap || projectionBoundary(projectionGap, text.Text)
				if part, ok := cleanProjectionText(text.Text); ok {
					parts = append(parts, part)
				}
			}
			cleaned = strings.Join(parts, "\n")
		}
		if cleaned != "" {
			projected = append(projected, ProjectedText{Content: cleaned, Role: ProjectionLabel(message.Role)})
		}
	}
	return boundProjection(projected, masker, ProjectionOptions{MaxChars: maxChars, CompactedHistoryExcluded: gap}, CurrentEpoch)
}
