package pytext

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

type normalizationSourceCase struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	SourceBase64 string `json:"source_base64"`
	NFCBase64    string `json:"nfc_base64"`
	IsNFC        bool   `json:"is_nfc"`
}

type bidiSourceCase struct {
	Codepoint rune   `json:"codepoint"`
	Class     string `json:"class"`
}

type invalidNormalizationCase struct {
	Name         string `json:"name"`
	SourceBase64 string `json:"source_base64"`
}

type unicodeNormalizationSource struct {
	SchemaVersion      int                        `json:"schema_version"`
	Implementation     string                     `json:"python_implementation"`
	PythonVersion      string                     `json:"python_version"`
	UnicodeVersion     string                     `json:"unicode_version"`
	ExporterSHA256     string                     `json:"exporter_sha256"`
	Scope              string                     `json:"scope"`
	NormalizationCases []normalizationSourceCase  `json:"normalization_cases"`
	BidiCases          []bidiSourceCase           `json:"bidi_cases"`
	InvalidUTF8Cases   []invalidNormalizationCase `json:"invalid_utf8_cases"`
}

func readNormalizationSource(t *testing.T) unicodeNormalizationSource {
	t.Helper()
	data, err := os.ReadFile("../../testdata/python-unicode-normalization.json")
	if err != nil {
		t.Fatal(err)
	}
	var source unicodeNormalizationSource
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatal("unexpected trailing fixture data", err)
	}
	if source.SchemaVersion != 1 || source.Implementation != "CPython" || source.PythonVersion != "3.11.3" || source.UnicodeVersion != "14.0.0" {
		t.Fatal("fixed Source profile changed")
	}
	if len(source.ExporterSHA256) != 64 || source.Scope == "" {
		t.Fatal("missing Source provenance")
	}
	if NormalizationPythonVersion != source.PythonVersion || NormalizationUnicodeVersion != source.UnicodeVersion {
		t.Fatal("normalization uses a different Source profile")
	}
	if PythonUnicodeVersion != source.UnicodeVersion {
		t.Fatal("casing uses a different Source Unicode profile")
	}
	return source
}

func normalizationText(t *testing.T, encoded string) string {
	t.Helper()
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNFC14MatchesActualCPythonNormalization(t *testing.T) {
	source := readNormalizationSource(t)
	kinds := make(map[string]int)
	names := make(map[string]bool)
	for _, recipe := range source.NormalizationCases {
		if recipe.Name == "" || names[recipe.Name] {
			t.Fatal("missing or duplicate Source recipe", recipe.Name)
		}
		names[recipe.Name] = true
		kinds[recipe.Kind]++
		t.Run(recipe.Name, func(t *testing.T) {
			input := normalizationText(t, recipe.SourceBase64)
			expected := normalizationText(t, recipe.NFCBase64)
			if !utf8.ValidString(input) || !utf8.ValidString(expected) {
				t.Fatal("Source normalization recipe is not valid UTF-8")
			}
			if recipe.IsNFC != (input == expected) {
				t.Fatal("Source normalize/is_normalized observations disagree")
			}
			if actual := IsNFC14(input); actual != recipe.IsNFC {
				t.Fatalf("IsNFC14 = %v; actual Source = %v", actual, recipe.IsNFC)
			}
			var actual strings.Builder
			for _, item := range normalizeNFC14(input) {
				actual.WriteRune(item.value)
			}
			if actual.String() != expected {
				t.Fatalf("canonical output differs from Source; actual bytes %d, expected bytes %d", actual.Len(), len(expected))
			}
			if !IsNFC14(expected) {
				t.Fatal("actual Source NFC output was refused")
			}
		})
	}
	for kind, minimum := range map[string]int{
		"curated": 30, "canonical-scalar": 2000, "canonical-parts": 2000,
		"composition": 900, "hangul": 1200, "long-marks": 3,
	} {
		if kinds[kind] < minimum {
			t.Fatal("Source corpus lost algorithm coverage", kind, kinds[kind])
		}
	}
}

func TestNFC14RejectsInvalidUTF8WithoutReplacingIt(t *testing.T) {
	source := readNormalizationSource(t)
	if len(source.InvalidUTF8Cases) != 8 {
		t.Fatal("invalid UTF-8 adapter corpus changed", len(source.InvalidUTF8Cases))
	}
	for _, recipe := range source.InvalidUTF8Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			value := normalizationText(t, recipe.SourceBase64)
			if utf8.ValidString(value) {
				t.Fatal("invalid UTF-8 control became valid")
			}
			if IsNFC14(value) {
				t.Fatal("invalid UTF-8 was normalized or replaced")
			}
		})
	}
	if !IsNFC14("\ufffd") {
		t.Fatal("valid literal replacement character was confused with invalid encoding")
	}
}

func TestBidiClass14MatchesActualCPythonProperties(t *testing.T) {
	source := readNormalizationSource(t)
	expectedClasses := map[string]BidiClass{
		"Unknown": BidiUnknown, "L": BidiL, "R": BidiR, "AL": BidiAL, "AN": BidiAN,
		"EN": BidiEN, "ES": BidiES, "CS": BidiCS, "ET": BidiET, "ON": BidiON,
		"BN": BidiBN, "NSM": BidiNSM, "B": BidiB, "S": BidiS, "WS": BidiWS,
		"LRE": BidiLRE, "LRO": BidiLRO, "RLE": BidiRLE, "RLO": BidiRLO,
		"PDF": BidiPDF, "LRI": BidiLRI, "RLI": BidiRLI, "FSI": BidiFSI, "PDI": BidiPDI,
	}
	seenClasses := make(map[string]bool)
	seenPoints := make(map[rune]bool)
	for _, recipe := range source.BidiCases {
		expected, ok := expectedClasses[recipe.Class]
		if !ok || seenPoints[recipe.Codepoint] {
			t.Fatal("unknown class or duplicate Source codepoint", recipe)
		}
		seenPoints[recipe.Codepoint] = true
		seenClasses[recipe.Class] = true
		if actual := BidiClass14(recipe.Codepoint); actual != expected {
			t.Fatalf("U+%04X bidi = %d; actual Source %s = %d", recipe.Codepoint, actual, recipe.Class, expected)
		}
	}
	if len(seenClasses) != len(expectedClasses) {
		t.Fatal("Source bidi corpus omitted a class", len(seenClasses))
	}
	for _, value := range []rune{-1, 0x110000, 0x11f04, 0x1fae8} {
		if BidiClass14(value) != BidiUnknown {
			t.Fatalf("out-of-range or post-Unicode-14 value U+%X acquired a bidi class", value)
		}
	}
}
