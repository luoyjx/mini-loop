package pytext

import (
	"sort"
	"unicode/utf8"
)

// BidiClass is a Unicode 14.0.0 bidirectional class. BidiUnknown means that
// CPython 3.11.3 unicodedata.bidirectional returns the empty string.
type BidiClass uint8

const (
	BidiUnknown BidiClass = iota
	BidiL
	BidiR
	BidiAL
	BidiAN
	BidiEN
	BidiES
	BidiCS
	BidiET
	BidiON
	BidiBN
	BidiNSM
	BidiB
	BidiS
	BidiWS
	BidiLRE
	BidiLRO
	BidiRLE
	BidiRLO
	BidiPDF
	BidiLRI
	BidiRLI
	BidiFSI
	BidiPDI
)

type bidiClassRange struct {
	first, last rune
	class       BidiClass
}

// BidiClass14 preserves the fixed Source database, including its unknown
// codepoints, independently of the Unicode version bundled with Go.
func BidiClass14(value rune) BidiClass {
	index := sort.Search(len(pythonBidi14), func(i int) bool { return pythonBidi14[i].last >= value })
	if index < len(pythonBidi14) && pythonBidi14[index].first <= value {
		return pythonBidi14[index].class
	}
	return BidiUnknown
}

type canonicalDecomposition struct {
	source rune
	offset uint32
	length uint8
}

type combiningClassRange struct {
	first, last rune
	class       uint8
}

type canonicalComposition struct {
	first, second, composite rune
}

type normalizationRune struct {
	value rune
	class uint8
}

const (
	hangulSBase  = 0xac00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11a7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// IsNFC14 reports whether valid UTF-8 is already in CPython 3.11.3's Unicode
// 14.0.0 NFC form. It does not repair text. Invalid UTF-8 (including encoded lone
// surrogates) is refused at this Go string boundary. Unassigned scalars have the
// identity normalization, as in the fixed Source database.
func IsNFC14(value string) bool {
	ascii := true
	for i := 0; i < len(value); i++ {
		if value[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		return true
	}
	if !utf8.ValidString(value) {
		return false
	}
	for _, item := range normalizeNFC14(value) {
		if value == "" {
			return false
		}
		actual, size := utf8.DecodeRuneInString(value)
		if actual != item.value {
			return false
		}
		value = value[size:]
	}
	return value == ""
}

// normalizeNFC14 requires valid UTF-8. Canonical decomposition has a fixed,
// generated acyclic depth; reorder uses stable counting sort, so a long run of
// combining marks cannot induce quadratic insertion-sort work.
func normalizeNFC14(value string) []normalizationRune {
	items := make([]normalizationRune, 0, utf8.RuneCountInString(value))
	for _, r := range value {
		items = appendCanonical14(items, r)
	}
	reorderCanonical14(items)
	return composeCanonical14(items)
}

func canonicalClass14(value rune) uint8 {
	index := sort.Search(len(pythonCombining14), func(i int) bool { return pythonCombining14[i].last >= value })
	if index < len(pythonCombining14) && pythonCombining14[index].first <= value {
		return pythonCombining14[index].class
	}
	return 0
}

func appendCanonical14(items []normalizationRune, value rune) []normalizationRune {
	if syllable := value - hangulSBase; syllable >= 0 && syllable < hangulSCount {
		items = append(items,
			normalizationRune{value: hangulLBase + syllable/hangulNCount},
			normalizationRune{value: hangulVBase + (syllable%hangulNCount)/hangulTCount})
		if trailing := syllable % hangulTCount; trailing != 0 {
			items = append(items, normalizationRune{value: hangulTBase + trailing})
		}
		return items
	}
	index := sort.Search(len(pythonCanonical14), func(i int) bool { return pythonCanonical14[i].source >= value })
	if index < len(pythonCanonical14) && pythonCanonical14[index].source == value {
		entry := pythonCanonical14[index]
		for _, part := range pythonCanonicalRunes14[entry.offset : entry.offset+uint32(entry.length)] {
			items = appendCanonical14(items, part)
		}
		return items
	}
	return append(items, normalizationRune{value: value, class: canonicalClass14(value)})
}

func reorderCanonical14(items []normalizationRune) {
	var scratch []normalizationRune
	for start := 0; start < len(items); {
		if items[start].class == 0 {
			start++
			continue
		}
		end, ordered := start+1, true
		for end < len(items) && items[end].class != 0 {
			if items[end-1].class > items[end].class {
				ordered = false
			}
			end++
		}
		if !ordered {
			var offsets [256]int
			for _, item := range items[start:end] {
				offsets[item.class]++
			}
			position := 0
			for class, count := range offsets {
				offsets[class] = position
				position += count
			}
			if cap(scratch) < end-start {
				scratch = make([]normalizationRune, end-start)
			} else {
				scratch = scratch[:end-start]
			}
			for _, item := range items[start:end] {
				scratch[offsets[item.class]] = item
				offsets[item.class]++
			}
			copy(items[start:end], scratch)
		}
		start = end
	}
}

func composeCanonical14(items []normalizationRune) []normalizationRune {
	length, starter := 0, -1
	var lastClass uint8
	for _, item := range items {
		if starter >= 0 && (lastClass < item.class || lastClass == 0) {
			if composite, ok := canonicalPair14(items[starter].value, item.value); ok {
				items[starter].value = composite
				continue
			}
		}
		items[length] = item
		if item.class == 0 {
			starter = length
		}
		length++
		lastClass = item.class
	}
	return items[:length]
}

func canonicalPair14(first, second rune) (rune, bool) {
	if leading := first - hangulLBase; leading >= 0 && leading < hangulLCount {
		if vowel := second - hangulVBase; vowel >= 0 && vowel < hangulVCount {
			return hangulSBase + (leading*hangulVCount+vowel)*hangulTCount, true
		}
	}
	if syllable := first - hangulSBase; syllable >= 0 && syllable < hangulSCount && syllable%hangulTCount == 0 {
		if trailing := second - hangulTBase; trailing > 0 && trailing < hangulTCount {
			return first + trailing, true
		}
	}
	index := sort.Search(len(pythonComposition14), func(i int) bool {
		entry := pythonComposition14[i]
		return entry.first > first || (entry.first == first && entry.second >= second)
	})
	if index < len(pythonComposition14) {
		entry := pythonComposition14[index]
		if entry.first == first && entry.second == second {
			return entry.composite, true
		}
	}
	return 0, false
}
