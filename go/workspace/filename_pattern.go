package workspace

import "github.com/luoyjx/mini-loop/go/internal/fnmatch"

type filenamePattern struct{ pattern fnmatch.Pattern }

func compileFilenamePattern(value string) filenamePattern {
	return filenamePattern{fnmatch.Compile(value)}
}
func (pattern filenamePattern) matches(value string) bool { return pattern.pattern.Matches(value) }
