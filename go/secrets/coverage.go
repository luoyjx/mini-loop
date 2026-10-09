package secrets

import (
	"github.com/luoyjx/mini-loop/go/internal/fnmatch"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// CountUnregistered reports only a count. Registration is exact-case and does
// not resolve values; unresolved and short values remain registered names.
func CountUnregistered(environment Environment, registered []Name) int {
	if environment == nil {
		environment = CurrentEnvironment()
	}
	names := make(map[Name]bool, len(registered))
	for _, name := range registered {
		names[name] = true
	}
	patterns := make([]fnmatch.Pattern, len(defaultPatterns))
	for i, pattern := range defaultPatterns {
		patterns[i] = fnmatch.Compile(pattern)
	}
	count := 0
	for name, value := range environment {
		if value == "" || names[name] {
			continue
		}
		for _, pattern := range patterns {
			if pattern.Matches(pytext.Upper(string(name))) {
				count++
				break
			}
		}
	}
	return count
}
