// Package secrets registers named credentials, injects only named values, and
// masks by cached value. It never retains an untyped payload or environment.
package secrets

import (
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/fnmatch"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
	"github.com/luoyjx/mini-loop/go/protocol"
)

const Mask = "<secret-hidden>"
const MinMaskableLength = 8
const FailedLookupRetry = 60 * time.Second
const ansiEscape = `(?:\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-_]))`

var defaultPatterns = [...]string{"*_API_KEY", "*_APIKEY", "*_TOKEN", "*_SECRET", "*_SECRET_KEY", "*_PASSWORD", "*_PASSWD", "*_CREDENTIALS", "*_PRIVATE_KEY", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"}

func DefaultPatterns() []string { return append([]string(nil), defaultPatterns[:]...) }

type Name string
type Environment map[Name]string

// CurrentEnvironment takes a detached process snapshot. A nil environment in
// FromEnvironment/ScrubEnvironment follows Python's os.environ default.
func CurrentEnvironment() Environment {
	result := Environment{}
	for _, entry := range os.Environ() {
		if name, value, ok := strings.Cut(entry, "="); ok {
			result[Name(name)] = value
		}
	}
	return result
}

type Lookup func() (string, error)
type Config struct {
	MaskWith  *string
	MinLength *int
}
type lookupFlight struct{ done chan struct{} }
type credential struct {
	source   Lookup
	value    *string
	failedAt *time.Time
	tooShort bool
	flight   *lookupFlight
}
type Registry struct {
	mu          sync.Mutex
	sources     map[Name]*credential
	replacement string
	minimum     int
	now         func() time.Time
}

func New(config Config) *Registry {
	replacement, minimum := Mask, MinMaskableLength
	if config.MaskWith != nil {
		replacement = *config.MaskWith
	}
	if config.MinLength != nil {
		minimum = *config.MinLength
	}
	return &Registry{sources: make(map[Name]*credential), replacement: replacement, minimum: minimum, now: time.Now}
}
func (registry *Registry) RegisterValue(name Name, value string) {
	registry.RegisterLookup(name, func() (string, error) { return value, nil })
}
func (registry *Registry) RegisterLookup(name Name, lookup Lookup) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.sources[name] = &credential{source: lookup}
}
func FromEnvironment(config Config, environment Environment, patterns []string, extra []Name) *Registry {
	if environment == nil {
		environment = CurrentEnvironment()
	}
	registry := New(config)
	if patterns == nil {
		patterns = DefaultPatterns()
	}
	compiled := make([]fnmatch.Pattern, len(patterns))
	for i, pattern := range patterns {
		compiled[i] = fnmatch.Compile(pattern)
	}
	extras := make(map[Name]bool, len(extra))
	for _, name := range extra {
		extras[name] = true
	}
	for name, value := range environment {
		if value == "" {
			continue
		}
		selected := extras[name]
		for _, pattern := range compiled {
			if pattern.Matches(pytext.Upper(string(name))) {
				selected = true
				break
			}
		}
		if selected {
			registry.RegisterValue(name, value)
		}
	}
	return registry
}
func (registry *Registry) Names() []Name {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	result := make([]Name, 0, len(registry.sources))
	for name := range registry.sources {
		result = append(result, name)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
func (registry *Registry) Unresolved() []Name  { return registry.report(false) }
func (registry *Registry) ShortValues() []Name { return registry.report(true) }
func (registry *Registry) report(short bool) []Name {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	result := []Name{}
	for name, source := range registry.sources {
		if (short && source.tooShort) || (!short && source.failedAt != nil) {
			result = append(result, name)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
func resolve(lookup Lookup) (value string, err error) {
	defer func() {
		if recover() != nil {
			value = ""
			err = errors.New("secret lookup panic")
		}
	}()
	if lookup == nil {
		return "", errors.New("missing secret lookup")
	}
	return lookup()
}
func (registry *Registry) value(name Name) string {
	for {
		registry.mu.Lock()
		source := registry.sources[name]
		if source == nil {
			registry.mu.Unlock()
			return ""
		}
		if source.value != nil {
			value := *source.value
			registry.mu.Unlock()
			return value
		}
		if source.failedAt != nil && registry.now().Sub(*source.failedAt) < FailedLookupRetry {
			registry.mu.Unlock()
			return ""
		}
		if source.flight != nil {
			done := source.flight.done
			registry.mu.Unlock()
			<-done
			continue
		}
		flight := &lookupFlight{done: make(chan struct{})}
		source.flight = flight
		registry.mu.Unlock()
		value, err := resolve(source.source)
		registry.mu.Lock()
		// Re-registration cannot let an old in-flight source populate the new cache.
		current := registry.sources[name] == source
		if current {
			if err != nil || value == "" {
				now := registry.now()
				source.failedAt = &now
				value = ""
			} else {
				source.value = &value
				source.failedAt = nil
				source.tooShort = utf8.RuneCountInString(value) < registry.minimum
			}
		}
		source.flight = nil
		close(flight.done)
		registry.mu.Unlock()
		if current {
			return value
		}
	}
}
func (registry *Registry) FindInText(text string) []Name {
	lowered := pytext.Lower(text)
	result := []Name{}
	if text == "" {
		return result
	}
	for _, name := range registry.Names() {
		if strings.Contains(lowered, pytext.Lower(string(name))) {
			result = append(result, name)
		}
	}
	return result
}
func (registry *Registry) EnvForCommand(command string) Environment {
	result := Environment{}
	for _, name := range registry.FindInText(command) {
		if value := registry.value(name); value != "" {
			result[name] = value
		}
	}
	return result
}
func (registry *Registry) ScrubEnvironment(environment Environment) Environment {
	if environment == nil {
		environment = CurrentEnvironment()
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	result := make(Environment, len(environment))
	for name, value := range environment {
		if _, registered := registry.sources[name]; !registered {
			result[name] = value
		}
	}
	return result
}
func (registry *Registry) MaskText(text string) string {
	if text == "" {
		return text
	}
	values := []string{}
	for _, name := range registry.Names() {
		value := registry.value(name)
		if value != "" && utf8.RuneCountInString(value) >= registry.minimum {
			values = append(values, value)
		}
	}
	sort.SliceStable(values, func(i, j int) bool { return utf8.RuneCountInString(values[i]) > utf8.RuneCountInString(values[j]) })
	for _, value := range values {
		parts := make([]string, 0, utf8.RuneCountInString(value))
		for _, char := range value {
			parts = append(parts, regexp.QuoteMeta(string(char)))
		}
		pattern := regexp.MustCompile(strings.Join(parts, "(?:"+ansiEscape+")*"))
		text = pattern.ReplaceAllStringFunc(text, func(string) string { return registry.replacement })
	}
	return text
}
func (registry *Registry) MaskApprovalInput(input protocol.ToolInput) protocol.ToolInput {
	return protocol.MapToolInputStrings(input, registry.MaskText)
}
func (registry *Registry) ApprovalPreview(input protocol.ToolInput) (string, error) {
	return protocol.MaskedPythonJSON(input, registry.MaskText, true, false)
}

// Null retains the deployment default: no registration, injection, or masking.
type Null struct{}

func (Null) Names() []Name                    { return []Name{} }
func (Null) FindInText(string) []Name         { return []Name{} }
func (Null) EnvForCommand(string) Environment { return Environment{} }
func (Null) ScrubEnvironment(environment Environment) Environment {
	if environment == nil {
		environment = CurrentEnvironment()
	}
	out := Environment{}
	for k, v := range environment {
		out[k] = v
	}
	return out
}
func (Null) MaskText(text string) string                                   { return text }
func (Null) MaskApprovalInput(input protocol.ToolInput) protocol.ToolInput { return input }
