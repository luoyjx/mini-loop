package jsonvalue

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func TestStringReadSpansRetainGrammarAndRawBytes(t *testing.T) {
	for _, row := range []struct{ input, text string }{
		{`""`, ""}, {`"abc"`, "abc"}, {`"中😀"`, "中😀"},
		{`"a\"b\\c\/d\b\f\n\r\t"`, "a\"b\\c/d\b\f\n\r\t"},
		{`"\u0000\u001f\u007f\u0080"`, "\x00\x1f\x7f\u0080"},
		{`"\ud83d\ude00"`, "😀"}, {`"\ud800\u0061"`, "\xed\xa0\x80a"},
		{`"\ud800"`, "\xed\xa0\x80"}, {`"\udc00"`, "\xed\xb0\x80"},
		{"\"a\xff\xc0\xaf\xe2\x82b\"", "a\xff\xc0\xaf\xe2\x82b"},
		{"\"\xed\xa0\x80\"", "\xed\xa0\x80"},
		{`"\n` + strings.Repeat("x", 524288) + `\t"`, "\n" + strings.Repeat("x", 524288) + "\t"},
	} {
		value, err := Decode(row.input)
		text, ok := value.Text()
		if err != nil || !ok || text != row.text {
			t.Fatalf("string projection differs: %v %v", ok, err)
		}
		object, err := Decode("{" + row.input + ":1," + row.input + ":2}")
		item, found := object.Lookup(row.text)
		integer, _ := item.Integer()
		if err != nil || !found || integer != "2" || len(object.Keys()) != 1 {
			t.Fatalf("object key/replacement differs: %v %v", found, err)
		}
	}
	for _, input := range []string{`"`, `"abc`, `"abc\`, `"\x"`, `"\u123"`, `"\uZZZZ"`, `"\ud800\uZZZZ"`, `"abc"tail`} {
		if _, err := Decode(input); !errors.Is(err, ErrSyntax) {
			t.Fatalf("invalid string accepted: %q %v", input, err)
		}
	}
	for control := byte(0); control < 32; control++ {
		if _, err := Decode("\"safe" + string([]byte{control}) + "tail\""); !errors.Is(err, ErrSyntax) {
			t.Fatalf("raw control accepted: %d %v", control, err)
		}
	}
}

func TestStringReadSpansDetachShortTextFromLargeArchive(t *testing.T) {
	input := `{"key":"value","unused":"` + strings.Repeat("x", 1048576) + `"}`
	value, err := Decode(input)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := value.Lookup("key")
	text, _ := item.Text()
	// Address comparison is test-only: short retained fields must not keep the
	// large input backing allocation alive, as the original copying reader did.
	start := uintptr(unsafe.Pointer(unsafe.StringData(input)))
	for _, retained := range []string{value.Keys()[0], text} {
		address := uintptr(unsafe.Pointer(unsafe.StringData(retained)))
		if address >= start && address < start+uintptr(len(input)) {
			t.Fatal("short field retains the whole input archive")
		}
	}
	runtime.KeepAlive(input)
}

func BenchmarkDecodeLargeString(b *testing.B) {
	for _, row := range []struct{ name, text string }{
		{"ascii", `"` + strings.Repeat("m", 524288) + `"`},
		{"escaped", `"` + strings.Repeat(strings.Repeat("m", 4096)+`\n`, 128) + `"`},
		{"unicode", `"` + strings.Repeat("中😀", 65536) + `"`},
	} {
		b.Run(row.name, func(b *testing.B) {
			b.SetBytes(int64(len(row.text)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Decode(row.text); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
