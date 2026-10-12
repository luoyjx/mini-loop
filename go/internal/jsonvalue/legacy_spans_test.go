package jsonvalue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type legacyTextProjection struct {
	Bytes  int
	SHA256 string
}

func assertLegacyTextProjection(t *testing.T, value Value, want map[string]legacyTextProjection) {
	t.Helper()
	for _, mode := range []struct {
		name   string
		encode func() (string, error)
	}{{"compact", value.LegacyString}, {"pretty", value.LegacyStringIndent}} {
		actual, err := mode.encode()
		if err != nil {
			t.Fatal(err)
		}
		expected, ok := want[mode.name]
		if !ok {
			t.Fatal("Source mode missing", mode.name)
		}
		hash := sha256.Sum256([]byte(actual))
		if len(actual) != expected.Bytes || hex.EncodeToString(hash[:]) != expected.SHA256 {
			t.Fatal("Source legacy text bytes differ", mode.name, len(actual), expected)
		}
	}
}
func TestLegacyTextSpansMatchSourceAndSurrogatepass(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name, Seed  string
			Repeat      int
			ObjectKey   bool `json:"object_key"`
			Projections map[string]legacyTextProjection
		}
		Legacy []struct {
			Name        string
			Input       string `json:"input_utf8_hex"`
			ObjectKey   bool   `json:"object_key"`
			Projections map[string]legacyTextProjection
		} `json:"legacy_cases"`
	}
	data, err := os.ReadFile("../../testdata/python-json-text-spans.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 14 || len(fixture.Legacy) != 8 {
		t.Fatal("Source recipe inventory changed")
	}
	for _, row := range fixture.Cases {
		name := row.Name + "/text"
		if row.ObjectKey {
			name = row.Name + "/key"
		}
		t.Run(name, func(t *testing.T) {
			text := strings.Repeat(row.Seed, row.Repeat)
			value := TextValue(text)
			if row.ObjectKey {
				value = ObjectValue([]Field{{Name: text, Value: value}})
			}
			assertLegacyTextProjection(t, value, map[string]legacyTextProjection{"compact": row.Projections["utf8"], "pretty": row.Projections["legacy_pretty"]})
		})
	}
	for _, row := range fixture.Legacy {
		name := row.Name + "/text"
		if row.ObjectKey {
			name = row.Name + "/key"
		}
		t.Run(name, func(t *testing.T) {
			raw, err := hex.DecodeString(row.Input)
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			value := TextValue(text)
			if row.ObjectKey {
				value = ObjectValue([]Field{{Name: text, Value: value}})
			}
			assertLegacyTextProjection(t, value, row.Projections)
		})
	}
}
func TestLegacyTextMalformedBytesKeepRuneReplacement(t *testing.T) {
	for _, text := range []string{"\xff", "\xc0\xaf", "\xe2\x82", "a\xffz"} {
		expected := string([]rune(text))
		want, err := TextValue(expected).LegacyString()
		if err != nil {
			t.Fatal(err)
		}
		for _, method := range []func() (string, error){TextValue(text).LegacyString, TextValue(text).LegacyStringIndent} {
			got, err := method()
			if err != nil || got != want {
				t.Fatal("native malformed-byte projection changed", err)
			}
		}
	}
}
func BenchmarkLegacyTextProjection(b *testing.B) {
	for _, row := range []struct{ name, text string }{{"ascii", strings.Repeat("m", 524288)}, {"escaped", strings.Repeat(strings.Repeat("m", 4096)+"\"\\\n", 128)}, {"unicode", strings.Repeat("中😀\u2028<>&", 32768)}} {
		for _, pretty := range []bool{false, true} {
			name := row.name + "/compact"
			if pretty {
				name = row.name + "/pretty"
			}
			b.Run(name, func(b *testing.B) {
				value := TextValue(row.text)
				b.SetBytes(int64(len(row.text)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var out string
					var err error
					if pretty {
						out, err = value.LegacyStringIndent()
					} else {
						out, err = value.LegacyString()
					}
					if err != nil || len(out) < len(row.text)+2 {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
