package decisions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestDecisionTextSourceBytesAndExactBudget(t *testing.T) {
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
		t.Fatal("Source inventory changed")
	}
	for _, row := range fixture.Cases {
		name := row.Name + "/text"
		if row.ObjectKey {
			name = row.Name + "/key"
		}
		t.Run(name, func(t *testing.T) {
			text := strings.Repeat(row.Seed, row.Repeat)
			value := StringValue(text)
			if row.ObjectKey {
				value = ObjectValue(map[string]Value{text: value})
			}
			want := row.Projections["utf8"]
			out, err := encodeValue(value, want.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(out)
			if len(out) != want.Bytes || hex.EncodeToString(hash[:]) != want.SHA256 {
				t.Fatal("Source bytes differ", len(out), want)
			}
			if out, err := encodeValue(value, want.Bytes-1); err == nil || out != nil {
				t.Fatal("escaped byte budget overflow accepted", err)
			}
			if want.Bytes <= MaxResponseBytes {
				out, err = value.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				hash = sha256.Sum256(out)
				if hex.EncodeToString(hash[:]) != want.SHA256 {
					t.Fatal("public encoder changed Source bytes")
				}
				parsed, err := DecodeValue(out)
				if err != nil {
					t.Fatal(err)
				}
				roundtrip, err := parsed.MarshalJSON()
				if err != nil || string(roundtrip) != string(out) {
					t.Fatal("roundtrip changed bytes", err)
				}
			} else if out, err := value.MarshalJSON(); err == nil || out != nil {
				t.Fatal("public byte budget lost")
			}
		})
	}
}

func TestDecisionTextRejectsInvalidUTF8AtAllPositions(t *testing.T) {
	for _, invalid := range []string{string([]byte{255}), string([]byte{0xed, 0xa0, 0x80}), string([]byte{0xe2, 0x82})} {
		for _, text := range []string{invalid + "valid", "valid" + invalid, "valid" + invalid + "valid"} {
			for _, value := range []Value{StringValue(text), ObjectValue(map[string]Value{text: NullValue()})} {
				if out, err := value.MarshalJSON(); err == nil || out != nil || !strings.Contains(err.Error(), "valid JSON data") {
					t.Fatal("invalid UTF-8 escaped its validation boundary", err)
				}
			}
		}
	}
	for _, limit := range []int{0, 1} {
		if out, err := encodeValue(StringValue(""), limit); err == nil || out != nil {
			t.Fatal("quote budget lost", limit)
		}
	}
}

func BenchmarkDecisionValueText(b *testing.B) {
	for _, row := range []struct{ name, text string }{
		{"ascii", strings.Repeat("m", MaxResponseBytes-2)},
		{"escaped", strings.Repeat(strings.Repeat("m", 4096)+"\"\\\n", 120)},
		{"unicode", strings.Repeat("中😀\u2028<>&", 32768)},
	} {
		b.Run(row.name, func(b *testing.B) {
			value := StringValue(row.text)
			b.SetBytes(int64(len(row.text)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := value.MarshalJSON()
				if err != nil || len(out) < len(row.text)+2 {
					b.Fatal("invalid encoding", err)
				}
			}
		})
	}
}
