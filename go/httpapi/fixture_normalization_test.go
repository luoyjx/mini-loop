package httpapi

import (
	"bytes"
	"strings"
	"testing"
)

func TestFixtureNormalizationRetainsLexicalNumbersAndReplacements(t *testing.T) {
	input := []byte(`{"n":9007199254740993,"decimal":1.00,"exp":1e+9999,"negative_zero":-0,"entries":[{"build":"old","created_at":12,"path":"/workspace/session"}],"flag":true,"empty":null}`)
	want := []byte(`{"decimal":1.00,"empty":null,"entries":[{"build":"test-build","created_at":0,"path":"ROOT/SID"}],"exp":1e+9999,"flag":true,"n":9007199254740993,"negative_zero":-0}`)
	got := normalizeJSON(t, input, map[string]string{"session": "SID"}, map[string]string{"/workspace": "ROOT"})
	if !bytes.Equal(got, want) {
		t.Fatalf("normalized fixture differs:\n%s\n%s", got, want)
	}
	// Deep fixtures retain the complete shape and numeric precision; processing
	// each child as encoded JSON caused repeated whole-subtree scans under race.
	deep := []byte(strings.Repeat(`{"x":`, 1000) + "9007199254740993" + strings.Repeat("}", 1000))
	if got := normalizeJSON(t, deep, nil, nil); !bytes.Equal(got, deep) {
		t.Fatal("deep fixture changed")
	}
}
