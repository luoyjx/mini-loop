package provider

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/punycode"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// HTTPX selects its ASCII bypass before lowercasing. Unicode hosts use strict
// IDNA2008, not UTS46 lookup mappings or normalization repair. The property
// tables below this adapter are pinned to idna 3.18 and CPython Unicode 14.
func providerEncodedHostname(host string) (string, error) {
	if asciiHostname(host) {
		return strings.ToLower(host), nil
	}
	host = pytext.Lower(host)
	if !utf8.ValidString(host) || utf8.RuneCountInString(host) > 254 {
		return "", errors.New("invalid provider IDNA hostname")
	}
	host = strings.Map(func(r rune) rune {
		if r == '\u3002' || r == '\uff0e' || r == '\uff61' {
			return '.'
		}
		return r
	}, host)
	trailing := strings.HasSuffix(host, ".")
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	for i, label := range labels {
		if asciiHostname(label) {
			if _, err := sourceIDNADecodeLabel(label); err != nil {
				return "", err
			}
		} else {
			if !sourceIDNAValidLabel(label) {
				return "", errors.New("invalid provider IDNA hostname")
			}
			var err error
			label, err = punycode.Encode(label)
			label = "xn--" + label
			if err != nil {
				return "", errors.New("invalid provider IDNA hostname")
			}
		}
		if len(label) == 0 || len(label) > 63 {
			return "", errors.New("invalid provider IDNA hostname")
		}
		labels[i] = label
	}
	encoded := strings.Join(labels, ".")
	limit := 253
	if trailing {
		encoded += "."
		limit = 254
	}
	if len(encoded) > limit {
		return "", errors.New("invalid provider IDNA hostname")
	}
	return encoded, nil
}

func asciiHostname(host string) bool {
	for i := range host {
		if host[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// HTTPX URL.host decodes the whole domain only when its first label has the
// ACE prefix. Request preparation accesses this property before dispatch; an
// ASCII malformed A-label must therefore fail here, not during construction.
func providerIDNARequestValid(host string) bool {
	if !strings.HasPrefix(host, "xn--") {
		return true
	}
	if len(host) > 254 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if _, err := sourceIDNADecodeLabel(label); err != nil {
			return false
		}
	}
	return true
}

func sourceIDNADecodeLabel(label string) (string, error) {
	if strings.HasPrefix(label, "xn--") {
		if len(label) == 4 || strings.HasSuffix(label, "-") {
			return "", errors.New("invalid provider IDNA hostname")
		}
		decoded, err := punycode.Decode(strings.TrimPrefix(label, "xn--"))
		if err != nil || !sourceIDNAValidLabel(decoded) {
			return "", errors.New("invalid provider IDNA hostname")
		}
		return decoded, nil
	}
	if !sourceIDNAValidLabel(label) {
		return "", errors.New("invalid provider IDNA hostname")
	}
	return label, nil
}

func sourceIDNAValidLabel(label string) bool {
	runes := []rune(label)
	if len(runes) == 0 || len(runes) > 254 || !utf8.ValidString(label) || !pytext.IsNFC14(label) {
		return false
	}
	if runes[0] == '-' || runes[len(runes)-1] == '-' || (len(runes) >= 4 && runes[2] == '-' && runes[3] == '-') || sourceIDNAMark(runes[0]) {
		return false
	}
	for i, r := range runes {
		if pytext.BidiClass14(r) == pytext.BidiUnknown {
			return false
		}
		switch {
		case sourceIDNAPValid(r):
		case sourceIDNAContextJ(r):
			if !sourceIDNAJoiner(runes, i) {
				return false
			}
		case sourceIDNAContextO(r):
			if !sourceIDNAOtherContext(runes, i) {
				return false
			}
		default:
			return false
		}
	}
	return sourceIDNABidi(runes)
}

func sourceIDNAJoiner(label []rune, pos int) bool {
	if pos > 0 && sourceIDNAVirama(label[pos-1]) {
		return true
	}
	if label[pos] != '\u200c' {
		return false
	}
	left := false
	for i := pos - 1; i >= 0; i-- {
		typeOf := sourceIDNAJoiningType(label[i])
		if typeOf == 'T' {
			continue
		}
		left = typeOf == 'L' || typeOf == 'D'
		break
	}
	if !left {
		return false
	}
	for i := pos + 1; i < len(label); i++ {
		typeOf := sourceIDNAJoiningType(label[i])
		if typeOf != 'T' {
			return typeOf == 'R' || typeOf == 'D'
		}
	}
	return false
}

func sourceIDNAOtherContext(label []rune, pos int) bool {
	r := label[pos]
	switch r {
	case '\u00b7':
		return pos > 0 && pos+1 < len(label) && label[pos-1] == 'l' && label[pos+1] == 'l'
	case '\u0375':
		return pos+1 < len(label) && sourceIDNAGreek(label[pos+1])
	case '\u05f3', '\u05f4':
		return pos > 0 && sourceIDNAHebrew(label[pos-1])
	case '\u30fb':
		for _, other := range label {
			if other != r && (sourceIDNAHiragana(other) || sourceIDNAKatakana(other) || sourceIDNAHan(other)) {
				return true
			}
		}
		return false
	}
	for _, other := range label {
		if r >= 0x660 && r <= 0x669 && other >= 0x6f0 && other <= 0x6f9 {
			return false
		}
		if r >= 0x6f0 && r <= 0x6f9 && other >= 0x660 && other <= 0x669 {
			return false
		}
	}
	return r >= 0x660 && r <= 0x669 || r >= 0x6f0 && r <= 0x6f9
}

func sourceIDNABidi(label []rune) bool {
	rtlPresent := false
	for _, r := range label {
		class := pytext.BidiClass14(r)
		rtlPresent = rtlPresent || class == pytext.BidiR || class == pytext.BidiAL || class == pytext.BidiAN
	}
	if !rtlPresent {
		return true
	}
	first := pytext.BidiClass14(label[0])
	rtl := first == pytext.BidiR || first == pytext.BidiAL
	if !rtl && first != pytext.BidiL {
		return false
	}
	ending, number := false, pytext.BidiUnknown
	for _, r := range label {
		class := pytext.BidiClass14(r)
		if rtl {
			switch class {
			case pytext.BidiR, pytext.BidiAL, pytext.BidiAN, pytext.BidiEN:
				ending = true
			case pytext.BidiES, pytext.BidiCS, pytext.BidiET, pytext.BidiON, pytext.BidiBN:
				ending = false
			case pytext.BidiNSM:
			default:
				return false
			}
			if class == pytext.BidiAN || class == pytext.BidiEN {
				if number != pytext.BidiUnknown && number != class {
					return false
				}
				number = class
			}
		} else {
			switch class {
			case pytext.BidiL, pytext.BidiEN:
				ending = true
			case pytext.BidiES, pytext.BidiCS, pytext.BidiET, pytext.BidiON, pytext.BidiBN:
				ending = false
			case pytext.BidiNSM:
			default:
				return false
			}
		}
	}
	return ending
}
