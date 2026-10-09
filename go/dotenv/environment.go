package dotenv

import (
	"regexp"
	"strings"
)

type Environment map[Name]string
type Variable struct {
	Key   Name    `json:"key"`
	Value *string `json:"value"`
}
type FailureClass string

const (
	InvalidEnvironment FailureClass = "ValueError"
	FilesystemFailure  FailureClass = "OSError"
	DecodeFailure      FailureClass = "UnicodeDecodeError"
	EncodingFailure    FailureClass = "LookupError"
	OriginFailure      FailureClass = "AssertionError"
	PermissionFailure  FailureClass = "PermissionError"
	MissingFileFailure FailureClass = "FileNotFoundError"
	DirectoryFailure   FailureClass = "IsADirectoryError"
)

type Failure struct {
	Class   FailureClass `json:"type"`
	Message string       `json:"message"`
}

func (f *Failure) Error() string { return f.Message }

type Application struct {
	Loaded      *bool       `json:"loaded"`
	Environment Environment `json:"applied"`
	Failure     *Failure    `json:"fault"`
}

var variablePattern = regexp.MustCompile(`\$\{([^}:]*)(?::-([^}]*))?\}`)

func interpolate(text string, lookup func(Name) (*string, bool)) string {
	return variablePattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := variablePattern.FindStringSubmatch(match)
		value, exists := lookup(Name(parts[1]))
		if exists {
			if value != nil {
				return *value
			}
			return ""
		}
		return parts[2]
	})
}

// Resolve preserves first-key insertion order and last binding values. Earlier
// valueless bindings shadow external values when override is true.
func Resolve(bindings []Binding, environment Environment, override bool) []Variable {
	values := []Variable{}
	index := make(map[Name]int)
	lookup := func(name Name) (*string, bool) {
		if !override {
			if v, ok := environment[name]; ok {
				return &v, true
			}
		}
		if i, ok := index[name]; ok {
			return values[i].Value, true
		}
		if v, ok := environment[name]; ok {
			return &v, true
		}
		return nil, false
	}
	for _, binding := range bindings {
		if binding.Error || binding.Key == nil {
			continue
		}
		var value *string
		if binding.Value != nil {
			v := interpolate(*binding.Value, lookup)
			value = &v
		}
		if i, ok := index[*binding.Key]; ok {
			values[i].Value = value
		} else {
			index[*binding.Key] = len(values)
			values = append(values, Variable{Key: *binding.Key, Value: value})
		}
	}
	return values
}

// Apply returns a detached environment, preserving writes before an invalid
// name/value fault. It never writes to os.Environ or mutates caller containers.
func Apply(values []Variable, environment Environment, override bool) Application {
	result := Application{Environment: make(Environment, len(environment))}
	for key, value := range environment {
		result.Environment[key] = value
	}
	if loadingDisabled(environment) {
		loaded := false
		result.Loaded = &loaded
		return result
	}
	for _, variable := range values {
		if _, exists := result.Environment[variable.Key]; exists && !override {
			continue
		}
		if variable.Value == nil {
			continue
		}
		message := ""
		if variable.Key == "" || strings.ContainsRune(string(variable.Key), '=') {
			message = "illegal environment variable name"
		} else if strings.ContainsRune(string(variable.Key), 0) || strings.ContainsRune(*variable.Value, 0) {
			message = "embedded null byte"
		}
		if message != "" {
			result.Failure = &Failure{Class: InvalidEnvironment, Message: message}
			return result
		}
		result.Environment[variable.Key] = *variable.Value
	}
	loaded := len(values) > 0
	result.Loaded = &loaded
	return result
}

func loadingDisabled(environment Environment) bool {
	// Long s is the only non-ASCII case-fold expansion into these finite tokens.
	value := strings.ReplaceAll(strings.ToLower(environment["PYTHON_DOTENV_DISABLED"]), "ſ", "s")
	return value == "1" || value == "true" || value == "t" || value == "yes" || value == "y"
}
