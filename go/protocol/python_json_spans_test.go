package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPythonJSONTextSourceHashes(t *testing.T) {
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
	data, err := os.ReadFile("../testdata/python-json-text-spans.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 14 {
		t.Fatal("Source recipe inventory changed")
	}
	for _, row := range fixture.Cases {
		name := row.Name + "/text"
		if row.ObjectKey {
			name = row.Name + "/key"
		}
		t.Run(name, func(t *testing.T) {
			text := strings.Repeat(row.Seed, row.Repeat)
			for _, mode := range []struct {
				name           string
				ascii, compact bool
			}{{"utf8", false, true}, {"spaced", false, false}, {"archive", true, true}, {"ascii_spaced", true, false}} {
				var actual string
				var err error
				if row.ObjectKey {
					actual, err = PythonJSON(map[string]string{text: text}, mode.ascii, mode.compact)
				} else {
					actual, err = PythonJSON(text, mode.ascii, mode.compact)
				}
				if err != nil {
					t.Fatal(mode.name, err)
				}
				want, ok := row.Projections[mode.name]
				if !ok {
					t.Fatal("Source profile missing", mode.name)
				}
				hash := sha256.Sum256([]byte(actual))
				if len(actual) != want.Bytes || hex.EncodeToString(hash[:]) != want.SHA256 {
					t.Fatal("Source JSON bytes differ", mode.name, len(actual), want)
				}
			}
		})
	}
}

func TestPythonJSONFormattingPreservesNumericLexemesAndStringDelimiters(t *testing.T) {
	input := []byte(`{"a":[1e-6,-0,1.0,NaN,Infinity,-Infinity],"b":"colon:comma,quote\"slash\\"}`)
	for _, ascii := range []bool{false, true} {
		compact, err := formatPythonJSONBytes(input, ascii, true)
		if err != nil || compact != string(input) {
			t.Fatal("compact spelling changed", err, compact)
		}
		spaced, err := formatPythonJSONBytes(input, ascii, false)
		expected := `{"a": [1e-6, -0, 1.0, NaN, Infinity, -Infinity], "b": "colon:comma,quote\"slash\\"}`
		if err != nil || spaced != expected {
			t.Fatal("spaced spelling/string delimiter changed", err, spaced)
		}
	}
}

func BenchmarkPythonJSONTextFormatting(b *testing.B) {
	for _, row := range []struct{ name, text string }{
		{"ascii", strings.Repeat("m", 524288)},
		{"escaped", strings.Repeat(strings.Repeat("m", 4096)+"\"\\\n", 128)},
		{"unicode", strings.Repeat("中😀\u2028<>&", 32768)},
	} {
		for _, ascii := range []bool{false, true} {
			name := row.name + "/utf8"
			if ascii {
				name = row.name + "/ascii"
			}
			b.Run(name, func(b *testing.B) {
				raw, err := json.Marshal(row.text)
				if err != nil {
					b.Fatal(err)
				}
				b.SetBytes(int64(len(row.text)))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					out, err := formatPythonJSONBytes(raw, ascii, false)
					if err != nil || len(out) < len(row.text)+2 {
						b.Fatal("invalid output", err)
					}
				}
			})
		}
	}
}
