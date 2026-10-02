package pytext

import (
	"sort"
	"strings"
)

type caseMapping struct {
	source rune
	value  string
}
type caseRange struct{ first, last rune }

func mappedRune(value rune, table []caseMapping) string {
	index := sort.Search(len(table), func(i int) bool { return table[i].source >= value })
	if index < len(table) && table[index].source == value {
		return table[index].value
	}
	return string(value)
}
func caseContains(value rune, table []caseRange) bool {
	index := sort.Search(len(table), func(i int) bool { return table[i].last >= value })
	return index < len(table) && table[index].first <= value
}

// Lower preserves Python's full Unicode mappings and contextual final sigma.
func Lower(value string) string {
	runes := []rune(value)
	var out strings.Builder
	for index, current := range runes {
		if current == 'Σ' {
			before, after := false, false
			for i := index - 1; i >= 0; i-- {
				if caseContains(runes[i], pythonCaseIgnorable[:]) {
					continue
				}
				before = caseContains(runes[i], pythonCased[:])
				break
			}
			for i := index + 1; i < len(runes); i++ {
				if caseContains(runes[i], pythonCaseIgnorable[:]) {
					continue
				}
				after = caseContains(runes[i], pythonCased[:])
				break
			}
			if before && !after {
				out.WriteRune('ς')
				continue
			}
		}
		out.WriteString(mappedRune(current, pythonLower[:]))
	}
	return out.String()
}
func Upper(value string) string {
	var out strings.Builder
	for _, current := range value {
		out.WriteString(mappedRune(current, pythonUpper[:]))
	}
	return out.String()
}
