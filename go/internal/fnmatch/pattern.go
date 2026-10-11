package fnmatch

type patternTokenKind uint8

const (
	patternLiteral patternTokenKind = iota
	patternStar
	patternQuestion
	patternClass
)

type runeRange struct{ first, last rune }
type patternToken struct {
	kind     patternTokenKind
	literal  rune
	negated  bool
	literals []rune
	ranges   []runeRange
}
type Pattern struct {
	tokens        []patternToken
	minimumLength int
}

// Python fnmatch treats backslash as a literal and tolerates malformed brackets.
// filepath.Match uses different escape and malformed-pattern behavior.
func Compile(pattern string) Pattern {
	runes := []rune(pattern)
	var result Pattern
	for i := 0; i < len(runes); i++ {
		token := patternToken{kind: patternLiteral, literal: runes[i]}
		switch runes[i] {
		case '*':
			if len(result.tokens) > 0 && result.tokens[len(result.tokens)-1].kind == patternStar {
				continue
			}
			token.kind = patternStar
		case '?':
			token.kind = patternQuestion
		case '[':
			end := i + 1
			if end < len(runes) && runes[end] == '!' {
				end++
			}
			if end < len(runes) && runes[end] == ']' {
				end++
			}
			for end < len(runes) && runes[end] != ']' {
				end++
			}
			if end < len(runes) {
				token = compileClass(runes[i+1 : end])
				i = end
			}
		}
		if token.kind != patternStar {
			result.minimumLength++
		}
		result.tokens = append(result.tokens, token)
	}
	return result
}

func compileClass(data []rune) patternToken {
	token := patternToken{kind: patternClass}
	var chunks [][]rune
	start, search := 0, 1
	if len(data) > 0 && data[0] == '!' {
		search = 2
	}
	for {
		index := search
		for index < len(data) && data[index] != '-' {
			index++
		}
		if index >= len(data) {
			break
		}
		chunks = append(chunks, append([]rune(nil), data[start:index]...))
		start, search = index+1, index+3
	}
	if start < len(data) {
		chunks = append(chunks, append([]rune(nil), data[start:]...))
	} else if len(chunks) > 0 {
		chunks[len(chunks)-1] = append(chunks[len(chunks)-1], '-')
	}
	// Python removes descending ranges instead of rejecting the whole pattern.
	for i := len(chunks) - 1; i > 0; i-- {
		before, after := chunks[i-1], chunks[i]
		if before[len(before)-1] > after[0] {
			chunks[i-1] = append(before[:len(before)-1], after[1:]...)
			chunks = append(chunks[:i], chunks[i+1:]...)
		}
	}
	if len(chunks) > 0 && len(chunks[0]) > 0 && chunks[0][0] == '!' {
		token.negated = true
		chunks[0] = chunks[0][1:]
	}
	for i, chunk := range chunks {
		token.literals = append(token.literals, chunk...)
		if i > 0 && len(chunks[i-1]) > 0 && len(chunk) > 0 {
			token.ranges = append(token.ranges, runeRange{chunks[i-1][len(chunks[i-1])-1], chunk[0]})
		}
	}
	return token
}

func (token patternToken) accepts(value rune) bool {
	switch token.kind {
	case patternLiteral:
		return token.literal == value
	case patternQuestion:
		return true
	case patternClass:
		contained := false
		for _, literal := range token.literals {
			if value == literal {
				contained = true
				break
			}
		}
		if !contained {
			for _, interval := range token.ranges {
				if value >= interval.first && value <= interval.last {
					contained = true
					break
				}
			}
		}
		return contained != token.negated
	default:
		return false
	}
}

// Dynamic programming bounds matching work; wildcard strings never become an
// unchecked regular expression or a backtracking search.
func (pattern Pattern) Matches(name string) bool {
	runes := []rune(name)
	if len(runes) < pattern.minimumLength {
		return false
	}
	previous, current := make([]bool, len(runes)+1), make([]bool, len(runes)+1)
	previous[0] = true
	for _, token := range pattern.tokens {
		clear(current)
		if token.kind == patternStar {
			current[0] = previous[0]
		}
		for i, value := range runes {
			if token.kind == patternStar {
				current[i+1] = previous[i+1] || current[i]
			} else {
				current[i+1] = previous[i] && token.accepts(value)
			}
		}
		previous, current = current, previous
	}
	return previous[len(runes)]
}
