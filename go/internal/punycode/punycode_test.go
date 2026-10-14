package punycode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRFC3492RawPayloads(t *testing.T) {
	// The Chinese and mixed-script Japanese vectors are RFC 3492 section 7.1
	// examples B and L. Other small vectors pin the raw codec's delimiter rule.
	cases := []struct {
		text    string
		payload string
	}{
		{"", ""},
		{"abc", "abc-"},
		{"AbC", "AbC-"},
		{"-", "--"},
		{"\x00-\x7f", "\x00-\x7f-"},
		{"\u0080", "a"},
		{"\u0080\u0080", "aa"},
		{"b\u00fccher", "bcher-kva"},
		{"ma\u00f1ana", "maana-pta"},
		{"\u4ed6\u4eec\u4e3a\u4ec0\u4e48\u4e0d\u8bf4\u4e2d\u6587", "ihqwcrb4cv8a8dqg056pqjye"},
		{"3\u5e74B\u7d44\u91d1\u516b\u5148\u751f", "3B-ww4c5e180e575a65lsy2b"},
	}
	for _, entry := range cases {
		encoded, err := Encode(entry.text)
		if err != nil || encoded != entry.payload {
			t.Errorf("Encode(%q) = %q, %v; want %q", entry.text, encoded, err, entry.payload)
		}
		decoded, err := Decode(entry.payload)
		if err != nil || decoded != entry.text {
			t.Errorf("Decode(%q) = %q, %v; want %q", entry.payload, decoded, err, entry.text)
		}
	}
	for _, payload := range []string{"bcher-KVA", "bcher-kVa"} {
		if decoded, err := Decode(payload); err != nil || decoded != "b\u00fccher" {
			t.Errorf("case-insensitive extended digits %q: %q, %v", payload, decoded, err)
		}
	}
	if decoded, err := Decode("BCHER-KVA"); err != nil || decoded != "B\u00fcCHER" {
		t.Fatalf("basic prefix case was not retained: %q, %v", decoded, err)
	}
	if decoded, err := Decode("-"); err != nil || decoded != "" {
		t.Fatalf("CPython empty basic prefix: %q, %v", decoded, err)
	}
}

func TestScalarAndWorkBounds(t *testing.T) {
	for _, input := range []string{"\x80", "\xc0\x80", "\xed\xa0\x80", "\xed\xbf\xbf", "\xf4\x90\x80\x80"} {
		if result, err := Encode(input); result != "" || !errors.Is(err, ErrInvalidUTF8) {
			t.Errorf("invalid UTF-8 %x: %q, %v", input, result, err)
		}
	}
	for _, input := range []string{"z", "9", "!", "a-!", "\x80-", "a-\xff", "a\x00"} {
		if result, err := Decode(input); result != "" || !errors.Is(err, ErrInvalidPayload) {
			t.Errorf("malformed payload %x: %q, %v", input, result, err)
		}
	}
	if result, err := Decode(strings.Repeat("9", 256)); result != "" || !errors.Is(err, ErrOverflow) {
		t.Fatalf("overflowing generalized integer: %q, %v", result, err)
	}
	if result, err := Decode(strings.Repeat("9", 10) + "a"); result != "" || !errors.Is(err, ErrInvalidScalar) {
		t.Fatalf("out-of-range scalar: %q, %v", result, err)
	}
	for _, text := range []string{strings.Repeat("A", MaxCodePoints), strings.Repeat("\u0080", MaxCodePoints)} {
		payload, err := Encode(text)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(payload)
		if err != nil || decoded != text {
			t.Fatalf("accepted scalar boundary did not round trip: %v", err)
		}
	}
	if _, err := Encode(strings.Repeat("a", MaxCodePoints+1)); !errors.Is(err, ErrLimit) {
		t.Fatalf("encoder code point limit: %v", err)
	}
	for _, payload := range []string{
		strings.Repeat("a", MaxCodePoints+1) + "-",
		strings.Repeat("a", MaxCodePoints+1),
		strings.Repeat("a", MaxPayloadBytes+1),
	} {
		if _, err := Decode(payload); !errors.Is(err, ErrLimit) {
			t.Errorf("decoder limit for %d bytes: %v", len(payload), err)
		}
	}
	// Arithmetic checks are independent of machine int width and check the
	// exact uint64 boundary, rather than relying only on a long bad payload.
	if value, ok := addProduct(maxInteger-6, 2, 3); !ok || value != maxInteger {
		t.Fatal("exact representable arithmetic boundary was refused")
	}
	if _, ok := addProduct(maxInteger-5, 2, 3); ok {
		t.Fatal("overflowing addition was accepted")
	}
	if _, ok := addProduct(0, maxInteger, 2); ok {
		t.Fatal("overflowing multiplication was accepted")
	}
}

type codecReference struct {
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	SourcePath     string `json:"source_path"`
	SourceSHA256   string `json:"source_sha256"`
	License        string `json:"license"`
}

type codecLimits struct {
	ScalarOnly      bool `json:"scalar_only"`
	MaxCodePoints   int  `json:"max_code_points"`
	MaxPayloadBytes int  `json:"max_payload_bytes"`
}

