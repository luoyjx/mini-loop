package pytext

import "testing"

func TestPrintableRetainsSourceUnicodeVersion(t *testing.T) {
	for _, r := range []rune{' ', '界', '\U0001f600'} {
		if !Printable(r) {
			t.Fatal("source printable lost", r)
		}
	}
	for _, r := range []rune{'\n', '\u00a0', '\u200d', '\ue000', 0xd800, '\U0001fae8', -1, 0x110000} {
		if Printable(r) {
			t.Fatal("source nonprintable accepted", r)
		}
	}
	if Repr("\U0001fae8") != `'\U0001fae8'` {
		t.Fatal("diagnostic quoting uses newer Go tables")
	}
	if PythonUnicodeVersion != "14.0.0" {
		t.Fatal("declared source profile changed", PythonUnicodeVersion)
	}
}
