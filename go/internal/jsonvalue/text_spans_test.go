package jsonvalue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestLargeTextBytesMatchCPython(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name, Seed  string
			Repeat      int
			ObjectKey   bool `json:"object_key"`
			Projections map[string]struct {
				Bytes  int
				SHA256 string
			}
		}
	}
	data, err := os.ReadFile("../../testdata/python-json-text-spans.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 14 {
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		name := row.Name + "/text"
		if row.ObjectKey {
			name = row.Name + "/object-key"
		}
		t.Run(name, func(t *testing.T) {
			text := strings.Repeat(row.Seed, row.Repeat)
			value := TextValue(text)
			if row.ObjectKey {
				value = ObjectValue([]Field{{text, value}})
			}
			for _, method := range []struct {
				name   string
				encode func() ([]byte, error)
			}{{"utf8", value.MarshalUTF8}, {"utf8", value.MarshalLegacyUTF8}, {"spaced", value.MarshalUTF8Spaced}, {"archive", value.MarshalJSON}} {
				actual, err := method.encode()
				if err != nil {
					t.Fatal(method.name, err)
				}
				want := row.Projections[method.name]
				digest := sha256.Sum256(actual)
				if len(actual) != want.Bytes || hex.EncodeToString(digest[:]) != want.SHA256 {
					t.Fatal("Source bytes differ", method.name, len(actual), want)
				}
			}
		})
	}
}

func TestUTF8TextRetainsInvalidByteAndSurrogateBoundaries(t *testing.T) {
	for _, text := range []string{"\xed\xa0\x80", "a\xed\xbf\xbfz"} {
		for _, value := range []Value{TextValue(text), ObjectValue([]Field{{text, NullValue()}})} {
			if _, err := value.MarshalUTF8(); !errors.Is(err, ErrSurrogate) {
				t.Fatal("surrogate accepted", err)
			}
			if _, err := value.MarshalLegacyUTF8(); !errors.Is(err, ErrSurrogate) {
				t.Fatal("legacy surrogate became UTF-8", err)
			}
		}
	}
	// Existing native raw-string construction replaces malformed bytes with RuneError.
	for _, text := range []string{"\xff", "\xc0\xaf", "\xe2\x82"} {
		actual, err := TextValue(text).MarshalUTF8()
		want, _ := json.Marshal(string([]rune(text)))
		if err != nil || string(actual) != string(want) {
			t.Fatal("malformed-byte projection changed", actual, want, err)
		}
	}
}

func BenchmarkUTF8LargeText(b *testing.B) {
	for _, row := range []struct{ name, text string }{
		{"ascii", strings.Repeat("m", 524288)},
		{"escaped", strings.Repeat(strings.Repeat("m", 4096)+"\"\\\n", 128)},
		{"unicode", strings.Repeat("中😀\u2028<>&", 32768)},
	} {
		b.Run(row.name, func(b *testing.B) {
			value := TextValue(row.text)
			b.SetBytes(int64(len(row.text)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := value.MarshalUTF8()
				if err != nil || len(out) < len(row.text)+2 {
					b.Fatal("invalid encoding", err)
				}
			}
		})
	}
}

func BenchmarkArchiveASCIIText(b *testing.B) {
	value := TextValue(strings.Repeat("m", 524288))
	b.SetBytes(524288)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out, err := value.MarshalJSON()
		if err != nil || len(out) != 524290 {
			b.Fatal("invalid archival encoding", err)
		}
	}
}