type encodeObservation struct {
	Name             string `json:"name"`
	SourceCodePoints []int  `json:"source_code_points"`
	SourcePayload    string `json:"source_payload"`
	NativeAccept     bool   `json:"native_accept"`
}

type decodeObservation struct {
	Name               string `json:"name"`
	PayloadHex         string `json:"payload_hex"`
	SourceCodePoints   []int  `json:"source_code_points"`
	SourceErrorClass   string `json:"source_error_class"`
	SourceErrorMessage string `json:"source_error_message"`
	NativeAccept       bool   `json:"native_accept"`
}

type codecFixture struct {
	SchemaVersion   int                 `json:"schema_version"`
	Reference       codecReference      `json:"reference"`
	GeneratorSHA256 string              `json:"generator_sha256"`
	NativeContract  codecLimits         `json:"native_contract"`
	Encodings       []encodeObservation `json:"encodings"`
	Decodings       []decodeObservation `json:"decodings"`
}

func TestCPython3113DifferentialEncodeAndDecode(t *testing.T) {
	data, err := os.ReadFile("../../testdata/python-punycode.json")
	if err != nil {
		t.Fatal("generate fixed CPython fixture with python/tools/export_go_punycode.py:", err)
	}
	var fixture codecFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 1 || fixture.Reference.Implementation != "CPython" || fixture.Reference.Version != "3.11.3" ||
		fixture.Reference.SourcePath != "Lib/encodings/punycode.py" ||
		fixture.Reference.SourceSHA256 != "34edc8fb1c50e4d1cbaa1e008bb491cd7c12116c316e51974f333fe7b628eb7c" ||
		fixture.Reference.License != "Python Software Foundation License Version 2" {
		t.Fatalf("unrecognized reference provenance: %+v", fixture.Reference)
	}
	producer, err := os.ReadFile("../../../python/tools/export_go_punycode.py")
	if err != nil {
		t.Fatal(err)
	}
	producerHash := sha256.Sum256(producer)
	if fixture.GeneratorSHA256 != hex.EncodeToString(producerHash[:]) {
		t.Fatal("fixture does not match the current fixed-source producer")
	}
	if fixture.NativeContract != (codecLimits{true, MaxCodePoints, MaxPayloadBytes}) ||
		len(fixture.Encodings) != 135 || len(fixture.Decodings) != 215 {
		t.Fatalf("unexpected fixture bounds/counts: %+v, %d/%d", fixture.NativeContract, len(fixture.Encodings), len(fixture.Decodings))
	}
	for _, entry := range fixture.Encodings {
		t.Run("encode/"+entry.Name, func(t *testing.T) {
			text, scalar := referenceText(t, entry.SourceCodePoints)
			if entry.NativeAccept != scalar {
				t.Fatal("fixture scalar adaptation is inconsistent")
			}
			actual, err := Encode(text)
			if !entry.NativeAccept {
				if actual != "" || !errors.Is(err, ErrInvalidUTF8) {
					t.Fatalf("Python surrogate string must be explicitly refused: %x, %v", actual, err)
				}
				return
			}
			if err != nil || actual != entry.SourcePayload {
				t.Fatalf("raw encode = %q, %v; CPython = %q", actual, err, entry.SourcePayload)
			}
		})
	}
	for _, entry := range fixture.Decodings {
		t.Run("decode/"+entry.Name, func(t *testing.T) {
			payload, err := hex.DecodeString(entry.PayloadHex)
			if err != nil {
				t.Fatal(err)
			}
			actual, decodeErr := Decode(string(payload))
			if !entry.NativeAccept {
				if actual != "" || decodeErr == nil {
					t.Fatalf("invalid/non-scalar payload accepted: %x, %v (Source %s)", actual, decodeErr, entry.SourceErrorClass)
				}
				if entry.SourceErrorClass == "" && !errors.Is(decodeErr, ErrInvalidScalar) {
					t.Fatalf("Source surrogate acceptance requires explicit native scalar refusal: %v", decodeErr)
				}
				return
			}
			if entry.SourceErrorClass != "" || entry.SourceErrorMessage != "" {
				t.Fatal("accepted fixture records a Source error")
			}
			want, scalar := referenceText(t, entry.SourceCodePoints)
			if !scalar || decodeErr != nil || actual != want || !utf8.ValidString(actual) {
				t.Fatalf("raw decode = %q, %v; CPython = %q", actual, decodeErr, want)
			}
		})
	}
}

// referenceText preserves Python surrogate code points as invalid UTF-8 for the
// Encode refusal cases; converting []rune directly would silently make U+FFFD.
// Fixture strings themselves use numeric code points to avoid JSON replacement.
func referenceText(t *testing.T, points []int) (string, bool) {
	t.Helper()
	var result strings.Builder
	scalar := true
	for _, point := range points {
		if point < 0 || point > utf8.MaxRune {
			t.Fatalf("invalid Source code point %x", point)
		}
		if point >= 0xd800 && point <= 0xdfff {
			result.WriteByte(byte(0xe0 | point>>12))
			result.WriteByte(byte(0x80 | (point>>6)&0x3f))
			result.WriteByte(byte(0x80 | point&0x3f))
			scalar = false
		} else {
			result.WriteRune(rune(point))
		}
	}
	return result.String(), scalar
}
